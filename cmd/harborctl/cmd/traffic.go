package cmd

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/austindbirch/harbor_hook/cmd/harborctl/cmd/ascii"
	"github.com/spf13/cobra"
)

// TrafficConfig holds the configuration for traffic generation
type TrafficConfig struct {
	Duration      int     `json:"duration"`
	Volume        int     `json:"volume"`
	TenantID      string  `json:"tenant_id"`
	WebhookURL    string  `json:"webhook_url"`
	EventType     string  `json:"event_type"`
	ServerHost    string  `json:"server_host"`
	JWKSHost      string  `json:"jwks_host"`
	Mode          string  `json:"mode"`
	FailureRate   float64 `json:"failure_rate"`   // Percentage of requests that should fail (0-100)
	Burst         bool    `json:"burst"`          // Whether to generate burst traffic after normal traffic
	BurstVolume   int     `json:"burst_volume"`   // Requests per second during burst (default: 50)
	BurstDuration int     `json:"burst_duration"` // Duration of burst in seconds (default: 30)
}

// TrafficSummary holds the summary of generated traffic
type TrafficSummary struct {
	TotalRequests     int           `json:"total_requests"`
	SuccessRequests   int           `json:"success_requests"`
	FailedRequests    int           `json:"failed_requests"`
	GoodEndpointReqs  int           `json:"good_endpoint_requests"` // Sent to good endpoint
	BadEndpointReqs   int           `json:"bad_endpoint_requests"`  // Sent to bad endpoint
	NormalRequests    int           `json:"normal_requests"`        // Normal traffic phase
	BurstRequests     int           `json:"burst_requests"`         // Burst traffic phase
	TotalDuration     time.Duration `json:"total_duration"`         // Total time including burst
	NormalDuration    time.Duration `json:"normal_duration"`        // Normal traffic duration
	BurstDuration     time.Duration `json:"burst_duration"`         // Burst traffic duration
	NormalRPS         float64       `json:"normal_rps"`             // RPS during normal phase
	BurstRPS          float64       `json:"burst_rps"`              // RPS during burst phase
	OverallRPS        float64       `json:"overall_rps"`            // Overall RPS
	EndpointID        string        `json:"endpoint_id"`
	BadEndpointID     string        `json:"bad_endpoint_id"`
	SubscriptionID    string        `json:"subscription_id"`
	BadSubscriptionID string        `json:"bad_subscription_id"`
	Mode              string        `json:"mode"`
	HadBurst          bool          `json:"had_burst"` // Whether burst traffic was generated
}

// trafficCmd represents the traffic command
var trafficCmd = &cobra.Command{
	Use:   "traffic",
	Short: "Generate test traffic for Harborhook",
	Long: `Generate test traffic to demonstrate Harborhook functionality.
This command provides an interactive interface to configure and generate
webhook traffic for testing observability, performance, and functionality.

Supports two modes:
• Good traffic: Successful webhook deliveries for normal testing
• Bad traffic: Failing deliveries to demonstrate DLQ (Dead Letter Queue) behavior`,
	Annotations: map[string]string{
		ascii.AnnotationKey: ascii.Event, // Reuse event ASCII art
	},
}

// generateCmd represents the generate subcommand
var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate test traffic interactively",
	Long: `Start an interactive session to configure and generate test traffic.
You'll be prompted for parameters like duration, volume, tenant ID, etc.

Choose between two traffic modes:
• good: Generates mostly successful traffic with configurable failure rate and optional burst (default: 120s, 10 req/s, 5% failures)
• bad:  Generates DLQ traffic (default: 30s, 5 req/s) for testing failures

After confirmation, the command will generate the specified traffic pattern.`,
	RunE: runGenerateTraffic,
}

func init() {
	rootCmd.AddCommand(trafficCmd)
	trafficCmd.AddCommand(generateCmd)
}

// runGenerateTraffic handles the interactive traffic generation
func runGenerateTraffic(cmd *cobra.Command, args []string) error {
	printHeader("🚀 Harborhook Traffic Generator")

	// Step 1: Collect parameters interactively
	config, err := collectTrafficParameters()
	if err != nil {
		return fmt.Errorf("failed to collect parameters: %w", err)
	}

	// Step 2: Show parameters and get confirmation
	if !confirmParameters(config) {
		printInfo("Traffic generation cancelled")
		return nil
	}

	// Step 3: Check dependencies
	if err := checkTrafficDependencies(); err != nil {
		return err
	}
	printSuccess("All dependencies available")

	// Step 4: Get JWT token
	jwtToken, err := getJWTToken(config.JWKSHost, config.TenantID)
	if err != nil {
		return fmt.Errorf("failed to get JWT token: %w", err)
	}
	printSuccess("Got JWT token")

	// Step 5: Setup endpoints and subscriptions
	endpointID, subscriptionID, badEndpointID, badSubscriptionID, err := setupTrafficEndpoints(config, jwtToken)
	if err != nil {
		return fmt.Errorf("failed to setup endpoints: %w", err)
	}
	printSuccess(fmt.Sprintf("Good Endpoint ID: %s", endpointID))
	printSuccess(fmt.Sprintf("Good Subscription ID: %s", subscriptionID))
	if config.Mode == "good" && config.FailureRate > 0 {
		printSuccess(fmt.Sprintf("Bad Endpoint ID: %s", badEndpointID))
		printSuccess(fmt.Sprintf("Bad Subscription ID: %s", badSubscriptionID))
	}

	// Step 6: Generate traffic
	summary, err := generateTrafficWithProgress(config, jwtToken)
	if err != nil {
		return fmt.Errorf("failed to generate traffic: %w", err)
	}
	summary.EndpointID = endpointID
	summary.SubscriptionID = subscriptionID
	summary.BadEndpointID = badEndpointID
	summary.BadSubscriptionID = badSubscriptionID
	summary.Mode = config.Mode

	// Step 7: Show summary
	printTrafficSummary(summary)

	return nil
}

// collectTrafficParameters interactively collects traffic generation parameters
func collectTrafficParameters() (*TrafficConfig, error) {
	reader := bufio.NewReader(os.Stdin)

	printStep("Configuring traffic generation parameters...")
	fmt.Println()

	// Traffic mode selection first
	fmt.Printf("Traffic mode (good/bad) [default: good]: ")
	mode := "good"
	if input, _ := reader.ReadString('\n'); strings.TrimSpace(input) != "" {
		input = strings.ToLower(strings.TrimSpace(input))
		if input == "bad" || input == "dlq" {
			mode = "bad"
		}
	}

	// Set defaults based on traffic mode
	var config *TrafficConfig
	if mode == "bad" {
		config = &TrafficConfig{
			Duration:   30, // Shorter duration for bad traffic
			Volume:     5,  // Lower volume for bad traffic
			TenantID:   "harborctl_badtraffic",
			WebhookURL: "http://fake-receiver:8081/fail", // Failing endpoint
			EventType:  "harborctl.traffic.failevent",
			ServerHost: "localhost:8443",
			JWKSHost:   "localhost:8082",
			Mode:       "bad",
		}
		//fmt.Printf("🔥 DLQ Mode: Using defaults optimized for Dead Letter Queue testing\n\n")
	} else {
		config = &TrafficConfig{
			Duration:      120,
			Volume:        10,
			TenantID:      "harborctl_traffic",
			WebhookURL:    "http://fake-receiver:8081/hook",
			EventType:     "harborctl.traffic.successevent",
			ServerHost:    "localhost:8443",
			JWKSHost:      "localhost:8082",
			Mode:          "good",
			FailureRate:   5.0,   // 5% failure rate by default
			Burst:         false, // No burst by default
			BurstVolume:   25,    // Reduced from 50 to 25 req/s for stability
			BurstDuration: 30,    // 30 seconds of burst
		}
		//fmt.Printf("✅ Good Mode: Using defaults for mixed success/failure traffic\n\n")
	}

	// Traffic duration
	fmt.Printf("Traffic duration in seconds [default: %d]: ", config.Duration)
	if input, _ := reader.ReadString('\n'); strings.TrimSpace(input) != "" {
		if duration, err := strconv.Atoi(strings.TrimSpace(input)); err == nil && duration > 0 {
			config.Duration = duration
		}
	}

	// Traffic volume
	fmt.Printf("Traffic volume (requests per second) [default: %d]: ", config.Volume)
	if input, _ := reader.ReadString('\n'); strings.TrimSpace(input) != "" {
		if volume, err := strconv.Atoi(strings.TrimSpace(input)); err == nil && volume > 0 {
			config.Volume = volume
		}
	}

	// Tenant ID
	fmt.Printf("Tenant ID [default: %s]: ", config.TenantID)
	if input, _ := reader.ReadString('\n'); strings.TrimSpace(input) != "" {
		config.TenantID = strings.TrimSpace(input)
	}

	// Webhook URL
	fmt.Printf("Webhook URL [default: %s]: ", config.WebhookURL)
	if input, _ := reader.ReadString('\n'); strings.TrimSpace(input) != "" {
		config.WebhookURL = strings.TrimSpace(input)
	}

	// Event type
	fmt.Printf("Event type [default: %s]: ", config.EventType)
	if input, _ := reader.ReadString('\n'); strings.TrimSpace(input) != "" {
		config.EventType = strings.TrimSpace(input)
	}

	// Server host
	fmt.Printf("Server host [default: %s]: ", config.ServerHost)
	if input, _ := reader.ReadString('\n'); strings.TrimSpace(input) != "" {
		config.ServerHost = strings.TrimSpace(input)
	}

	// JWKS host
	fmt.Printf("JWKS host [default: %s]: ", config.JWKSHost)
	if input, _ := reader.ReadString('\n'); strings.TrimSpace(input) != "" {
		config.JWKSHost = strings.TrimSpace(input)
	}

	// Failure rate (only for good traffic mode)
	if config.Mode == "good" {
		fmt.Printf("Failure rate percentage (0-100) [default: %.1f]: ", config.FailureRate)
		if input, _ := reader.ReadString('\n'); strings.TrimSpace(input) != "" {
			if failureRate, err := strconv.ParseFloat(strings.TrimSpace(input), 64); err == nil && failureRate >= 0 && failureRate <= 100 {
				config.FailureRate = failureRate
			}
		}

		// Burst traffic options
		fmt.Printf("Enable burst traffic after normal traffic? (y/N) [default: N]: ")
		if input, _ := reader.ReadString('\n'); strings.ToLower(strings.TrimSpace(input)) == "y" || strings.ToLower(strings.TrimSpace(input)) == "yes" {
			config.Burst = true

			fmt.Printf("Burst volume (requests per second) [default: %d]: ", config.BurstVolume)
			if input, _ := reader.ReadString('\n'); strings.TrimSpace(input) != "" {
				if burstVolume, err := strconv.Atoi(strings.TrimSpace(input)); err == nil && burstVolume > 0 {
					config.BurstVolume = burstVolume
				}
			}

			fmt.Printf("Burst duration in seconds [default: %d]: ", config.BurstDuration)
			if input, _ := reader.ReadString('\n'); strings.TrimSpace(input) != "" {
				if burstDuration, err := strconv.Atoi(strings.TrimSpace(input)); err == nil && burstDuration > 0 {
					config.BurstDuration = burstDuration
				}
			}
		}
	}

	return config, nil
}

// confirmParameters displays the configuration and asks for confirmation
func confirmParameters(config *TrafficConfig) bool {
	fmt.Println()
	printStep("Configuration Summary:")

	// Show mode prominently with emoji
	if config.Mode == "bad" {
		fmt.Printf("  Mode:         🔥 %s traffic (DLQ testing)\n", config.Mode)
	} else {
		fmt.Printf("  Mode:         ✅ %s traffic (mixed success/failure)\n", config.Mode)
	}

	fmt.Printf("  Duration:     %d seconds\n", config.Duration)
	fmt.Printf("  Volume:       %d requests/second\n", config.Volume)
	fmt.Printf("  Tenant ID:    %s\n", config.TenantID)
	fmt.Printf("  Webhook URL:  %s\n", config.WebhookURL)
	fmt.Printf("  Event Type:   %s\n", config.EventType)
	fmt.Printf("  Server Host:  %s\n", config.ServerHost)
	fmt.Printf("  JWKS Host:    %s\n", config.JWKSHost)
	if config.Mode == "good" {
		fmt.Printf("  Failure Rate: %.1f%%\n", config.FailureRate)
		if config.Burst {
			fmt.Printf("  Burst:        Yes (%d req/s for %ds after normal traffic)\n", config.BurstVolume, config.BurstDuration)
		} else {
			fmt.Printf("  Burst:        No\n")
		}
	}
	fmt.Println()

	normalRequests := config.Duration * config.Volume
	burstRequests := 0
	if config.Burst {
		burstRequests = config.BurstDuration * config.BurstVolume
	}
	totalRequests := normalRequests + burstRequests
	fmt.Printf("This will generate approximately %d total requests", normalRequests)
	if config.Burst {
		fmt.Printf(" + %d burst requests = %d total", burstRequests, totalRequests)
	}
	fmt.Printf(".\n")

	if config.Mode == "bad" {
		fmt.Printf("\n⚠️  DLQ Traffic: These requests will intentionally fail to demonstrate\n")
		fmt.Printf("   Dead Letter Queue behavior. Monitor NSQ Admin (http://localhost:4171)\n")
		fmt.Printf("   to see messages move to the DLQ after retry exhaustion.\n")
	} else {
		expectedFailures := int(float64(totalRequests) * (config.FailureRate / 100))
		expectedSuccesses := totalRequests - expectedFailures
		fmt.Printf("\n✅ Mixed Traffic: ~%d requests should succeed (%.1f%%), ~%d should fail (%.1f%%)\n", expectedSuccesses, 100-config.FailureRate, expectedFailures, config.FailureRate)
		if config.Burst {
			fmt.Printf("   Includes burst phase: %d req/s for %ds after normal %d req/s for %ds\n", config.BurstVolume, config.BurstDuration, config.Volume, config.Duration)
		}
		fmt.Printf("   This provides realistic failure patterns for testing alerting and monitoring.\n")
	}
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Continue with traffic generation? (y/N): ")
	response, _ := reader.ReadString('\n')
	response = strings.ToLower(strings.TrimSpace(response))

	return response == "y" || response == "yes"
}

// checkTrafficDependencies checks if required dependencies are available
func checkTrafficDependencies() error {
	printStep("Checking dependencies...")

	// Check for curl
	if _, err := exec.LookPath("curl"); err != nil {
		return fmt.Errorf("curl is required but not installed")
	}

	// Check for jq
	if _, err := exec.LookPath("jq"); err != nil {
		return fmt.Errorf("jq is required but not installed")
	}

	return nil
}

// getJWTToken obtains a JWT token from the JWKS server
func getJWTToken(jwksHost, tenantID string) (string, error) {
	printStep("Getting JWT token...")

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	payload := map[string]string{"tenant_id": tenantID}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal token request: %w", err)
	}

	url := fmt.Sprintf("http://%s/token", jwksHost)
	req, err := http.NewRequest("POST", url, strings.NewReader(string(payloadBytes)))
	if err != nil {
		return "", fmt.Errorf("failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to get token from %s: %w", jwksHost, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token request failed with status %d", resp.StatusCode)
	}

	var tokenResp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to parse token response: %w", err)
	}

	if tokenResp.Token == "" {
		return "", fmt.Errorf("received empty token")
	}

	return tokenResp.Token, nil
}

// setupTrafficEndpoints creates the endpoint and subscription for traffic generation
// Returns: goodEndpointID, goodSubscriptionID, badEndpointID, badSubscriptionID, error
func setupTrafficEndpoints(config *TrafficConfig, token string) (string, string, string, string, error) {
	printStep("Setting up webhook endpoints and subscriptions...")

	// Temporarily set the global JWT token and HTTP mode for API calls
	originalToken := jwtToken
	originalHTTP := useHTTP
	jwtToken = token
	useHTTP = true // Use HTTP for better reliability with Envoy gateway
	defer func() {
		jwtToken = originalToken
		useHTTP = originalHTTP
	}()

	// Create good endpoint via HTTP API (replicating createEndpointCmd logic)
	goodEndpointPayload := map[string]interface{}{
		"url": config.WebhookURL,
	}
	if "demo_secret" != "" {
		goodEndpointPayload["secret"] = "demo_secret"
	}

	resp, err := makeHTTPRequest("POST", fmt.Sprintf("/v1/tenants/%s/endpoints", config.TenantID), goodEndpointPayload)
	if err != nil {
		return "", "", "", "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", "", "", "", fmt.Errorf("HTTP error: %s", resp.Status)
	}

	var endpointResult map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&endpointResult); err != nil {
		return "", "", "", "", fmt.Errorf("failed to decode endpoint response: %w", err)
	}

	// Extract endpoint ID from response
	endpoint, ok := endpointResult["endpoint"].(map[string]interface{})
	if !ok {
		return "", "", "", "", fmt.Errorf("invalid endpoint response format")
	}
	goodEndpointID, ok := endpoint["id"].(string)
	if !ok {
		return "", "", "", "", fmt.Errorf("endpoint ID not found in response")
	}

	// Create good subscription via HTTP API (replicating createSubscriptionCmd logic)
	goodSubscriptionPayload := map[string]interface{}{
		"endpointId": goodEndpointID,
		"eventType":  config.EventType,
	}

	resp2, err := makeHTTPRequest("POST", fmt.Sprintf("/v1/tenants/%s/subscriptions", config.TenantID), goodSubscriptionPayload)
	if err != nil {
		return "", "", "", "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != 200 {
		return "", "", "", "", fmt.Errorf("HTTP error: %s", resp2.Status)
	}

	var subscriptionResult map[string]interface{}
	if err := json.NewDecoder(resp2.Body).Decode(&subscriptionResult); err != nil {
		return "", "", "", "", fmt.Errorf("failed to decode subscription response: %w", err)
	}

	// Extract subscription ID from response
	subscription, ok := subscriptionResult["subscription"].(map[string]interface{})
	if !ok {
		return "", "", "", "", fmt.Errorf("invalid subscription response format")
	}
	goodSubscriptionID, ok := subscription["id"].(string)
	if !ok {
		return "", "", "", "", fmt.Errorf("subscription ID not found in response")
	}

	// Create bad endpoint and subscription for mixed traffic if needed
	badEndpointID := ""
	badSubscriptionID := ""
	if config.Mode == "good" && config.FailureRate > 0 {
		// Create bad endpoint (pointing to /fail)
		badEndpointPayload := map[string]interface{}{
			"url": "http://fake-receiver:8081/fail",
		}
		if "demo_secret" != "" {
			badEndpointPayload["secret"] = "demo_secret"
		}

		resp3, err := makeHTTPRequest("POST", fmt.Sprintf("/v1/tenants/%s/endpoints", config.TenantID), badEndpointPayload)
		if err != nil {
			return "", "", "", "", fmt.Errorf("bad endpoint HTTP request failed: %w", err)
		}
		defer resp3.Body.Close()

		if resp3.StatusCode != 200 {
			return "", "", "", "", fmt.Errorf("bad endpoint HTTP error: %s", resp3.Status)
		}

		var badEndpointResult map[string]interface{}
		if err := json.NewDecoder(resp3.Body).Decode(&badEndpointResult); err != nil {
			return "", "", "", "", fmt.Errorf("failed to decode bad endpoint response: %w", err)
		}

		badEndpoint, ok := badEndpointResult["endpoint"].(map[string]interface{})
		if !ok {
			return "", "", "", "", fmt.Errorf("invalid bad endpoint response format")
		}
		badEndpointID, ok = badEndpoint["id"].(string)
		if !ok {
			return "", "", "", "", fmt.Errorf("bad endpoint ID not found in response")
		}

		// Create bad subscription for failure events
		badSubscriptionPayload := map[string]interface{}{
			"endpointId": badEndpointID,
			"eventType":  config.EventType + ".fail", // Different event type for failures
		}

		resp4, err := makeHTTPRequest("POST", fmt.Sprintf("/v1/tenants/%s/subscriptions", config.TenantID), badSubscriptionPayload)
		if err != nil {
			return "", "", "", "", fmt.Errorf("bad subscription HTTP request failed: %w", err)
		}
		defer resp4.Body.Close()

		if resp4.StatusCode != 200 {
			return "", "", "", "", fmt.Errorf("bad subscription HTTP error: %s", resp4.Status)
		}

		var badSubscriptionResult map[string]interface{}
		if err := json.NewDecoder(resp4.Body).Decode(&badSubscriptionResult); err != nil {
			return "", "", "", "", fmt.Errorf("failed to decode bad subscription response: %w", err)
		}

		badSubscription, ok := badSubscriptionResult["subscription"].(map[string]interface{})
		if !ok {
			return "", "", "", "", fmt.Errorf("invalid bad subscription response format")
		}
		badSubscriptionID, ok = badSubscription["id"].(string)
		if !ok {
			return "", "", "", "", fmt.Errorf("bad subscription ID not found in response")
		}
	}

	return goodEndpointID, goodSubscriptionID, badEndpointID, badSubscriptionID, nil
}

// generateTrafficWithProgress generates traffic and shows progress updates
func generateTrafficWithProgress(config *TrafficConfig, token string) (*TrafficSummary, error) {
	trafficDesc := fmt.Sprintf("%d RPS for %d seconds", config.Volume, config.Duration)
	if config.Burst {
		trafficDesc += fmt.Sprintf(" + BURST %d RPS for %d seconds", config.BurstVolume, config.BurstDuration)
	}
	printStep(fmt.Sprintf("Generating traffic (%s)...", trafficDesc))

	// Phase 1: Normal traffic
	fmt.Printf("Phase 1: Normal Traffic (%d RPS for %d seconds)\n", config.Volume, config.Duration)
	normalSummary, err := generateTrafficPhase(config, token, config.Volume, config.Duration, "normal")
	if err != nil {
		return nil, fmt.Errorf("normal traffic phase failed: %w", err)
	}

	// Phase 2: Burst traffic (if enabled)
	var burstSummary *TrafficSummary
	if config.Burst {
		fmt.Printf("\nPhase 2: Burst Traffic (%d RPS for %d seconds)\n", config.BurstVolume, config.BurstDuration)
		burstSummary, err = generateTrafficPhase(config, token, config.BurstVolume, config.BurstDuration, "burst")
		if err != nil {
			return nil, fmt.Errorf("burst traffic phase failed: %w", err)
		}
	}

	// Combine summaries
	combinedSummary := combineTrafficSummaries(normalSummary, burstSummary, config.Burst)
	return combinedSummary, nil
}

// generateTrafficPhase generates traffic for a single phase (normal or burst)
func generateTrafficPhase(config *TrafficConfig, token string, volume, duration int, phase string) (*TrafficSummary, error) {
	// Temporarily set the global JWT token and HTTP mode for API calls
	originalToken := jwtToken
	originalHTTP := useHTTP
	jwtToken = token
	useHTTP = true // Use HTTP for better reliability with Envoy gateway
	defer func() {
		jwtToken = originalToken
		useHTTP = originalHTTP
	}()

	startTime := time.Now()
	endTime := startTime.Add(time.Duration(duration) * time.Second)

	requestCount := 0
	successCount := 0
	goodEndpointRequests := 0
	badEndpointRequests := 0

	// Initialize random number generator for failure rate
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	// Rate limiting: sleep time between requests
	sleepDuration := time.Second / time.Duration(volume)

	fmt.Printf("Progress: ")

	for time.Now().Before(endTime) {
		// Determine if this request should fail (only for good mode with failure rate)
		shouldFail := false
		if config.Mode == "good" && config.FailureRate > 0 {
			// Generate random number 0-100 and compare with failure rate
			shouldFail = rng.Float64()*100 < config.FailureRate
		}

		// Create event payload
		payload := map[string]interface{}{
			"demo":        true,
			"type":        "traffic_test",
			"timestamp":   time.Now().UTC().Format(time.RFC3339),
			"request_id":  fmt.Sprintf("req-%d", requestCount),
			"mode":        config.Mode,
			"should_fail": shouldFail,
		}

		// Choose event type based on whether this should fail
		eventType := config.EventType
		if shouldFail {
			eventType = config.EventType + ".fail"
			badEndpointRequests++
		} else {
			goodEndpointRequests++
		}

		// Publish event via HTTP API (replicating publishCmd logic)
		httpPayload := map[string]interface{}{
			"eventType": eventType,
			"payload":   payload,
		}

		resp, err := makeHTTPRequest("POST", fmt.Sprintf("/v1/tenants/%s/events:publish", config.TenantID), httpPayload)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				successCount++
			}
		}

		requestCount++

		// Progress indicator
		if requestCount%10 == 0 {
			fmt.Print(".")
		}
		if requestCount%(volume*10) == 0 {
			elapsed := time.Since(startTime)
			remaining := time.Duration(duration)*time.Second - elapsed
			fmt.Printf(" [%d reqs, %ds remaining]\n          ", requestCount, int(remaining.Seconds()))
		}

		// Rate limiting
		time.Sleep(sleepDuration)
	}

	actualDuration := time.Since(startTime)
	fmt.Println() // New line after progress

	summary := &TrafficSummary{
		TotalRequests:    requestCount,
		SuccessRequests:  successCount,
		FailedRequests:   requestCount - successCount,
		GoodEndpointReqs: goodEndpointRequests,
		BadEndpointReqs:  badEndpointRequests,
		TotalDuration:    actualDuration,
		OverallRPS:       float64(requestCount) / actualDuration.Seconds(),
	}

	// Set phase-specific fields
	if phase == "normal" {
		summary.NormalRequests = requestCount
		summary.NormalDuration = actualDuration
		summary.NormalRPS = float64(requestCount) / actualDuration.Seconds()
	} else if phase == "burst" {
		summary.BurstRequests = requestCount
		summary.BurstDuration = actualDuration
		summary.BurstRPS = float64(requestCount) / actualDuration.Seconds()
	}

	return summary, nil
}

// combineTrafficSummaries combines normal and burst traffic summaries
func combineTrafficSummaries(normal, burst *TrafficSummary, hadBurst bool) *TrafficSummary {
	combined := &TrafficSummary{
		TotalRequests:    normal.TotalRequests,
		SuccessRequests:  normal.SuccessRequests,
		FailedRequests:   normal.FailedRequests,
		GoodEndpointReqs: normal.GoodEndpointReqs,
		BadEndpointReqs:  normal.BadEndpointReqs,
		NormalRequests:   normal.NormalRequests,
		NormalDuration:   normal.NormalDuration,
		NormalRPS:        normal.NormalRPS,
		TotalDuration:    normal.TotalDuration,
		HadBurst:         hadBurst,
	}

	if hadBurst && burst != nil {
		// Add burst statistics
		combined.TotalRequests += burst.TotalRequests
		combined.SuccessRequests += burst.SuccessRequests
		combined.FailedRequests += burst.FailedRequests
		combined.GoodEndpointReqs += burst.GoodEndpointReqs
		combined.BadEndpointReqs += burst.BadEndpointReqs
		combined.BurstRequests = burst.BurstRequests
		combined.BurstDuration = burst.BurstDuration
		combined.BurstRPS = burst.BurstRPS
		combined.TotalDuration = normal.TotalDuration + burst.TotalDuration
	}

	// Calculate overall RPS
	if combined.TotalDuration.Seconds() > 0 {
		combined.OverallRPS = float64(combined.TotalRequests) / combined.TotalDuration.Seconds()
	}

	return combined
}

// printTrafficSummary prints the final traffic generation summary
func printTrafficSummary(summary *TrafficSummary) {
	// Mode-specific header
	if summary.Mode == "bad" {
		printHeader("🔥 DLQ Traffic Generation Complete!")
	} else if summary.HadBurst {
		printHeader("🚀 Mixed Traffic + Burst Generation Complete!")
	} else {
		printHeader("✅ Traffic Generation Complete!")
	}

	fmt.Printf("Total Requests:    %d\n", summary.TotalRequests)

	if summary.Mode == "bad" {
		// For DLQ traffic, explain the success/failure context
		fmt.Printf("Events Published:  %d (%.2f%%) - Successfully sent to Harborhook\n", summary.SuccessRequests, float64(summary.SuccessRequests)/float64(summary.TotalRequests)*100)
		fmt.Printf("Publish Failures:  %d (%.2f%%) - Failed to send to Harborhook\n", summary.FailedRequests, float64(summary.FailedRequests)/float64(summary.TotalRequests)*100)
		fmt.Println()
		fmt.Printf("⚠️  Note: Published events will fail webhook delivery to %s\n", "/fail endpoint")
		fmt.Printf("   and eventually move to DLQ after retry exhaustion.\n")
	} else {
		// For good traffic with mixed success/failure
		fmt.Printf("Events Published:  %d (%.2f%%) - Successfully sent to Harborhook\n", summary.SuccessRequests, float64(summary.SuccessRequests)/float64(summary.TotalRequests)*100)
		fmt.Printf("Publish Failures:  %d (%.2f%%) - Failed to send to Harborhook\n", summary.FailedRequests, float64(summary.FailedRequests)/float64(summary.TotalRequests)*100)
		fmt.Println()
		// Show endpoint breakdown for mixed traffic
		if summary.GoodEndpointReqs > 0 && summary.BadEndpointReqs > 0 {
			fmt.Printf("Good Endpoint:     %d (%.1f%%) - Will succeed webhook delivery\n", summary.GoodEndpointReqs, float64(summary.GoodEndpointReqs)/float64(summary.TotalRequests)*100)
			fmt.Printf("Bad Endpoint:      %d (%.1f%%) - Will fail webhook delivery\n", summary.BadEndpointReqs, float64(summary.BadEndpointReqs)/float64(summary.TotalRequests)*100)
		} else {
			fmt.Printf("All requests sent to good endpoint - Should succeed webhook delivery\n")
		}
	}

	if summary.HadBurst {
		fmt.Printf("Normal Phase:      %d requests in %.1fs (%.2f RPS)\n", summary.NormalRequests, summary.NormalDuration.Seconds(), summary.NormalRPS)
		fmt.Printf("Burst Phase:       %d requests in %.1fs (%.2f RPS)\n", summary.BurstRequests, summary.BurstDuration.Seconds(), summary.BurstRPS)
		fmt.Printf("Total Duration:    %.2f seconds\n", summary.TotalDuration.Seconds())
		fmt.Printf("Overall RPS:       %.2f requests/second\n", summary.OverallRPS)
	} else {
		fmt.Printf("Duration:          %.2f seconds\n", summary.TotalDuration.Seconds())
		fmt.Printf("Actual RPS:        %.2f requests/second\n", summary.OverallRPS)
	}
	fmt.Printf("Good Endpoint ID:  %s\n", summary.EndpointID)
	fmt.Printf("Good Subscription: %s\n", summary.SubscriptionID)
	if summary.BadEndpointID != "" {
		fmt.Printf("Bad Endpoint ID:   %s\n", summary.BadEndpointID)
		fmt.Printf("Bad Subscription:  %s\n", summary.BadSubscriptionID)
	}

	fmt.Println()

	// Mode-specific next steps based on actual mode, not success/failure ratio
	if summary.Mode == "bad" {
		printInfo("🔥 DLQ Traffic Next Steps:")
		fmt.Println("1. Check NSQ Admin (http://localhost:4171) to see DLQ message counts")
		fmt.Println("2. Monitor worker backoff behavior in Grafana dashboards")
		fmt.Println("3. Look for retry attempts in Grafana → Explore → Loki")
		fmt.Println("4. Watch SLO burn rate alerts trigger due to high failure rate")
		fmt.Println("5. Webhook deliveries will fail and move to DLQ within 1-2 minutes")
	} else {
		if summary.HadBurst {
			printInfo("🚀 Burst Traffic Next Steps:")
			fmt.Printf("1. Monitor traffic spike in Grafana - burst generated %.2f RPS vs %.2f RPS normal\n", summary.BurstRPS, summary.NormalRPS)
			fmt.Println("2. Check for backlog growth during burst phase")
			fmt.Println("3. Verify autoscaling or rate limiting behavior if configured")
			fmt.Println("4. Monitor latency increases during high-volume periods")
			if summary.BadEndpointReqs > 0 {
				fmt.Printf("5. Watch failure rate during burst - expected ~%.1f%% overall\n", float64(summary.BadEndpointReqs)/float64(summary.TotalRequests)*100)
			}
		} else if summary.BadEndpointReqs > 0 {
			printInfo("📊 Mixed Traffic Next Steps:")
			fmt.Printf("1. Monitor success rate in Grafana - should be ~%.1f%%\n", float64(summary.GoodEndpointReqs)/float64(summary.TotalRequests)*100)
			fmt.Println("2. Watch for delivery failures from bad endpoint requests")
			fmt.Println("3. Check retry attempts and eventual DLQ movement for failed deliveries")
			fmt.Println("4. Monitor SLO burn rate alerts if failure rate is significant")
			fmt.Println("5. Verify mixed traffic shows in Grafana dashboards")
		} else {
			printInfo("✅ Good Traffic Next Steps:")
			fmt.Println("1. Visit Grafana (http://localhost:3000) to view metrics and dashboards")
			fmt.Println("2. Check successful delivery traces in Grafana → Explore → Tempo")
			fmt.Println("3. Monitor success rate metrics staying high")
			fmt.Println("4. Check fake-receiver logs for successful webhook deliveries")
		}
	}

	fmt.Println()
}

// Helper functions for colored output (matching demo.sh style)
func printHeader(msg string) {
	fmt.Printf("\n\033[0;35m%s\033[0m\n", msg)
	fmt.Println("==============================================")
}

func printStep(msg string) {
	fmt.Printf("\033[0;34m==> %s\033[0m\n", msg)
}

func printSuccess(msg string) {
	fmt.Printf("\033[0;32m✓ %s\033[0m\n", msg)
}

func printInfo(msg string) {
	fmt.Printf("\033[0;36mℹ %s\033[0m\n", msg)
}
