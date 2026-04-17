// Package cmd hosts the cobra command tree for the tavily binary.
package cmd

import (
	"context"

	"github.com/spf13/cobra"
)

// Persistent flags shared by every subcommand.
var (
	flagPretty     bool
	flagVerbose    bool
	flagQuiet      bool
	flagOut        string
	flagMaxRetries int
)

// rootCmd is the base command. Running `tavily` with no args prints help.
var rootCmd = &cobra.Command{
	Use:   "tavily",
	Short: "Thin CLI wrapper for the Tavily search API",
	Long:  "tavily is a minimal, agent-friendly wrapper for the Tavily search API. JSON to stdout, progress to stderr.",
	// Silence cobra's own error printing so we can control stderr formatting;
	// Execute() still returns the error and main() maps it to an exit code.
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute is called by main. Returns the error; main decides the exit code.
func Execute() error {
	return rootCmd.Execute()
}

// ExecuteContext runs the command tree with the supplied context so signals
// propagate into in-flight HTTP requests.
func ExecuteContext(ctx context.Context) error {
	return rootCmd.ExecuteContext(ctx)
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&flagPretty, "pretty", false, "Indent JSON output")
	rootCmd.PersistentFlags().BoolVar(&flagVerbose, "verbose", false, "Verbose progress to stderr")
	rootCmd.PersistentFlags().BoolVar(&flagQuiet, "quiet", false, "Suppress non-error stderr output")
	rootCmd.PersistentFlags().StringVar(&flagOut, "out", "", "Write JSON output to this file instead of stdout")
	rootCmd.PersistentFlags().IntVar(&flagMaxRetries, "max-retries", 3, "Max retries on 429/5xx with exponential backoff")
}
