package cmd

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/austindbirch/harbor_hook/cmd/harborctl/cmd/ascii"
	webhookv1 "github.com/austindbirch/harbor_hook/protogen/go/api/webhook/v1"
)

var (
	cfgFile    string
	serverAddr string
	timeout    time.Duration
	useHTTP    bool
	outputJSON bool
	prettyJSON bool
	jwtToken   string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "harborctl",
	Short: "Harbor Hook CLI - Interact with the Harbor Hook webhook service",
	Long: `Harbor Hook CLI (harborctl) is a command line tool for interacting with 
the Harbor Hook webhook delivery service.

You can use it to publish events, check delivery status, replay deliveries, 
and manage endpoints and subscriptions.`,
	Annotations: map[string]string{
		ascii.AnnotationKey: ascii.Root,
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.harborctl.yaml)")
	rootCmd.PersistentFlags().StringVar(&serverAddr, "server", "localhost:8443", "server address (host:port) - defaults to HTTPS gateway")
	rootCmd.PersistentFlags().DurationVar(&timeout, "timeout", 30*time.Second, "request timeout")
	rootCmd.PersistentFlags().BoolVar(&useHTTP, "http", false, "use HTTP instead of gRPC")
	rootCmd.PersistentFlags().BoolVar(&outputJSON, "json", false, "output in JSON format")
	rootCmd.PersistentFlags().BoolVar(&prettyJSON, "pretty", false, "use jq for pretty JSON formatting (requires jq)")
	rootCmd.PersistentFlags().StringVar(&jwtToken, "token", "", "JWT token for authentication (overrides JWT_TOKEN env var)")

	// Bind flags to viper
	viper.BindPFlag("server", rootCmd.PersistentFlags().Lookup("server"))
	viper.BindPFlag("timeout", rootCmd.PersistentFlags().Lookup("timeout"))
	viper.BindPFlag("http", rootCmd.PersistentFlags().Lookup("http"))
	viper.BindPFlag("json", rootCmd.PersistentFlags().Lookup("json"))
	viper.BindPFlag("pretty", rootCmd.PersistentFlags().Lookup("pretty"))
	viper.BindPFlag("token", rootCmd.PersistentFlags().Lookup("token"))
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".harborctl")
	}

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}

	// Override global variables with config values if flags weren't explicitly set
	if !rootCmd.PersistentFlags().Changed("server") {
		if s := viper.GetString("server"); s != "" {
			serverAddr = s
		}
	}
	if !rootCmd.PersistentFlags().Changed("timeout") {
		if d := viper.GetDuration("timeout"); d > 0 {
			timeout = d
		}
	}
	if !rootCmd.PersistentFlags().Changed("http") {
		useHTTP = viper.GetBool("http")
	}
	if !rootCmd.PersistentFlags().Changed("json") {
		outputJSON = viper.GetBool("json")
	}
	if !rootCmd.PersistentFlags().Changed("pretty") {
		prettyJSON = viper.GetBool("pretty")
	}
	if !rootCmd.PersistentFlags().Changed("token") {
		if t := viper.GetString("token"); t != "" {
			jwtToken = t
		} else if t := os.Getenv("JWT_TOKEN"); t != "" {
			jwtToken = t
		}
	}
}

// getClient returns a gRPC client for the webhook service
func getClient() (webhookv1.WebhookServiceClient, func(), error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	conn, err := grpc.DialContext(ctx, serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("failed to connect: %w", err)
	}

	client := webhookv1.NewWebhookServiceClient(conn)
	cleanup := func() {
		cancel()
		conn.Close()
	}

	return client, cleanup, nil
}

// makeHTTPRequest makes an HTTP request to the REST API
func makeHTTPRequest(method, path string, body interface{}) (*http.Response, error) {
	// Create HTTP client with TLS support for HTTPS
	tr := &http.Transport{
		TLSClientConfig:    &tls.Config{InsecureSkipVerify: true}, // For development with self-signed certs
		DisableCompression: true,                                  // Disable compression to avoid parsing issues
	}
	client := &http.Client{
		Timeout:   timeout,
		Transport: tr,
	}

	var bodyReader strings.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal body: %w", err)
		}
		bodyReader = *strings.NewReader(string(bodyBytes))
	}

	// Always use HTTPS for the gateway (useHTTP only controls gRPC vs HTTP/REST)
	scheme := "https"
	url := fmt.Sprintf("%s://%s%s", scheme, serverAddr, path)

	req, err := http.NewRequest(method, url, &bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// Add JWT token if available
	if jwtToken != "" {
		req.Header.Set("Authorization", "Bearer "+jwtToken)
	}

	return client.Do(req)
}

// checkJQAvailable checks if jq is available in PATH
func checkJQAvailable() bool {
	_, err := exec.LookPath("jq")
	return err == nil
}

// formatWithJQ formats JSON using jq for pretty printing
func formatWithJQ(jsonData []byte) (string, error) {
	if !checkJQAvailable() {
		return "", fmt.Errorf("jq not found in PATH")
	}

	cmd := exec.Command("jq", ".")
	cmd.Stdin = bytes.NewReader(jsonData)

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("jq formatting failed: %s", stderr.String())
	}

	return out.String(), nil
}

// printOutput prints the response in the requested format
func printOutput(v interface{}) {
	if outputJSON {
		var jsonData []byte
		var err error

		if msg, ok := v.(proto.Message); ok {
			// Use protojson for protobuf messages
			opts := protojson.MarshalOptions{
				Multiline:       !prettyJSON, // Don't use multiline if we're using jq
				Indent:          "  ",
				EmitUnpopulated: false,
			}
			jsonData, err = opts.Marshal(msg)
		} else {
			// Use standard JSON for other types
			if prettyJSON {
				// Compact JSON if we're going to format with jq
				jsonData, err = json.Marshal(v)
			} else {
				jsonData, err = json.MarshalIndent(v, "", "  ")
			}
		}

		if err != nil {
			fmt.Fprintf(os.Stderr, "Error marshaling to JSON: %v\n", err)
			return
		}

		if prettyJSON {
			formatted, jqErr := formatWithJQ(jsonData)
			if jqErr != nil {
				// Fall back to standard pretty printing if jq fails
				fmt.Fprintf(os.Stderr, "Warning: %v, falling back to standard formatting\n", jqErr)
				if msg, ok := v.(proto.Message); ok {
					opts := protojson.MarshalOptions{
						Multiline:       true,
						Indent:          "  ",
						EmitUnpopulated: false,
					}
					jsonData, _ = opts.Marshal(msg)
				} else {
					jsonData, _ = json.MarshalIndent(v, "", "  ")
				}
				fmt.Println(string(jsonData))
			} else {
				// Print jq-formatted output (already includes newline)
				fmt.Print(formatted)
			}
		} else {
			fmt.Println(string(jsonData))
		}
	} else {
		// Human-readable format
		fmt.Printf("%+v\n", v)
	}
}

// parseJSON parses a JSON string into a protobuf Struct
func parseJSON(jsonStr string) (*structpb.Struct, error) {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return structpb.NewStruct(data)
}

// parseTimestamp parses a timestamp string (RFC3339 format)
func parseTimestamp(timeStr string) (*timestamppb.Timestamp, error) {
	if timeStr == "" {
		return nil, nil
	}

	t, err := time.Parse(time.RFC3339, timeStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse timestamp (expected RFC3339 format): %w", err)
	}

	return timestamppb.New(t), nil
}

// parseInt32 parses a string to int32
func parseInt32(s string) (int32, error) {
	if s == "" {
		return 0, nil
	}

	i, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return 0, err
	}

	return int32(i), nil
}
