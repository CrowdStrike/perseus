package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/CrowdStrike/perseus/internal/git"
	"github.com/CrowdStrike/perseus/perseusapi"
)

var (
	moduleVersion versionArg
)

// createUpdateCommand initializes and returns a *cobra.Command that implements the 'update' CLI sub-command
func createUpdateCommand() *cobra.Command {
	cmd := cobra.Command{
		Use:          "update path/to/go/module",
		Short:        "Processes a Go module and updates the Perseus graph with its direct dependencies",
		Example:      "perseus update . --version 0.11.38\nperseus update $HOME/dev/go/foo --version 1.0.0\nperseus update $HOME/dev/go/bar",
		RunE:         runUpdateCmd,
		SilenceUsage: true,
	}
	fset := cmd.Flags()
	fset.VarP(&moduleVersion, "version", "v", "specifies the version of the Go module to be processed.")
	fset.String("server-addr", "", "the TCP host and port of the Perseus server")

	return &cmd
}

// runUpdateCmd implements the 'update' CLI sub-command.
func runUpdateCmd(cmd *cobra.Command, args []string) error {
	// parse parameters and setup options
	var (
		opts []clientOption
		conf clientConfig
	)
	opts = append(opts, readClientConfigEnv()...)
	opts = append(opts, readClientConfigFlags(cmd.Flags())...)
	for _, fn := range opts {
		if err := fn(&conf); err != nil {
			return fmt.Errorf("could not apply client config option: %w", err)
		}
	}

	// validate config
	if conf.serverAddr == "" {
		return fmt.Errorf("the Perseus server address must be specified")
	}
	if len(args) != 1 {
		return fmt.Errorf("the path to the module is required")
	}
	moduleDir := path.Clean(args[0])

	// extract the module version from the repo if not specified
	if moduleVersion == "" {
		repo, err := git.Open(moduleDir)
		if err != nil {
			return err
		}
		tags, err := repo.VersionTags()
		if err != nil {
			return nil
		}
		switch len(tags) {
		case 1:
			moduleVersion = versionArg(tags[0])
		case 0:
			return fmt.Errorf("No semver tags exist at the current commit. Please specify a version explicitly.")
		default:
			return fmt.Errorf("Multiple semver tags exist at the current commit. Please specify a version explicitly. tags=%v", tags)
		}
	}

	// parse the module info
	info, err := parseModule(moduleDir)
	if err != nil {
		return err
	}
	mod := module{
		Name:    info.Name,
		Version: string(moduleVersion),
	}
	if debugMode {
		fmt.Printf("Processing Go module %s@%s (path=%q)\nDirect Dependencies", info.Name, moduleVersion, moduleDir)
		for _, d := range info.Deps {
			fmt.Printf("\t%s@%s\n", d.Name, d.Version)
		}
	}

	// send updates to the Perseus server
	if err := applyUpdates(conf, mod, info.Deps); err != nil {
		return fmt.Errorf("Unable to update the Perseus graph: %w", err)
	}
	return nil
}

// applyUpdates calls the Perseus server to update the dependencies of the specified module
func applyUpdates(conf clientConfig, mod module, deps []module) (err error) {
	// translate RPC errors to human-friendly ones on return
	defer func() {
		switch err {
		case context.DeadlineExceeded:
			err = fmt.Errorf("timed out trying to connect to the Perseus server")
		default:
			if err != nil {
				sc := status.Code(err)
				switch sc {
				case codes.Unavailable:
					err = fmt.Errorf("unable to connect to the Perseus server")
				default:
				}
			}
		}
	}()

	// setup gRPC connection options and connect
	dialOpts := []grpc.DialOption{
		grpc.WithBlock(),
		grpc.WithTransportCredentials(insecure.NewCredentials()), // TODO: support TLS
	}
	debugLog("connecting to Perseus server at %s", conf.serverAddr)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	conn, err := grpc.DialContext(ctx, conf.serverAddr, dialOpts...)
	if err != nil {
		return err
	}

	// create the client and call the server
	ctx = context.Background()
	client := perseusapi.NewPerseusServiceClient(conn)
	req := perseusapi.UpdateDependenciesRequest{
		ModuleName: mod.Name,
		Version:    mod.Version,
	}
	req.Dependencies = make([]*perseusapi.Module, len(deps))
	for i, d := range deps {
		req.Dependencies[i] = &perseusapi.Module{
			Name:     d.Name,
			Versions: []string{d.Version},
		}
	}
	if _, err = client.UpdateDependencies(ctx, &req); err != nil {
		return err
	}
	return nil
}

// moduleInfo represents the relevant Go module metadata for this application.
//
// This struct does not contain a version because the Go module library (golang.org/x/mod/modfile)
// does not return a version for the "main" module even if it is a library package.
type moduleInfo struct {
	// the module name, ex: github.com/CrowdStrike/perseus
	Name string
	// zero or more direct dependencies of the module
	Deps []module
}

// module represents a specific released version of a Go module
type module struct {
	Name, Version string
}

// parseModule reads the module info for a Go module at path p, which should be the path to a folder
// containing a go.mod file.
func parseModule(p string) (info moduleInfo, err error) {
	nfo, err := os.Stat(p)
	if err != nil {
		return info, fmt.Errorf("invalid module path: %w", err)
	}
	if !nfo.IsDir() {
		return info, fmt.Errorf("invalid module path: %w", err)
	}

	f, err := os.Open(path.Join(p, "go.mod"))
	if err != nil {
		return info, fmt.Errorf("unable to read go.mod: %w", err)
	}
	defer f.Close()

	contents, _ := io.ReadAll(f)
	mf, err := modfile.ParseLax("", contents, nil)
	if err != nil {
		return info, fmt.Errorf("unable to parse go.mod: %w", err)
	}
	info.Name = mf.Module.Mod.Path
	for _, req := range mf.Require {
		if req.Indirect {
			continue
		}
		info.Deps = append(info.Deps, module{Name: req.Mod.Path, Version: req.Mod.Version})
	}
	return info, nil
}

// versionArg represents a string CLI parameter that must be a valid semantic version string
type versionArg string

// String returns the argument value string
func (v *versionArg) String() string {
	return string(*v)
}

// Set assigns the argument value to s.  If s is not a valid semantic version string per Go modules
// rules, this method returns an error
func (v *versionArg) Set(s string) error {
	if !semver.IsValid(s) {
		return fmt.Errorf("%q is not a valid semantic version string", s)
	}
	*v = versionArg(s)
	return nil
}

// Type returns a string description of the argument type
func (v *versionArg) Type() string {
	return "[SemVer string]"
}

// Get returns the value of the argument
func (v *versionArg) Get() any {
	return *v
}
