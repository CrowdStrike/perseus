package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
	"golang.org/x/mod/module"

	"github.com/CrowdStrike/perseus/perseusapi/perseusapiconnect"
)

const findPathsExampleUsage = `# find any path between the latest version of github.com/example/foo and any version of gRPC
# and output the result as a tree
perseus find-paths github.com/example/foo google.golang.org/grpc

# same, but output JSON
perseus find-paths github.com/example/foo google.golang.org/grpc --json

# find all paths between the latest version of github.com/example/foo and v1.43.0 of gRPC
# and output the result as a tree
perseus find-paths github.com/example/foo google.golang.org/grpc@v1.43.0 --all

# find all paths between v1.0.0 of github.com/example/foo and any version of gRPC
# and output the results as line-delimited JSON
perseus find-paths github.com/example/foo@v1.0.0 google.golang.org/grpc --all --json`

// createFindPathsCommand creates and returns a *cobra.Command that implements the 'find-paths' CLI command
func createFindPathsCommand() *cobra.Command {
	cmd := cobra.Command{
		Use:          "find-paths from_module[@version] to_module[@version]",
		Example:      findPathsExampleUsage,
		Aliases:      []string{"fp", "why"},
		Short:        "Queries the Perseus graph to find dependency path(s) between modules",
		RunE:         runFindPathsCommand,
		SilenceUsage: true,
	}
	fset := cmd.Flags()
	fset.String("server-addr", os.Getenv("PERSEUS_SERVER_ADDR"), "the TCP host and port of the Perseus server (default is $PERSEUS_SERVER_ADDR environment variable)")
	fset.BoolVar(&formatAsJSON, "json", false, "specifies that the output should be formatted as line-delimited JSON")
	fset.Bool("all", false, "Return all paths between the two modules")
	fset.IntVar(&maxDepth, "max-depth", 4, "specifies the maximum number of levels to be returned")
	fset.BoolVar(&disableTLS, "insecure", false, "do not use TLS when connecting to the Perseus server")

	return &cmd
}

// runFindPathsCmd implements the logic behind the 'find-paths' CLI sub-command
func runFindPathsCommand(cmd *cobra.Command, args []string) (err error) {
	conf, err := parseSharedQueryOpts(cmd, args)
	if err != nil {
		return err
	}
	switch len(args) {
	case 0, 1:
		return fmt.Errorf("The 'from' and 'to' modules are required")
	case 2:
		break
	default:
		return fmt.Errorf("Only 2 positional arguments, the 'from' and 'to' modules, are supported")
	}

	updateSpinner, stopSpinner := startSpinner()
	defer stopSpinner()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	updateSpinner("connecting to the server at " + conf.serverAddr)
	ps := conf.getClient()

	// validate the 'from' and 'to' modules, defaulting to the highest known release for 'from'
	// if no version is specified
	from, err := parseModuleArg(ctx, args[0], ps, true, updateSpinner)
	if err != nil {
		return err
	}
	to, err := parseModuleArg(ctx, args[1], ps, false, updateSpinner)
	if err != nil {
		return err
	}

	updateSpinner("Determining path(s) from " + from.String() + " to " + to.String())
	var (
		showAll, _ = cmd.Flags().GetBool("all")
		paths      = [][]module.Version{}
		pf         = newPathFinder(ps, maxDepth, updateSpinner)
	)
	// write the results on the way out
	defer func() {
		stopSpinner()
		if err != nil {
			return
		}
		if formatAsJSON {
			printJSONLinesTo(os.Stdout, paths)
		} else {
			printTreeTo(os.Stdout, paths)
		}
	}()
	for p := range pf.findPathsBetween(ctx, from, to) {
		if p.err != nil {
			// context cancellation is not a failure
			if errors.Is(p.err, context.Canceled) || connect.CodeOf(err) == connect.CodeCanceled {
				return nil
			}
			return p.err
		}

		updateSpinner("adding path")
		paths = append(paths, p.path)
		if !showAll {
			cancel()
		}
	}

	return nil
}

// printTreeTo writes the provided list of dependency paths to w as a nested textual tree.  Each level
// of the tree is indented and prefixed with "-> ".
func printTreeTo(w io.Writer, paths [][]module.Version) {
	for _, p := range paths {
		for indent, pp := range p {
			if indent > 0 {
				_, _ = io.WriteString(w, fmt.Sprintf("%s-> ", strings.Repeat(" ", 3*(indent-1))))
			}
			_, _ = io.WriteString(w, pp.String())
			_, _ = io.WriteString(w, "\n")
		}
	}
}

// printJSONLinesTo writes the provided list of dependency paths to w as a series of line-delimited
// JSON objects.  The JSON is structured such that each level has exactly 1 key, the name and version
// of a module, with the value of that key being the remainder of the path.
func printJSONLinesTo(w io.Writer, paths [][]module.Version) {
	for _, p := range paths {
		for _, pp := range p {
			_, _ = io.WriteString(w, fmt.Sprintf("{%q:", pp))
		}
		_, _ = io.WriteString(w, fmt.Sprintf("{}%s\n", strings.Repeat("}", len(p))))
	}
}

// parseModuleArg parses the provided string as a Go module path, optionally with a version, and returns
// the parsed result.  If no version is specified, the highest known version is used.
func parseModuleArg(ctx context.Context, arg string, client perseusapiconnect.PerseusServiceClient, findLatest bool, status func(string)) (module.Version, error) {
	defer status("")
	var m module.Version
	toks := strings.Split(arg, "@")
	switch len(toks) {
	case 1:
		m.Path = toks[0]
	case 2:
		m.Path = toks[0]
		m.Version = toks[1]
	default:
		return module.Version{}, fmt.Errorf("Invalid 'from' module path/version %q", arg)
	}
	if err := module.CheckPath(m.Path); err != nil {
		return module.Version{}, fmt.Errorf("The specified module name %q is invalid: %w", m, err)
	}
	if m.Version == "" && findLatest {
		status("determining current version for " + m.String())
		v, err := lookupLatestModuleVersion(ctx, client, m.Path)
		if err != nil {
			return module.Version{}, fmt.Errorf("Unable to determine the current version for %q: %w", m.Path, err)
		}
		m.Version = v
	}
	return m, nil
}
