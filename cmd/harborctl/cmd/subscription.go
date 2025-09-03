package cmd

import (
	"context"
	"fmt"

	"github.com/austindbirch/harbor_hook/cmd/harborctl/cmd/ascii"
	webhookv1 "github.com/austindbirch/harbor_hook/protogen/go/api/webhook/v1"
	"github.com/spf13/cobra"
)

// subscriptionCmd represents the subscription command
var subscriptionCmd = &cobra.Command{
	Use:   "subscription",
	Short: "Manage webhook subscriptions",
	Long:  `Create and manage webhook subscriptions that link endpoints to event types.`,
	Annotations: map[string]string{
		ascii.AnnotationKey: ascii.Subscription,
	},
}

// createSubscriptionCmd represents the create subscription command
var createSubscriptionCmd = &cobra.Command{
	Use:   "create [tenant-id] [endpoint-id] [event-type]",
	Short: "Create a new webhook subscription",
	Long: `Create a new webhook subscription linking an endpoint to an event type.
	
Example:
  harborctl subscription create tn_123 ep_456 appointment.created`,
	Args: cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		tenantID := args[0]
		endpointID := args[1]
		eventType := args[2]

		client, cleanup, err := getClient()
		if err != nil {
			return fmt.Errorf("failed to connect: %w", err)
		}
		defer cleanup()

		ctx := context.Background()
		req := &webhookv1.CreateSubscriptionRequest{
			TenantId:   tenantID,
			EndpointId: endpointID,
			EventType:  eventType,
		}

		resp, err := client.CreateSubscription(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to create subscription: %w", err)
		}

		if outputJSON {
			printOutput(resp)
		} else {
			fmt.Printf("Created subscription: %s\n", resp.Subscription.Id)
			fmt.Printf("  Tenant ID: %s\n", resp.Subscription.TenantId)
			fmt.Printf("  Endpoint ID: %s\n", resp.Subscription.EndpointId)
			fmt.Printf("  Event Type: %s\n", resp.Subscription.EventType)
			fmt.Printf("  Created: %s\n", resp.Subscription.CreatedAt.AsTime().Format("2006-01-02 15:04:05"))
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(subscriptionCmd)
	subscriptionCmd.AddCommand(createSubscriptionCmd)
}
