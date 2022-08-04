package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	rootCommand.AddCommand(createServerCommand())

	if err := rootCommand.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

var rootCommand = &cobra.Command{
	Use:           "perseus",
	Short:         "perseus - defeating krakens since 2022",
	SilenceErrors: true, // don't print errors, we're handling it in main()
	SilenceUsage:  true, // don't print usage on error
}
