package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/spf13/cobra"

	"github.com/CrowdStrike/perseus/internal/server"
)

func main() {
	rootCommand.PersistentFlags().BoolVarP(&debugMode, "debug", "x", os.Getenv("LOG_VERBOSITY") == "debug", "enable verbose logging")

	rootCommand.AddCommand(server.CreateServerCommand(debugLog))
	rootCommand.AddCommand(createUpdateCommand())
	rootCommand.AddCommand(createQueryCommand())
	rootCommand.AddCommand(createFindPathsCommand())
	rootCommand.AddCommand(versionCommand)

	if err := rootCommand.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

var (
	rootCommand = &cobra.Command{
		Use:           "perseus",
		Short:         "perseus - Defeating krakens since 2022",
		SilenceErrors: true, // don't print errors, we're handling it in main()
		SilenceUsage:  true, // don't print usage on error
	}

	BuildDate    = "unknown"
	BuildVersion = "v0.0.0-dev"
	commitHash   = "unknown"
	commitDate   = "unknown"

	versionInfoTemplate = `perseus - Defeating krakens since 2022
	%s (built %s, %s %s/%s)
	commit: %s (date: %s)
`

	versionCommand = &cobra.Command{
		Use:   "version",
		Short: "shows build/version info",
		Run:   runVersionCmd,
	}
)

func runVersionCmd(_ *cobra.Command, _ []string) {
	goos, goarch := "", ""
	nfo, _ := debug.ReadBuildInfo()
	for _, s := range nfo.Settings {
		switch s.Key {
		case "vcs.time":
			commitDate = s.Value
		case "vcs.revision":
			commitHash = s.Value
		case "GOOS":
			goos = s.Value
		case "GOARCH":
			goarch = s.Value
		default:
			// don't care about other settings
		}
	}
	fmt.Printf(versionInfoTemplate,
		BuildVersion, BuildDate, nfo.GoVersion, goos, goarch,
		commitHash, commitDate)
}
