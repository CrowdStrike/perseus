package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"

	"github.com/spf13/cobra"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"

	"github.com/CrowdStrike/perseus/internal/git"
	"github.com/CrowdStrike/perseus/internal/modproxy"
	"github.com/CrowdStrike/perseus/perseusapi"
)

var (
	moduleVersion     versionArg
	includePrerelease bool
)

const updateExampleUsage = `perseus update -p . --version v0.11.38
	perseus update --path $HOME/dev/go/foo --version v1.0.0
	perseus update -p $HOME/dev/go/bar
	perseus update --module golang.org/x/sys
	perseus update -m github.com/rs/zerolog -v v1.28.0`

// createUpdateCommand initializes and returns a *cobra.Command that implements the 'update' CLI sub-command
func createUpdateCommand() *cobra.Command {
	cmd := cobra.Command{
		Use:          "update (-p|--path path/to/go/module/on/disk | -m|--module github.com/example/foo)",
		Short:        "Processes a Go module and updates the Perseus graph with its direct dependencies",
		Example:      updateExampleUsage,
		RunE:         runUpdateCmd,
		SilenceUsage: true,
	}
	fset := cmd.Flags()
	fset.VarP(&moduleVersion, "version", "v", "specifies the version of the Go module to be processed.")
	fset.String("server-addr", os.Getenv("PERSEUS_SERVER_ADDR"), "the TCP host and port of the Perseus server (default is $PERSEUS_SERVER_ADDR environment variable)")
	fset.BoolVar(&includePrerelease, "prerelease", false, "if specified, include pre-release tags when processing the module")
	fset.StringP("path", "p", "", "specifies the local path on disk to a Go module repository")
	fset.StringP("module", "m", "", "specifies the module path of a public Go module")
	fset.BoolVar(&disableTLS, "insecure", false, "do not use TLS when connecting to the Perseus server")

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
			return fmt.Errorf("Could not apply client config option: %w", err)
		}
	}

	// validate config
	if conf.serverAddr == "" {
		return fmt.Errorf("The Perseus server address must be specified")
	}
	filePath, _ := cmd.Flags().GetString("path")
	modPath, _ := cmd.Flags().GetString("module")
	if filePath == "" && modPath == "" {
		return fmt.Errorf("Either a local path (--path) or a module path (--module) must be specified")
	}
	if !xor(filePath != "", modPath != "") {
		return fmt.Errorf("Either a local path (--path) or a module path (--module) can be specified, but not both")
	}

	var (
		info moduleInfo
		err  error
	)
	switch {
	case filePath != "":
		// read module dependencies from source code on disk
		info, err = getModuleInfoFromDir(filePath)
	case modPath != "":
		// read module dependencies from the module proxy
		info, err = getModuleInfoFromProxy(modPath)
	}
	if err != nil {
		return err
	}
	// no info available (probably a skipped pre-release tag), so nothing to do
	if info.Name == "" {
		return nil
	}

	// send updates to the Perseus server
	mod := module.Version{
		Path:    info.Name,
		Version: info.Version,
	}
	if err := applyUpdates(conf, mod, info.Deps); err != nil {
		return fmt.Errorf("Unable to update the Perseus graph: %w", err)
	}
	return nil
}

// getModuleInfoFromDir extracts the current direct dependencies of a Go module by inspecting the source
// code on disk at dir.
func getModuleInfoFromDir(dir string) (moduleInfo, error) {
	moduleDir := path.Clean(dir)

	// extract the module version from the repo if not specified
	if moduleVersion == "" {
		repo, err := git.Open(moduleDir)
		if err != nil {
			return moduleInfo{}, err
		}
		tags, err := repo.VersionTags()
		if err != nil {
			return moduleInfo{}, fmt.Errorf("unable to read version tags from the repo: %w", err)
		}
		switch len(tags) {
		case 1:
			moduleVersion = versionArg(tags[0])
		case 0:
			return moduleInfo{}, fmt.Errorf("No semver tags exist at the current commit. Please specify a version explicitly.")
		default:
			return moduleInfo{}, fmt.Errorf("Multiple semver tags exist at the current commit. Please specify a version explicitly. tags=%v", tags)
		}
	}

	if !includePrerelease && semver.Prerelease(string(moduleVersion)) != "" {
		fmt.Printf("skipping pre-release tag %s\n", moduleVersion)
		return moduleInfo{}, nil
	}

	// parse the module info
	info, err := parseModuleDir(moduleDir)
	info.Version = moduleVersion.String()
	if err != nil {
		return moduleInfo{}, err
	}
	if debugMode {
		fmt.Printf("Processing Go module %s@%s (path=%q)...\nDirect Dependencies:\n", info.Name, moduleVersion, moduleDir)
		for _, d := range info.Deps {
			fmt.Printf("\t%s\n", d)
		}
	}
	return info, nil
}

// getModuleInfoFromProxy extracts the current direct dependencies of a Go module by querying the
// system-configured Go module proxy/proxies.
func getModuleInfoFromProxy(modulePath string) (moduleInfo, error) {
	var (
		v   string
		err error
	)
	// get @latest from the proxy if no version was specified
	v = moduleVersion.String()
	if v == "" {
		v, err = modproxy.GetCurrentVersion(http.DefaultClient, modulePath, includePrerelease)
		if err != nil {
			return moduleInfo{}, fmt.Errorf("unable to determine @latest for module %s: %w", modulePath, err)
		}
	}

	if !includePrerelease && semver.Prerelease(v) != "" {
		fmt.Printf("skipping pre-release tag %s\n", v)
		return moduleInfo{}, nil
	}

	// parse the module info
	info, err := parseModulePath(modulePath, v)
	if err != nil {
		return moduleInfo{}, err
	}
	if debugMode {
		fmt.Printf("Processing Go module %s@%s...\nDirect Dependencies:\n", info.Name, info.Version)
		for _, d := range info.Deps {
			fmt.Printf("\t%s\n", d)
		}
	}
	return info, nil
}

// applyUpdates calls the Perseus server to update the dependencies of the specified module
func applyUpdates(conf clientConfig, mod module.Version, deps []module.Version) (err error) {
	// create the client and call the server
	ctx := context.Background()
	client, err := conf.dialServer()
	if err != nil {
		return err
	}
	req := perseusapi.UpdateDependenciesRequest{
		ModuleName: mod.Path,
		Version:    mod.Version,
	}
	req.Dependencies = make([]*perseusapi.Module, len(deps))
	for i, d := range deps {
		req.Dependencies[i] = &perseusapi.Module{
			Name:     d.Path,
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
	// the module version, ex: v1.42.13
	Version string
	// zero or more direct dependencies of the module
	Deps []module.Version
}

// fromModFile populates m from the provided modfile.File
func (m *moduleInfo) fromModFile(mf *modfile.File, v string) {
	m.Name = mf.Module.Mod.Path
	m.Version = v
	for _, req := range mf.Require {
		if req.Indirect {
			continue
		}
		m.Deps = append(m.Deps, module.Version{Path: req.Mod.Path, Version: req.Mod.Version})
	}
}

// parseModuleDir reads the module info for a Go module at path p, which should be the path to a folder
// containing a go.mod file.
func parseModuleDir(p string) (info moduleInfo, err error) {
	nfo, err := os.Stat(p)
	if err != nil {
		return info, fmt.Errorf("invalid module path: %w", err)
	}
	if !nfo.IsDir() {
		return info, fmt.Errorf("invalid module path: must be a folder")
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
	info.fromModFile(mf, "")
	return info, nil
}

// parseModulePath reads the module info for a Go module with path m and version v from the configured
// module proxy/proxies.  If v is "" then this function returns the info for the latest version.
func parseModulePath(m, v string) (info moduleInfo, err error) {
	if v == "" {
		return info, fmt.Errorf("module version must be specified")
	}

	var mf *modfile.File
	mf, err = modproxy.GetModFile(http.DefaultClient, m, v)
	if err != nil {
		return info, err
	}
	info.fromModFile(mf, v)
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
