package cmd

import (
	"fmt"
	"runtime"

	"github.com/austindbirch/harbor_hook/cmd/harborctl/cmd/ascii"
	"github.com/spf13/cobra"
)

var (
	// These will be set by ldflags during build
	Version   = "dev"
	GitCommit = "unknown"
	BuildTime = "unknown"
)

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version information",
	Long:  `Print the version information for harborctl.`,
	Annotations: map[string]string{
		ascii.AnnotationKey: ascii.Version,
	},
	Run: func(cmd *cobra.Command, args []string) {
		if outputJSON {
			version := map[string]string{
				"version":   Version,
				"gitCommit": GitCommit,
				"buildTime": BuildTime,
				"goVersion": runtime.Version(),
				"goos":      runtime.GOOS,
				"goarch":    runtime.GOARCH,
			}
			printOutput(version)
		} else {
			fmt.Printf("harborctl version %s\n", Version)
			fmt.Printf("Git commit: %s\n", GitCommit)
			fmt.Printf("Built: %s\n", BuildTime)
			fmt.Printf("Go version: %s\n", runtime.Version())
			fmt.Printf("OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
		}
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
