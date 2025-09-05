package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/austindbirch/harbor_hook/cmd/harborctl/cmd/ascii"
	webhookv1 "github.com/austindbirch/harbor_hook/protogen/go/api/webhook/v1"
	"github.com/spf13/cobra"
)

// eventCmd represents the event command
var eventCmd = &cobra.Command{
	Use:   "event",
	Short: "Manage webhook events",
	Long:  `Publish webhook events and manage event deliveries.`,
	Annotations: map[string]string{
		ascii.AnnotationKey: ascii.Event,
	},
}

// publishCmd represents the publish command
var publishCmd = &cobra.Command{
	Use:   "publish [tenant-id] [event-type] [payload-json]",
	Short: "Publish a webhook event",
	Long: `Publish a webhook event with a JSON payload.
	
Example:
  harborctl event publish tn_123 appointment.created '{"id":"apt_789","patient":"John Doe"}'`,
	Args: cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		tenantID := args[0]
		eventType := args[1]
		payloadJSON := args[2]

		idempotencyKey, _ := cmd.Flags().GetString("idempotency-key")

		// Parse the JSON payload
		payload, err := parseJSON(payloadJSON)
		if err != nil {
			return fmt.Errorf("invalid payload JSON: %w", err)
		}

		if useHTTP {
			httpPayload := map[string]interface{}{
				"eventType": eventType,
				"payload":   payload,
			}
			if idempotencyKey != "" {
				httpPayload["idempotencyKey"] = idempotencyKey
			}

			resp, err := makeHTTPRequest("POST", fmt.Sprintf("/v1/tenants/%s/events:publish", tenantID), httpPayload)
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
		req := &webhookv1.PublishEventRequest{
			TenantId:       tenantID,
			EventType:      eventType,
			Payload:        payload,
			IdempotencyKey: idempotencyKey,
		}

		resp, err := client.PublishEvent(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to publish event: %w", err)
		}

		if outputJSON {
			printOutput(resp)
		} else {
			fmt.Printf("Published event: %s\n", resp.EventId)
			fmt.Printf("  Fanout count: %d\n", resp.FanoutCount)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(eventCmd)
	eventCmd.AddCommand(publishCmd)

	// Flags for publish
	publishCmd.Flags().String("idempotency-key", "", "idempotency key for deduplication")
}
