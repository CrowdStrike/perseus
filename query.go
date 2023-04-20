package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"text/template"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"github.com/theckman/yacspin"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"

	"github.com/CrowdStrike/perseus/perseusapi"
)

const (
	goTemplateArgUsage = `provides a Go text template to format the output.
Each result is an instance of the following struct:
	type Item struct {
		// the module name and version, ex: github.com/CrowdStrike/perseus and v0.11.38
		Path, Version string
		// true if this module is a direct dependency of the "root" module, false if not
		IsDirect bool
		// the number of dependency links between this module and the "root" module
		// - direct dependencies have a degree of 1, dependencies of direct dependencies
		//   have a degree of 2, etc.
		Degree int
	}
The Name() method also returns a string containing "[Path]@[Version]".`
	listModuleVersionsExampleUsage = `  # list all known versions of Perseus
  perseus query list-module-versions github.com/CrowdStrike/perseus

  # list all versions of CrowdStrike GitHub modules, including pre-releases
  perseus query lmv 'github.com/CrowdStrike/*' --include-prerelease

  # list the highest v1.x version of all CrowdStrike GitHub modules
  perseus q lmv 'github.com/CrowdStrike/*' -v 'v1.*' --latest`
)

func tty() bool {
	return isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
}

// createQueryCommand initializes and returns a *cobra.Command that implements the 'query' CLI sub-command
func createQueryCommand() *cobra.Command {
	cmd := cobra.Command{
		Use:          "query ...",
		Aliases:      []string{"q"},
		Short:        "Executes a query against the Perseus graph",
		SilenceUsage: true,
	}
	fset := cmd.PersistentFlags()
	fset.String("server-addr", os.Getenv("PERSEUS_SERVER_ADDR"), "the TCP host and port of the Perseus server (default is $PERSEUS_SERVER_ADDR environment variable)")
	fset.BoolVar(&formatAsJSON, "json", false, "specifies that the output should be formatted as JSON")
	fset.BoolVar(&formatAsList, "list", false, "specifies that the output should be formatted as a tabular list")
	fset.BoolVar(&formatAsDotGraph, "dot", false, "specifies that the output should be a DOT directed graph (not supported for list-modules or list-module-versions)")
	fset.StringVarP(&formatTemplate, "format", "f", "", goTemplateArgUsage)
	fset.IntVar(&maxDepth, "max-depth", 4, "specifies the maximum number of levels to be returned")
	fset.BoolVar(&disableTLS, "insecure", false, "do not use TLS when connecting to the Perseus server")

	listModulesCmd := cobra.Command{
		Use:          "list-modules [pattern]",
		Aliases:      []string{"lm"},
		Short:        "Outputs the list of modules, along with the highest version, that match a provided glob pattern",
		RunE:         runListModulesCmd,
		SilenceUsage: true,
	}
	cmd.AddCommand(&listModulesCmd)

	listVersionsCmd := cobra.Command{
		Use:          "list-module-versions [--versions=(version glob)] (module glob)",
		Example:      listModuleVersionsExampleUsage,
		Aliases:      []string{"lmv"},
		Short:        "Outputs a list of module versions that match the provided glob pattern(s)",
		RunE:         runListModuleVersionsCmd,
		SilenceUsage: true,
	}
	listVersionsCmd.Flags().StringP("versions", "v", "", "optional glob pattern specifying which module version(s) should be returned")
	listVersionsCmd.Flags().Bool("latest", false, "specifies that only the latest/highest version matching the provided pattern should be returned")
	listVersionsCmd.Flags().BoolP("include-prerelease", "p", false, "specifies that pre-release versions should be returned")
	cmd.AddCommand(&listVersionsCmd)

	descendantsCmd := cobra.Command{
		Use:          "descendants module[@version]",
		Aliases:      []string{"d", "dependants", "dependents"},
		Short:        "Outputs the list of modules that depend on the specified module",
		RunE:         runQueryModuleGraphCmd,
		SilenceUsage: true,
	}
	cmd.AddCommand(&descendantsCmd)

	ancestorsCmd := cobra.Command{
		Use:          "ancestors module[@version]",
		Aliases:      []string{"a", "dependencies"},
		Short:        "Outputs the list of modules that the specified module depends on",
		RunE:         runQueryModuleGraphCmd,
		SilenceUsage: true,
	}
	cmd.AddCommand(&ancestorsCmd)

	return &cmd
}

// runListModulesCmd implements the logic behind the 'query list-modules' CLI sub-commands
func runListModulesCmd(cmd *cobra.Command, args []string) error {
	conf, err := parseSharedQueryOpts(cmd, args)
	if err != nil {
		return err
	}
	if len(args) != 1 {
		return fmt.Errorf("The module match pattern must be provided")
	}

	if formatAsDotGraph {
		return fmt.Errorf("DOT graph output is not supported for this command")
	}
	formatAsJSON = formatAsJSON || !(formatAsList || formatTemplate != "")
	if !xor(formatAsJSON, formatAsList, formatTemplate != "") {
		return fmt.Errorf("Only one of --json, --list, or --format may be specified")
	}

	updateSpinner, stopSpinner := startSpinner()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ps, err := conf.dialServer()
	if err != nil {
		stopSpinner()
		return err
	}

	results, err := listModules(ctx, ps, args[0], updateSpinner)
	stopSpinner()
	if err != nil {
		return err
	}

	if err = writeResults(os.Stdout, results); err != nil {
		return err
	}
	return nil
}

func runListModuleVersionsCmd(cmd *cobra.Command, args []string) error {
	conf, err := parseSharedQueryOpts(cmd, args)
	if err != nil {
		return err
	}
	if len(args) != 1 {
		return fmt.Errorf("The module match pattern must be provided")
	}

	if formatAsDotGraph {
		return fmt.Errorf("DOT graph output is not supported for this command")
	}
	formatAsJSON = formatAsJSON || !(formatAsList || formatTemplate != "")
	if !xor(formatAsJSON, formatAsList, formatTemplate != "") {
		return fmt.Errorf("Only one of --json, --list, or --format may be specified")
	}

	updateSpinner, stopSpinner := startSpinner()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ps, err := conf.dialServer()
	if err != nil {
		stopSpinner()
		return err
	}

	versionFilter, err := cmd.Flags().GetString("versions")
	if err != nil {
		debugLog("error reading 'version' CLI flag", "err", err)
	}
	latest, err := cmd.Flags().GetBool("latest")
	if err != nil {
		debugLog("error reading 'latest' CLI flag", "err", err)
	}
	includePrerelease, err := cmd.Flags().GetBool("include-prerelease")
	if err != nil {
		debugLog("error reading 'include-prerelease' CLI flag", "err", err)
	}
	results, err := listModuleVersions(ctx, ps, listModuleVersionsRequest{
		modulePattern:     args[0],
		versionPattern:    versionFilter,
		latestOnly:        latest,
		includePrerelease: includePrerelease,
		updateStatus:      updateSpinner,
	})
	stopSpinner()
	if err != nil {
		return err
	}
	if len(results) == 0 {
		debugLog("found no matching versions for matching modules", "filter", versionFilter, "module", args[0])
		return nil
	}

	if err = writeResults(os.Stdout, results); err != nil {
		return err
	}

	return nil
}

// runQueryModuleGraphCmd implements the logic behind the 'query ancestors' and 'query descendants' CLI sub-commands
func runQueryModuleGraphCmd(cmd *cobra.Command, args []string) error {
	conf, err := parseSharedQueryOpts(cmd, args)
	if err != nil {
		return err
	}
	if len(args) != 1 {
		return fmt.Errorf("The root module name/version must be provided")
	}

	var rootMod module.Version
	toks := strings.Split(args[0], "@")
	switch len(toks) {
	case 1:
		rootMod.Path = toks[0]
	case 2:
		rootMod.Path = toks[0]
		rootMod.Version = toks[1]
	default:
		return fmt.Errorf("Invalid module path/version %q", args[0])
	}
	if err := module.CheckPath(rootMod.Path); err != nil {
		return fmt.Errorf("The specified module name %q is invalid: %w", rootMod, err)
	}

	formatAsJSON = formatAsJSON || !(formatAsList || formatAsDotGraph || formatTemplate != "")
	if !xor(formatAsJSON, formatAsList, formatAsDotGraph, formatTemplate != "") {
		return fmt.Errorf("Only one of --json, --list, --dot, or --format may be specified")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ps, err := conf.dialServer()
	if err != nil {
		return err
	}
	switch rootMod.Version {
	case "", "latest":
		rootMod.Version, err = lookupLatestModuleVersion(ctx, ps, rootMod.Path)
		if err != nil {
			return err
		}
	default:
		if !semver.IsValid(rootMod.Version) {
			return fmt.Errorf("%s is not a valid Go module semantic version string", rootMod.Version)
		}
	}

	if maxDepth <= 0 {
		maxDepth = 1
	}
	dir := perseusapi.DependencyDirection_dependencies
	if strings.HasPrefix(cmd.Use, "descendants") {
		dir = perseusapi.DependencyDirection_dependents
	}

	updateSpinner, stopSpinner := startSpinner()
	tree, err := walkDependencies(ctx, ps, rootMod, dir, 1, maxDepth, updateSpinner)
	if err != nil {
		return err
	}

	switch {
	case formatTemplate != "":
		tt := template.New("item")
		tt, err = tt.Parse(formatTemplate)
		if err != nil {
			stopSpinner()
			return fmt.Errorf("Invalid Go text template specified: %w", err)
		}
		list := flattenTree(tree, updateSpinner)
		stopSpinner()
		for _, e := range list {
			if err := tt.Execute(os.Stdout, e); err != nil {
				return fmt.Errorf("Error applying Go text template: %w", err)
			}
			os.Stdout.WriteString("\n")
		}

	case formatAsList:
		col1Label := "Dependent"
		if strings.HasPrefix(cmd.Use, "ancestors") {
			col1Label = "Dependency"
		}
		list := flattenTree(tree, updateSpinner)
		stopSpinner()
		tw := tabwriter.NewWriter(os.Stdout, 10, 4, 2, ' ', 0)
		defer func() { _ = tw.Flush() }()
		if _, err := tw.Write([]byte(col1Label + "\tDirect\n")); err != nil {
			return fmt.Errorf("Error writing tabular output: %w", err)
		}
		for _, e := range list {
			if _, err := tw.Write([]byte(fmt.Sprintf("%s\t%v\n", e.Name(), e.IsDirect))); err != nil {
				return fmt.Errorf("Error writing tabular output: %w", err)
			}
		}

	case formatAsDotGraph:
		updateSpinner("generating DOT graph")
		g := generateDotGraph(ctx, tree, dir)
		stopSpinner()
		os.Stdout.Write([]byte(g))

	default:
		// default to JSON output if no other option was specified
		updateSpinner("generating JSON")
		formattedTree, _ := json.Marshal(tree)
		stopSpinner()
		os.Stdout.WriteString(string(formattedTree))
		os.Stdout.WriteString("\n")
	}

	return nil
}

// parseSharedQueryOpts reads the process environment variables and CLI flags to populate a clientConfig
// instance
func parseSharedQueryOpts(cmd *cobra.Command, _ []string) (clientConfig, error) {
	// parse parameters and setup options
	var (
		opts []clientOption
		conf clientConfig
	)
	opts = append(opts, readClientConfigEnv()...)
	opts = append(opts, readClientConfigFlags(cmd.Flags())...)
	for _, fn := range opts {
		if err := fn(&conf); err != nil {
			return clientConfig{}, fmt.Errorf("could not apply client config option: %w", err)
		}
	}
	// validate config
	if conf.serverAddr == "" {
		return clientConfig{}, fmt.Errorf("the Perseus server address must be specified")
	}

	return conf, nil
}

// lookupLatestModuleVersion invokes the Perseus API to retrieve the highest known semantic version for
// the specified module.
func lookupLatestModuleVersion(ctx context.Context, c perseusapi.PerseusServiceClient, modulePath string) (version string, err error) {
	req := perseusapi.ListModuleVersionsRequest{
		ModuleName:    modulePath,
		VersionOption: perseusapi.ModuleVersionOption_latest,
	}
	resp, err := c.ListModuleVersions(ctx, &req)
	if err != nil {
		return "", err
	}
	if len(resp.Modules) == 0 || len(resp.Modules[0].Versions) == 0 {
		return "", fmt.Errorf("No version found for module %s", modulePath)
	}

	return resp.Modules[0].Versions[0], nil
}

// dependencyTreeNode defines the information returned by walkDependencies
type dependencyTreeNode struct {
	// the module name and version
	Module module.Version `json:"module"`
	// is this module a direct or indirect dependency of the "root" module being queried against
	Direct bool `json:"-"`
	// a list of one or more child dependencies of this module
	Deps []dependencyTreeNode `json:"deps,omitempty"`
}

// walkDependencies invokes the Perseus API to retrieve a list of directly dependencies for mod,
// recursing to the specified maximum depth
func walkDependencies(ctx context.Context, client perseusapi.PerseusServiceClient, mod module.Version,
	direction perseusapi.DependencyDirection, depth, maxDepth int, status func(string)) (node dependencyTreeNode, err error) {
	select {
	case <-ctx.Done():
		return node, ctx.Err()
	default:
	}
	if depth > maxDepth {
		return node, nil
	}

	node.Module = mod
	node.Direct = (depth == 1)
	status("processing " + node.Module.String())
	req := perseusapi.QueryDependenciesRequest{
		ModuleName: mod.Path,
		Version:    mod.Version,
		Direction:  direction,
	}
	for done := false; !done; done = (req.PageToken != "") {
		resp, err := client.QueryDependencies(ctx, &req)
		if err != nil {
			return dependencyTreeNode{}, err
		}
		for _, dep := range resp.Modules {
			dn := dependencyTreeNode{
				Module: module.Version{
					Path:    dep.GetName(),
					Version: dep.Versions[0],
				},
			}
			ndeps, err := walkDependencies(ctx, client, dn.Module, direction, depth+1, maxDepth, status)
			if err != nil {
				return dependencyTreeNode{}, err
			}
			if len(ndeps.Deps) > 0 {
				dn.Deps = append(dn.Deps, ndeps.Deps...)
			}
			node.Deps = append(node.Deps, dn)

		}
		req.PageToken = resp.NextPageToken
	}
	return node, nil
}

// listModules invokes the Perseus API to retrieve a list of all modules that match the provided filter
func listModules(ctx context.Context, ps perseusapi.PerseusServiceClient, filter string, status func(string)) (results []dependencyItem, err error) {
	req := perseusapi.ListModulesRequest{
		Filter: filter,
	}
	for done := false; !done; {
		status("retrieving modules")
		resp, err := ps.ListModules(ctx, &req)
		if err != nil {
			return nil, fmt.Errorf("todo: %w", err)
		}
		for _, mod := range resp.Modules {
			status(fmt.Sprintf("determining latest version for %s", mod.GetName()))
			req2 := perseusapi.ListModuleVersionsRequest{
				ModuleName:    mod.GetName(),
				VersionOption: perseusapi.ModuleVersionOption_latest,
			}
			resp2, err := ps.ListModuleVersions(ctx, &req2)
			switch {
			case err != nil:
				return nil, fmt.Errorf("Unable to determine the current version for %s: %w", mod.GetName(), err)

			case len(resp2.Modules) == 0 || len(resp2.Modules[0].Versions) == 0:
				status(fmt.Sprintf("no stable version found for %s, trying to find a pre-release", mod.GetName()))
				req2.IncludePrerelease = true
				resp2, err = ps.ListModuleVersions(ctx, &req2)
				switch {
				case err != nil:
					return nil, fmt.Errorf("Unable to determine the current version for %s: %w", mod.GetName(), err)

				case len(resp2.Modules) == 0 || len(resp2.Modules[0].Versions) == 0:
					return nil, fmt.Errorf("No versions found for %s", mod.GetName())

				default:
					// got it
				}
			default:
				// got it
			}

			results = append(results, dependencyItem{
				Path:    mod.GetName(),
				Version: resp2.Modules[0].Versions[0],
			})
		}
		req.PageToken = resp.GetNextPageToken()
		done = (req.PageToken != "")
	}
	return results, nil
}

type listModuleVersionsRequest struct {
	modulePattern     string
	versionPattern    string
	latestOnly        bool
	includePrerelease bool
	updateStatus      func(string)
}

// listModuleVersions invokes the Perseus API to retrieve a list of module versions that match the provided
// filter options.
func listModuleVersions(ctx context.Context, ps perseusapi.PerseusServiceClient, req listModuleVersionsRequest) (results []dependencyItem, err error) {
	var pageToken string
	for done := false; !done; {
		req.updateStatus("retrieving module versions")
		apiRequest := perseusapi.ListModuleVersionsRequest{
			ModuleFilter:      req.modulePattern,
			VersionFilter:     req.versionPattern,
			IncludePrerelease: req.includePrerelease,
			VersionOption:     perseusapi.ModuleVersionOption_all,
			PageToken:         pageToken,
		}
		if req.latestOnly {
			apiRequest.VersionOption = perseusapi.ModuleVersionOption_latest
		}
		req.updateStatus(fmt.Sprintf("retreiving versions for modules matching %q", req.modulePattern))
		resp, err := ps.ListModuleVersions(ctx, &apiRequest)
		if err != nil {
			return nil, fmt.Errorf("todo: %w", err)
		}

		// API response is 1 result per module with a list of versions
		// - flatten to 1 dependencyItem per module/version pair
		for _, mod := range resp.Modules {
			for _, ver := range mod.Versions {
				results = append(results, dependencyItem{
					Path:    mod.GetName(),
					Version: ver,
				})
			}
		}
		pageToken = resp.GetNextPageToken()
		done = (pageToken != "")
	}
	return results, nil
}

// flattenTree converts the nested tree of module dependencies into a flat list of unique modules
// sorted by module name then by highest to lowest semantic version
func flattenTree(tree dependencyTreeNode, updateStatus func(string)) []dependencyItem {
	var (
		uniqueMods = make(map[string]struct{})
		items      []dependencyItem
	)
	for _, dep := range tree.Deps {
		updateStatus("processing " + dep.Module.String())
		di := dependencyItem{
			Path:     dep.Module.Path,
			Version:  dep.Module.Version,
			IsDirect: true,
			Degree:   1,
		}
		items = append(items, di)
		uniqueMods[dep.Module.String()] = struct{}{}
		if len(dep.Deps) > 0 {
			items = append(items, processChildren(dep.Deps, uniqueMods, 2, updateStatus)...)
		}
	}
	updateStatus("sorting results")
	sort.Slice(items, func(i, j int) bool {
		lhs, rhs := items[i], items[j]
		cmp := strings.Compare(lhs.Path, rhs.Path)
		if cmp != 0 {
			return (cmp < 0)
		}
		return semver.Compare(lhs.Version, rhs.Version) > 0
	})
	return items
}

// processChildren flattens the dependency tree of deps into a list of unique modules
func processChildren(deps []dependencyTreeNode, uniqueMods map[string]struct{}, depth int, updateStatus func(string)) []dependencyItem {
	var items []dependencyItem
	for _, d := range deps {
		modName := d.Module.String()
		updateStatus("processing " + modName)
		if _, exists := uniqueMods[modName]; !exists {
			di := dependencyItem{
				Path:     d.Module.Path,
				Version:  d.Module.Version,
				IsDirect: (depth == 1),
				Degree:   depth,
			}
			items = append(items, di)
			uniqueMods[modName] = struct{}{}
			if len(d.Deps) > 0 {
				items = append(items, processChildren(d.Deps, uniqueMods, depth+1, updateStatus)...)
			}
		}
	}
	return items
}

// generateDotGraph constructs a DOT digraph for the specified dependency tree
func generateDotGraph(_ context.Context, tree dependencyTreeNode, dir perseusapi.DependencyDirection) string {
	rankDir, arrowDir := "RL", ""
	if dir == perseusapi.DependencyDirection_dependencies {
		rankDir, arrowDir = "LR", " [dir=back]"
	}
	var sb strings.Builder
	sb.WriteString(`digraph G {
    bgcolor="#414142";
	rankdir="` + rankDir + `";
	subgraph cluster_D {
        label="";
        node [shape=box style="rounded,filled" fontname=Arial fontsize=14 margin=.25 fillcolor="#F3F3F4" fontcolor="#58595B"]
        edge [color="#EC3525"]
		bgcolor="#58595B";
        style="rounded";
`)
	stack := []dependencyTreeNode{tree}
	uniq := make(map[string]struct{})
	for len(stack) > 0 {
		node := stack[0]
		stack = stack[1:]
		for _, dep := range node.Deps {
			// skip existing edges
			// . the same 2 module/version nodes can appear at multiple places within the overall tree
			// . the DOT renderer will draw an arrow for each if we include them all
			edgeKey := fmt.Sprintf("%s->%s", node.Module, dep.Module)
			if _, exists := uniq[edgeKey]; exists {
				continue
			}
			uniq[edgeKey] = struct{}{}

			sb.WriteString(fmt.Sprintf("\t\t%q -> %q%s\n", dep.Module, node.Module, arrowDir))
			if len(dep.Deps) > 0 {
				stack = append(stack, dep)
			}
		}
	}
	sb.WriteString("\t}\n}\n")
	return sb.String()
}

// dependencyItem represents the metadata associated with a particular module
type dependencyItem struct {
	// the module path, ex: github.com/CrowdStrike/perseus
	Path string
	// the module version, ex: v1.11.38
	Version string
	// is this module a direct or indirect dependency of the "root" module being queried against
	IsDirect bool
	// the number of dependency links between this module and the "root" module being queried against
	// . IsDirect = (Degree == 1)
	Degree int
}

// Name returns the full name of the dependency in "[name]@[version]" format
func (d dependencyItem) Name() string {
	return d.Path + "@" + d.Version
}

// xor implements a boolean exclusive OR for a set of values.  This is necessary because Go does not
// provide XOR operators (boolean or bitwise)
func xor(vs ...bool) bool {
	if len(vs) == 0 {
		return false
	}
	n := 0
	for _, v := range vs {
		if v {
			n++
		}
		if n > 1 {
			return false
		}
	}
	return n == 1
}

// startSpinner initializes and starts a "spinner" for the console and returns
// a function for updating the spinner's message and another to stop it.
func startSpinner() (update func(string), done func()) {
	update = func(string) {}
	done = func() {}

	// no-op if we're not writing to a TTY
	if tty() {
		spinner, _ := yacspin.New(yacspin.Config{
			CharSet:         yacspin.CharSets[11],
			Frequency:       300 * time.Millisecond,
			Message:         "",
			Prefix:          "querying the graph ",
			Suffix:          " ",
			SuffixAutoColon: false,
		})
		_ = spinner.Start()

		update = func(msg string) {
			spinner.Message(msg)
		}
		done = func() {
			_ = spinner.Stop()
		}
	}
	return update, done
}

// writeResults writes the contents of results to the provided io.Writer based on the configured output options
func writeResults(w io.Writer, results []dependencyItem) error {
	var err error
	switch {
	case formatTemplate != "":
		// apply the provided text template
		tt := template.New("item")
		tt, err = tt.Parse(formatTemplate)
		if err != nil {
			return fmt.Errorf("Invalid Go text template specified: %w", err)
		}
		for _, e := range results {
			if err := tt.Execute(w, e); err != nil {
				return fmt.Errorf("Error applying Go text template: %w", err)
			}
			fmt.Fprintln(w)
		}

	case formatAsList:
		// output a tabular list
		tw := tabwriter.NewWriter(w, 10, 4, 2, ' ', 0)
		defer func() { _ = tw.Flush() }()
		if _, err := tw.Write([]byte("Module\tVersion\n")); err != nil {
			return fmt.Errorf("Error writing tabular output: %w", err)
		}
		for _, e := range results {
			if _, err := tw.Write([]byte(fmt.Sprintf("%s\t%s\n", e.Path, e.Version))); err != nil {
				return fmt.Errorf("Error writing tabular output: %w", err)
			}
		}

	default:
		// output JSON
		output, _ := json.Marshal(results)
		_, _ = w.Write(output)
		fmt.Fprintln(w)
	}
	return nil
}
