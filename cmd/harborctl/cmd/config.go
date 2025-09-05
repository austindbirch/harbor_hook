package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/austindbirch/harbor_hook/cmd/harborctl/cmd/ascii"
	webhookv1 "github.com/austindbirch/harbor_hook/protogen/go/api/webhook/v1"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// configCmd represents the config command
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage harborctl configuration",
	Long:  `Manage harborctl configuration settings.`,
	Annotations: map[string]string{
		ascii.AnnotationKey: ascii.Config,
	},
}

// configViewCmd represents the config view command
var configViewCmd = &cobra.Command{
	Use:   "view",
	Short: "View current configuration",
	Long:  `Display the current configuration settings.`,
	Run: func(cmd *cobra.Command, args []string) {
		if outputJSON {
			config := map[string]interface{}{
				"server":  viper.GetString("server"),
				"timeout": viper.GetDuration("timeout").String(),
				"http":    viper.GetBool("http"),
				"json":    viper.GetBool("json"),
				"pretty":  viper.GetBool("pretty"),
			}
			printOutput(config)
		} else {
			fmt.Println("Current configuration:")
			fmt.Printf("  Server: %s\n", viper.GetString("server"))
			fmt.Printf("  Timeout: %s\n", viper.GetDuration("timeout"))
			fmt.Printf("  Use HTTP: %v\n", viper.GetBool("http"))
			fmt.Printf("  JSON Output: %v\n", viper.GetBool("json"))
			fmt.Printf("  Pretty JSON: %v\n", viper.GetBool("pretty"))

			if viper.GetBool("pretty") && !checkJQAvailable() {
				fmt.Printf("  ⚠️  Warning: pretty=true but jq not found in PATH\n")
			}

			if viper.ConfigFileUsed() != "" {
				fmt.Printf("  Config file: %s\n", viper.ConfigFileUsed())
			} else {
				fmt.Println("  Config file: none (using defaults)")
			}
		}
	},
}

// configSetCmd represents the config set command
var configSetCmd = &cobra.Command{
	Use:   "set [key] [value]",
	Short: "Set a configuration value",
	Long: `Set a configuration value and save it to the config file.
	
Examples:
  harborctl config set server localhost:8080
  harborctl config set timeout 60s
  harborctl config set http true
  harborctl config set pretty true`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := args[0]
		value := args[1]

		// Validate the key
		validKeys := map[string]bool{
			"server":  true,
			"timeout": true,
			"http":    true,
			"json":    true,
			"pretty":  true,
		}

		if !validKeys[key] {
			return fmt.Errorf("invalid configuration key: %s. Valid keys are: server, timeout, http, json, pretty", key)
		}

		// Special handling for pretty - warn if jq is not available
		if key == "pretty" && (value == "true" || value == "1") && !checkJQAvailable() {
			fmt.Printf("⚠️  Warning: jq not found in PATH. Pretty formatting will fall back to standard formatting.\n")
			fmt.Printf("To install jq: https://jqlang.github.io/jq/download/\n\n")
		}

		// Handle boolean values properly
		switch key {
		case "http", "json", "pretty":
			switch value {
			case "true", "1", "yes", "on":
				viper.Set(key, true)
			case "false", "0", "no", "off":
				viper.Set(key, false)
			default:
				return fmt.Errorf("invalid boolean value for %s: %s (use true/false)", key, value)
			}
		case "timeout":
			// Parse duration
			if dur, err := time.ParseDuration(value); err == nil {
				viper.Set(key, dur)
			} else {
				viper.Set(key, value)
			}
		default:
			viper.Set(key, value)
		}

		// Ensure config directory exists
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}

		configPath := filepath.Join(home, ".harborctl.yaml")

		if err := viper.WriteConfigAs(configPath); err != nil {
			return fmt.Errorf("failed to write config file: %w", err)
		}

		fmt.Printf("Set %s = %s\n", key, value)
		fmt.Printf("Configuration saved to: %s\n", configPath)

		return nil
	},
}

// configInitCmd represents the config init command
var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize configuration file",
	Long:  `Create a default configuration file in the home directory.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}

		configPath := filepath.Join(home, ".harborctl.yaml")

		// Check if config file already exists
		if _, err := os.Stat(configPath); err == nil {
			overwrite, _ := cmd.Flags().GetBool("force")
			if !overwrite {
				return fmt.Errorf("config file already exists at %s (use --force to overwrite)", configPath)
			}
		}

		// Set default values
		viper.Set("server", "localhost:8080")
		viper.Set("timeout", "30s")
		viper.Set("http", false)
		viper.Set("json", false)
		viper.Set("pretty", false)

		if err := viper.WriteConfigAs(configPath); err != nil {
			return fmt.Errorf("failed to create config file: %w", err)
		}

		fmt.Printf("Configuration file created: %s\n", configPath)
		fmt.Println("Default settings:")
		fmt.Println("  server: localhost:8080")
		fmt.Println("  timeout: 30s")
		fmt.Println("  http: false")
		fmt.Println("  json: false")
		fmt.Println("  pretty: false")

		return nil
	},
}

// configCheckCmd represents the config check command
var configCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Check configuration and dependencies",
	Long:  `Check the current configuration and verify that dependencies like jq are available.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Configuration check:")
		fmt.Printf("  ✅ harborctl version: %s\n", Version)

		if viper.ConfigFileUsed() != "" {
			fmt.Printf("  ✅ Config file: %s\n", viper.ConfigFileUsed())
		} else {
			fmt.Printf("  ⚠️  Config file: not found (using defaults)\n")
		}

		if checkJQAvailable() {
			fmt.Printf("  ✅ jq: available\n")
		} else {
			fmt.Printf("  ❌ jq: not found in PATH\n")
			fmt.Printf("     Install from: https://jqlang.github.io/jq/download/\n")
		}

		fmt.Printf("  ✅ Server: %s\n", viper.GetString("server"))

		if viper.GetBool("pretty") && !checkJQAvailable() {
			fmt.Printf("  ⚠️  Pretty formatting enabled but jq not available\n")
		}

		fmt.Println("\nTesting server connectivity...")
		if err := func() error {
			client, cleanup, err := getClient()
			if err != nil {
				return err
			}
			defer cleanup()

			ctx := context.Background()
			_, err = client.Ping(ctx, &webhookv1.PingRequest{})
			return err
		}(); err != nil {
			fmt.Printf("  ❌ Server connectivity: %v\n", err)
		} else {
			fmt.Printf("  ✅ Server connectivity: OK\n")
		}
	},
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configViewCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configInitCmd)
	configCmd.AddCommand(configCheckCmd)

	// Flags for init command
	configInitCmd.Flags().Bool("force", false, "overwrite existing config file")
}
