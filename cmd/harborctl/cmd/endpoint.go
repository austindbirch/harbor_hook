package cmd

import (
	"context"
	"fmt"

	webhookv1 "github.com/austindbirch/harbor_hook/protogen/go/api/webhook/v1"
	"github.com/spf13/cobra"
)

// endpointCmd represents the endpoint command
var endpointCmd = &cobra.Command{
	Use:   "endpoint",
	Short: "Manage webhook endpoints",
	Long:  `Create and manage webhook endpoints that will receive event deliveries.`,
}

// createEndpointCmd represents the create endpoint command
var createEndpointCmd = &cobra.Command{
	Use:   "create [tenant-id] [url]",
	Short: "Create a new webhook endpoint",
	Long: `Create a new webhook endpoint for a tenant.
	
Example:
  harborctl endpoint create tn_123 https://example.com/webhook`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		tenantID := args[0]
		url := args[1]
		secret, _ := cmd.Flags().GetString("secret")

		client, cleanup, err := getClient()
		if err != nil {
			return fmt.Errorf("failed to connect: %w", err)
		}
		defer cleanup()

		ctx := context.Background()
		req := &webhookv1.CreateEndpointRequest{
			TenantId: tenantID,
			Url:      url,
			Secret:   secret,
		}

		resp, err := client.CreateEndpoint(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to create endpoint: %w", err)
		}

		if outputJSON {
			printOutput(resp)
		} else {
			fmt.Printf("Created endpoint: %s\n", resp.Endpoint.Id)
			fmt.Printf("  Tenant ID: %s\n", resp.Endpoint.TenantId)
			fmt.Printf("  URL: %s\n", resp.Endpoint.Url)
			fmt.Printf("  Created: %s\n", resp.Endpoint.CreatedAt.AsTime().Format("2006-01-02 15:04:05"))
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(endpointCmd)
	endpointCmd.AddCommand(createEndpointCmd)

	// Flags for create endpoint
	createEndpointCmd.Flags().String("secret", "", "webhook secret (if not provided, one will be generated)")
}
