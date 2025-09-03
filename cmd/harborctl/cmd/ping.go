package cmd

import (
	"context"
	"fmt"

	"github.com/austindbirch/harbor_hook/cmd/harborctl/cmd/ascii"
	webhookv1 "github.com/austindbirch/harbor_hook/protogen/go/api/webhook/v1"
	"github.com/spf13/cobra"
)

// pingCmd represents the ping command
var pingCmd = &cobra.Command{
	Use:   "ping",
	Short: "Ping the Harbor Hook service",
	Long:  `Send a ping request to verify the Harbor Hook service is running and accessible.`,
	Annotations: map[string]string{
		ascii.AnnotationKey: ascii.Ping,
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if useHTTP {
			resp, err := makeHTTPRequest("GET", "/v1/ping", nil)
			if err != nil {
				return fmt.Errorf("HTTP request failed: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != 200 {
				return fmt.Errorf("HTTP error: %s", resp.Status)
			}

			fmt.Println("Pong! Service is running (HTTP)")
			return nil
		}

		client, cleanup, err := getClient()
		if err != nil {
			return fmt.Errorf("failed to connect: %w", err)
		}
		defer cleanup()

		ctx := context.Background()
		resp, err := client.Ping(ctx, &webhookv1.PingRequest{})
		if err != nil {
			return fmt.Errorf("ping failed: %w", err)
		}

		if outputJSON {
			printOutput(resp)
		} else {
			fmt.Printf("Pong! Service is running: %s\n", resp.Message)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(pingCmd)
}
