package cmd

import (
	"context"
	"fmt"

	webhookv1 "github.com/austindbirch/harbor_hook/protogen/go/api/webhook/v1"
	"github.com/spf13/cobra"
)

// deliveryCmd represents the delivery command
var deliveryCmd = &cobra.Command{
	Use:   "delivery",
	Short: "Manage webhook deliveries",
	Long:  `Check delivery status, replay deliveries, and manage the dead letter queue.`,
}

// statusCmd represents the status command
var statusCmd = &cobra.Command{
	Use:   "status [event-id]",
	Short: "Get delivery status for an event",
	Long: `Get the delivery status and attempts for a specific event.
	
Example:
  harborctl delivery status evt_123`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		eventID := args[0]

		endpointID, _ := cmd.Flags().GetString("endpoint-id")
		fromStr, _ := cmd.Flags().GetString("from")
		toStr, _ := cmd.Flags().GetString("to")
		limitStr, _ := cmd.Flags().GetString("limit")

		// Parse optional parameters
		from, err := parseTimestamp(fromStr)
		if err != nil {
			return fmt.Errorf("invalid 'from' timestamp: %w", err)
		}

		to, err := parseTimestamp(toStr)
		if err != nil {
			return fmt.Errorf("invalid 'to' timestamp: %w", err)
		}

		limit, err := parseInt32(limitStr)
		if err != nil {
			return fmt.Errorf("invalid limit: %w", err)
		}

		client, cleanup, err := getClient()
		if err != nil {
			return fmt.Errorf("failed to connect: %w", err)
		}
		defer cleanup()

		ctx := context.Background()
		req := &webhookv1.GetDeliveryStatusRequest{
			EventId:    eventID,
			EndpointId: endpointID,
			From:       from,
			To:         to,
			Limit:      limit,
		}

		resp, err := client.GetDeliveryStatus(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to get delivery status: %w", err)
		}

		if outputJSON {
			printOutput(resp)
		} else {
			fmt.Printf("Delivery attempts for event %s:\n", eventID)
			if len(resp.Attempts) == 0 {
				fmt.Println("  No delivery attempts found")
				return nil
			}

			for i, attempt := range resp.Attempts {
				fmt.Printf("\n  Attempt %d:\n", i+1)
				fmt.Printf("    Delivery ID: %s\n", attempt.DeliveryId)
				fmt.Printf("    Endpoint ID: %s\n", attempt.EndpointId)
				fmt.Printf("    Status: %s\n", attempt.Status.String())
				if attempt.HttpStatus > 0 {
					fmt.Printf("    HTTP Status: %d\n", attempt.HttpStatus)
				}
				if attempt.ErrorReason != "" {
					fmt.Printf("    Error: %s\n", attempt.ErrorReason)
				}
				if attempt.ReplayOf != "" {
					fmt.Printf("    Replay of: %s\n", attempt.ReplayOf)
				}
				if attempt.EnqueuedAt != nil {
					fmt.Printf("    Enqueued: %s\n", attempt.EnqueuedAt.AsTime().Format("2006-01-02 15:04:05"))
				}
				if attempt.DeliveredAt != nil {
					fmt.Printf("    Delivered: %s\n", attempt.DeliveredAt.AsTime().Format("2006-01-02 15:04:05"))
				}
				if attempt.FailedAt != nil {
					fmt.Printf("    Failed: %s\n", attempt.FailedAt.AsTime().Format("2006-01-02 15:04:05"))
				}
				if attempt.DlqAt != nil {
					fmt.Printf("    Dead Lettered: %s\n", attempt.DlqAt.AsTime().Format("2006-01-02 15:04:05"))
				}
			}
		}

		return nil
	},
}

// replayCmd represents the replay command
var replayCmd = &cobra.Command{
	Use:   "replay [delivery-id]",
	Short: "Replay a failed delivery",
	Long: `Replay a specific delivery attempt by creating a new delivery task.
	
Example:
  harborctl delivery replay del_456 --reason "endpoint was down"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		deliveryID := args[0]
		reason, _ := cmd.Flags().GetString("reason")

		client, cleanup, err := getClient()
		if err != nil {
			return fmt.Errorf("failed to connect: %w", err)
		}
		defer cleanup()

		ctx := context.Background()
		req := &webhookv1.ReplayDeliveryRequest{
			DeliveryId: deliveryID,
			Reason:     reason,
		}

		resp, err := client.ReplayDelivery(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to replay delivery: %w", err)
		}

		if outputJSON {
			printOutput(resp)
		} else {
			fmt.Printf("Replayed delivery: %s\n", resp.NewAttempt.DeliveryId)
			fmt.Printf("  Event ID: %s\n", resp.NewAttempt.EventId)
			fmt.Printf("  Endpoint ID: %s\n", resp.NewAttempt.EndpointId)
			fmt.Printf("  Status: %s\n", resp.NewAttempt.Status.String())
			fmt.Printf("  Replay of: %s\n", resp.NewAttempt.ReplayOf)
		}

		return nil
	},
}

// dlqCmd represents the dlq command
var dlqCmd = &cobra.Command{
	Use:   "dlq",
	Short: "List dead letter queue entries",
	Long: `List all delivery attempts currently in the dead letter queue.
	
Example:
  harborctl delivery dlq --limit 20`,
	RunE: func(cmd *cobra.Command, args []string) error {
		endpointID, _ := cmd.Flags().GetString("endpoint-id")
		limitStr, _ := cmd.Flags().GetString("limit")

		limit, err := parseInt32(limitStr)
		if err != nil {
			return fmt.Errorf("invalid limit: %w", err)
		}

		client, cleanup, err := getClient()
		if err != nil {
			return fmt.Errorf("failed to connect: %w", err)
		}
		defer cleanup()

		ctx := context.Background()
		req := &webhookv1.ListDLQRequest{
			EndpointId: endpointID,
			Limit:      limit,
		}

		resp, err := client.ListDLQ(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to list DLQ: %w", err)
		}

		if outputJSON {
			printOutput(resp)
		} else {
			fmt.Println("Dead Letter Queue entries:")
			if len(resp.Dead) == 0 {
				fmt.Println("  No entries found")
				return nil
			}

			for i, attempt := range resp.Dead {
				fmt.Printf("\n  Entry %d:\n", i+1)
				fmt.Printf("    Delivery ID: %s\n", attempt.DeliveryId)
				fmt.Printf("    Event ID: %s\n", attempt.EventId)
				fmt.Printf("    Endpoint ID: %s\n", attempt.EndpointId)
				fmt.Printf("    Status: %s\n", attempt.Status.String())
				if attempt.HttpStatus > 0 {
					fmt.Printf("    Last HTTP Status: %d\n", attempt.HttpStatus)
				}
				if attempt.ErrorReason != "" {
					fmt.Printf("    Error: %s\n", attempt.ErrorReason)
				}
				if attempt.DlqAt != nil {
					fmt.Printf("    Dead Lettered: %s\n", attempt.DlqAt.AsTime().Format("2006-01-02 15:04:05"))
				}
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(deliveryCmd)
	deliveryCmd.AddCommand(statusCmd)
	deliveryCmd.AddCommand(replayCmd)
	deliveryCmd.AddCommand(dlqCmd)

	// Flags for status command
	statusCmd.Flags().String("endpoint-id", "", "filter by endpoint ID")
	statusCmd.Flags().String("from", "", "start time (RFC3339 format)")
	statusCmd.Flags().String("to", "", "end time (RFC3339 format)")
	statusCmd.Flags().String("limit", "10", "maximum number of results")

	// Flags for replay command
	replayCmd.Flags().String("reason", "", "reason for replaying the delivery")

	// Flags for dlq command
	dlqCmd.Flags().String("endpoint-id", "", "filter by endpoint ID")
	dlqCmd.Flags().String("limit", "10", "maximum number of results")
}
