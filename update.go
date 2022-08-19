package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/spf13/cobra"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/CrowdStrike/perseus/internal/git"
	"github.com/CrowdStrike/perseus/perseusapi"
)

var (
	moduleVersion versionArg
)

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
	fset.String("server-addr", "localhost:31138", "the TCP host and port of the Perseus server")

	return &cmd
}

func runUpdateCmd(cmd *cobra.Command, args []string) error {
	var opts []clientOption
	opts = append(opts, readClientConfigEnv()...)
	opts = append(opts, readClientConfigFlags(cmd.Flags())...)

	var conf clientConfig
	for _, fn := range opts {
		if err := fn(&conf); err != nil {
			return fmt.Errorf("could not apply client config option: %w", err)
		}
	}

	if len(args) != 1 {
		return fmt.Errorf("the path to the module is required")
	}
	moduleDir := path.Clean(args[0])

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
			//nolint: stylecheck // we want capitals and punctuation b/c these errors are shown to the user
			return fmt.Errorf("No semver tags exist at the current commit. Please specify a version explicitly.")
		default:
			//nolint: stylecheck // we want capitals and punctuation b/c these errors are shown to the user
			return fmt.Errorf("Multiple semver tags exist at the current commit. Please specify a version explicitly.")
		}
	}
	info, err := parseModule(moduleDir)
	if err != nil {
		return err
	}
	if debugMode {
		fmt.Printf("Processing Go module %s@%s (path=%q)\n", info.Name, moduleVersion, moduleDir)
	}
	mod := module{
		Name:    info.Name,
		Version: string(moduleVersion),
	}
	if debugMode {
		fmt.Println("Direct Dependencies:")
		for _, d := range info.Deps {
			fmt.Printf("\t%s@%s\n", d.Name, d.Version)
		}
	}
	if err := applyUpdates(conf, mod, info.Deps); err != nil {
		//nolint: stylecheck // we want capitals and punctuation b/c these errors are shown to the user
		return fmt.Errorf("Unable to update the Perseus graph: %w", err)
	}
	return nil
}

func applyUpdates(conf clientConfig, mod module, deps []module) error {
	// TODO: implement TLS
	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	debugLog("connecting to Perseus server at %s", conf.serverAddr)
	conn, err := grpc.Dial(conf.serverAddr, dialOpts...)
	if err != nil {
		return err
	}

	client := perseusapi.NewPerseusServiceClient(conn)

	ctx := context.Background()
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

type module struct {
	Name, Version string
}

type moduleInfo struct {
	Name string
	Deps []module
}

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

type versionArg string

func (v *versionArg) String() string {
	return string(*v)
}

func (v *versionArg) Set(s string) error {
	if !semver.IsValid(s) {
		return fmt.Errorf("%q is not a valid semantic version string", s)
	}
	*v = versionArg(s)
	return nil
}

func (v *versionArg) Type() string {
	return "[SemVer string]"
}

func (v *versionArg) Get() any {
	return *v
}
