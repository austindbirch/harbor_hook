package main

import (
	"fmt"

	"github.com/austindbirch/harbor_hook/cmd/harborctl/cmd/ascii"
	"github.com/spf13/cobra"
)

// Example showing how to access ASCII art from command annotations
func main() {
	// Create a sample command with ASCII art annotation
	cmd := &cobra.Command{
		Use:   "example",
		Short: "Example command",
		Annotations: map[string]string{
			ascii.AnnotationKey: ascii.Event,
		},
	}

	// Access the ASCII art from annotations
	if art, exists := cmd.Annotations[ascii.AnnotationKey]; exists {
		fmt.Println("ASCII Art for this command:")
		fmt.Println(art)
	}

	fmt.Println("\nAll available ASCII art:")
	fmt.Printf("Root: %s\n", ascii.Root)
	fmt.Printf("Ping: %s\n", ascii.Ping)
	fmt.Printf("Event: %s\n", ascii.Event)
	fmt.Printf("Delivery: %s\n", ascii.Delivery)
	fmt.Printf("Endpoint: %s\n", ascii.Endpoint)
	fmt.Printf("Subscription: %s\n", ascii.Subscription)
	fmt.Printf("Health: %s\n", ascii.Health)
	fmt.Printf("Version: %s\n", ascii.Version)
	fmt.Printf("Config: %s\n", ascii.Config)
	fmt.Printf("Completion: %s\n", ascii.Completion)
	fmt.Printf("Quick: %s\n", ascii.Quick)
}
