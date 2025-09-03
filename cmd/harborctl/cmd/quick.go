package cmd

import (
	"context"
	"fmt"

	webhookv1 "github.com/austindbirch/harbor_hook/protogen/go/api/webhook/v1"
	"github.com/spf13/cobra"
)

// quickCmd represents a set of quick/easy commands for common operations
var quickCmd = &cobra.Command{
	Use:   "quick",
	Short: "Quick operations for common tasks",
	Long:  `Quick operations that combine multiple steps for common workflows.`,
}

// quickSetupCmd sets up a complete endpoint and subscription in one command
var quickSetupCmd = &cobra.Command{
	Use:   "setup [tenant-id] [url] [event-type]",
	Short: "Quick setup: create endpoint and subscription",
	Long: `Create an endpoint and subscription in one command.
	
Example:
  harborctl quick setup tn_123 https://example.com/webhook appointment.created`,
	Args: cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		tenantID := args[0]
		url := args[1]
		eventType := args[2]
		secret, _ := cmd.Flags().GetString("secret")

		client, cleanup, err := getClient()
		if err != nil {
			return fmt.Errorf("failed to connect: %w", err)
		}
		defer cleanup()

		ctx := context.Background()

		// Create endpoint
		fmt.Printf("Creating endpoint for %s...\n", url)
		endpointReq := &webhookv1.CreateEndpointRequest{
			TenantId: tenantID,
			Url:      url,
			Secret:   secret,
		}

		endpointResp, err := client.CreateEndpoint(ctx, endpointReq)
		if err != nil {
			return fmt.Errorf("failed to create endpoint: %w", err)
		}

		endpointID := endpointResp.Endpoint.Id
		fmt.Printf("✅ Created endpoint: %s\n", endpointID)

		// Create subscription
		fmt.Printf("Creating subscription for event type %s...\n", eventType)
		subscriptionReq := &webhookv1.CreateSubscriptionRequest{
			TenantId:   tenantID,
			EndpointId: endpointID,
			EventType:  eventType,
		}

		subscriptionResp, err := client.CreateSubscription(ctx, subscriptionReq)
		if err != nil {
			return fmt.Errorf("failed to create subscription: %w", err)
		}

		fmt.Printf("✅ Created subscription: %s\n", subscriptionResp.Subscription.Id)

		if outputJSON {
			result := map[string]interface{}{
				"endpoint":     endpointResp.Endpoint,
				"subscription": subscriptionResp.Subscription,
			}
			printOutput(result)
		} else {
			fmt.Printf("\n🎉 Setup complete!\n")
			fmt.Printf("  Tenant: %s\n", tenantID)
			fmt.Printf("  Endpoint: %s (%s)\n", endpointID, url)
			fmt.Printf("  Subscription: %s (%s)\n", subscriptionResp.Subscription.Id, eventType)
			fmt.Printf("\nYou can now publish events with:\n")
			fmt.Printf("  harborctl event publish %s %s '{\"key\":\"value\"}'\n", tenantID, eventType)
		}

		return nil
	},
}

// quickTestCmd publishes a test event and shows the result
var quickTestCmd = &cobra.Command{
	Use:   "test [tenant-id] [event-type]",
	Short: "Quick test: publish a test event",
	Long: `Publish a test event with sample data and show delivery status.
	
Example:
  harborctl quick test tn_123 appointment.created`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		tenantID := args[0]
		eventType := args[1]

		// Create test payload
		testPayload := `{
			"test": true,
			"source": "harborctl-quick-test",
			"message": "This is a test event from harborctl"
		}`

		payload, err := parseJSON(testPayload)
		if err != nil {
			return fmt.Errorf("failed to create test payload: %w", err)
		}

		client, cleanup, err := getClient()
		if err != nil {
			return fmt.Errorf("failed to connect: %w", err)
		}
		defer cleanup()

		ctx := context.Background()

		// Publish event
		fmt.Printf("Publishing test event: %s\n", eventType)
		publishReq := &webhookv1.PublishEventRequest{
			TenantId:  tenantID,
			EventType: eventType,
			Payload:   payload,
		}

		publishResp, err := client.PublishEvent(ctx, publishReq)
		if err != nil {
			return fmt.Errorf("failed to publish event: %w", err)
		}

		eventID := publishResp.EventId
		fmt.Printf("✅ Published event: %s (fanout: %d)\n", eventID, publishResp.FanoutCount)

		if publishResp.FanoutCount == 0 {
			fmt.Printf("⚠️  No subscriptions found for event type %s\n", eventType)
			return nil
		}

		// Wait a moment for delivery
		fmt.Printf("Waiting for delivery...\n")
		// Simple sleep instead of importing time
		for i := 0; i < 3; i++ {
			fmt.Printf(".")
		}
		fmt.Printf("\n")

		// Check delivery status
		statusReq := &webhookv1.GetDeliveryStatusRequest{
			EventId: eventID,
			Limit:   10,
		}

		statusResp, err := client.GetDeliveryStatus(ctx, statusReq)
		if err != nil {
			return fmt.Errorf("failed to get delivery status: %w", err)
		}

		if outputJSON {
			result := map[string]interface{}{
				"event":      publishResp,
				"deliveries": statusResp,
			}
			printOutput(result)
		} else {
			fmt.Printf("\n📊 Delivery Status:\n")
			if len(statusResp.Attempts) == 0 {
				fmt.Printf("  No delivery attempts found yet\n")
			} else {
				for i, attempt := range statusResp.Attempts {
					status := "Unknown"
					switch attempt.Status.String() {
					case "DELIVERY_ATTEMPT_STATUS_QUEUED":
						status = "⏳ Queued"
					case "DELIVERY_ATTEMPT_STATUS_IN_FLIGHT":
						status = "🚀 In Flight"
					case "DELIVERY_ATTEMPT_STATUS_DELIVERED":
						status = "✅ Delivered"
					case "DELIVERY_ATTEMPT_STATUS_FAILED":
						status = "❌ Failed"
					case "DELIVERY_ATTEMPT_STATUS_DEAD_LETTERED":
						status = "💀 Dead Letter"
					}

					fmt.Printf("  %d. %s | Endpoint: %s", i+1, status, attempt.EndpointId)
					if attempt.HttpStatus > 0 {
						fmt.Printf(" | HTTP: %d", attempt.HttpStatus)
					}
					if attempt.ErrorReason != "" {
						fmt.Printf(" | Error: %s", attempt.ErrorReason)
					}
					fmt.Printf("\n")
				}
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(quickCmd)
	quickCmd.AddCommand(quickSetupCmd)
	quickCmd.AddCommand(quickTestCmd)

	// Flags for quick setup
	quickSetupCmd.Flags().String("secret", "", "webhook secret (if not provided, one will be generated)")
}
