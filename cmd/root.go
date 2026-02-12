package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var (
	project      = "dead-mans-switch"
	envVarPrefix = "DEAD_MANS_SWITCH"

	// Set at build time with -ldflags
	version = "dev"
	commit  = "none"
	date    = "unknown"

	rootCmd = &cobra.Command{
		Use:     project,
		Short:   "Manage Dead Man's switches",
		Version: version,
	}
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("%s %s (commit: %s, built: %s)\n", project, version, commit, date)
	},
}

// define config opts to be used by cobra + viber for configuration
type flagDef struct {
	Name      string
	Shorthand string
	Type      string // "bool", "string", "stringArray", "int"
	Default   interface{}
	Usage     string
	ViperKey  string
}

// RegisterFlagTypes registers flags on the provided cobra command according
// to the provided definitions.
func RegisterFlagTypes(cmd *cobra.Command, defs []flagDef) {
	for _, d := range defs {
		switch d.Type {
		case "bool":
			cmd.Flags().BoolP(d.Name, d.Shorthand, d.Default.(bool), d.Usage)
		case "duration":
			cmd.Flags().DurationP(d.Name, d.Shorthand, d.Default.(time.Duration), d.Usage)
		case "int":
			cmd.Flags().IntP(d.Name, d.Shorthand, d.Default.(int), d.Usage)
		case "string":
			cmd.Flags().StringP(d.Name, d.Shorthand, d.Default.(string), d.Usage)
		case "stringArray":
			cmd.Flags().StringArrayP(d.Name, d.Shorthand, d.Default.([]string), d.Usage)
		}
	}
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
