package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/austindbirch/harbor_hook/cmd/harborctl/cmd/ascii"
	webhookv1 "github.com/austindbirch/harbor_hook/protogen/go/api/webhook/v1"
	"github.com/spf13/cobra"
)

// endpointCmd represents the endpoint command
var endpointCmd = &cobra.Command{
	Use:   "endpoint",
	Short: "Manage webhook endpoints",
	Long:  `Create and manage webhook endpoints that will receive event deliveries.`,
	Annotations: map[string]string{
		ascii.AnnotationKey: ascii.Endpoint,
	},
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

		if useHTTP {
			payload := map[string]interface{}{
				"url": url,
			}
			if secret != "" {
				payload["secret"] = secret
			}

			resp, err := makeHTTPRequest("POST", fmt.Sprintf("/v1/tenants/%s/endpoints", tenantID), payload)
			if err != nil {
				return fmt.Errorf("HTTP request failed: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != 200 {
				return fmt.Errorf("HTTP error: %s", resp.Status)
			}

			var result map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			printOutput(result)
			return nil
		}

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
