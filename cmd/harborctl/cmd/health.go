package cmd

import (
	"context"
	"fmt"
	"time"

	webhookv1 "github.com/austindbirch/harbor_hook/protogen/go/api/webhook/v1"
	"github.com/spf13/cobra"
)

// healthCmd represents the health command
var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Check the health of the Harbor Hook service",
	Long:  `Check the health status of the Harbor Hook service using gRPC health checks.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if useHTTP {
			resp, err := makeHTTPRequest("GET", "/healthz", nil)
			if err != nil {
				return fmt.Errorf("HTTP health check failed: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode == 200 {
				fmt.Println("✓ Service is healthy (HTTP)")
			} else {
				fmt.Printf("✗ Service is unhealthy (HTTP %d)\n", resp.StatusCode)
			}
			return nil
		}

		client, cleanup, err := getClient()
		if err != nil {
			return fmt.Errorf("failed to connect: %w", err)
		}
		defer cleanup()

		// For gRPC health check, we need to use the health service client
		// This requires importing the health service
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// For now, just use ping as health check since we don't have health service imported
		// In a full implementation, you'd use grpc_health_v1.HealthClient
		_, err = client.Ping(ctx, &webhookv1.PingRequest{})
		if err != nil {
			fmt.Printf("✗ Service is unhealthy: %v\n", err)
			return nil
		}

		fmt.Println("✓ Service is healthy")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(healthCmd)
}
