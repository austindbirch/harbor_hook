# harborctl - Harborhook CLI

`harborctl` is a CLI tool for interacting with Harborhook. It provides a complete set of tools for managing endpoints, subscriptions, events, and deliveries.

## Installation

### From Source

```bash
# Clone the repository
git clone https://github.com/austindbirch/harbor_hook.git
cd harbor_hook

# Build the CLI
make build-cli

# Install to system (optional)
make install-cli
```

### Using Docker

```bash
# Build the Docker image
docker build -t harborctl ./cmd/harborctl

# Run the CLI in a container
docker run --rm harborctl --help
```

## Configuration

### Initialize Configuration

Create a default configuration file:

```bash
harborctl config init
```

This creates `~/.harborctl.yaml` with default settings.

### Set Configuration Values

```bash
# Set the server address
harborctl config set server localhost:8080

# Set request timeout
harborctl config set timeout 60s

# Use HTTP instead of gRPC
harborctl config set http true

# Enable JSON output by default
harborctl config set json true

# Enable pretty JSON formatting (requires jq)
harborctl config set pretty true
```

# Use HTTP instead of gRPC
harborctl config set http true

# Enable JSON output by default
harborctl config set json true
```

### View Current Configuration

```bash
harborctl config view
```

## Quick Start

### 1. Check Service Health

```bash
# Ping the service
harborctl ping

# Health check
harborctl health
```

### 2. Create an Endpoint

```bash
# Create a webhook endpoint
harborctl endpoint create tn_123 https://example.com/webhook

# Create with custom secret
harborctl endpoint create tn_123 https://example.com/webhook --secret my-secret-key
```

### 3. Create a Subscription

```bash
# Subscribe the endpoint to an event type
harborctl subscription create tn_123 ep_456 appointment.created
```

### 4. Publish an Event

```bash
# Publish an event with JSON payload
harborctl event publish tn_123 appointment.created '{"id":"apt_789","patient":"John Doe"}'

# Publish with idempotency key
harborctl event publish tn_123 appointment.created '{"id":"apt_789","patient":"John Doe"}' --idempotency-key unique-key-123
```

### 5. Check Delivery Status

```bash
# Get delivery status for an event
harborctl delivery status evt_123

# Filter by endpoint
harborctl delivery status evt_123 --endpoint-id ep_456

# Filter by time range
harborctl delivery status evt_123 --from "2023-01-01T00:00:00Z" --to "2023-01-02T00:00:00Z"

# Limit results
harborctl delivery status evt_123 --limit 20
```

### 6. Manage Failed Deliveries

```bash
# List dead letter queue entries
harborctl delivery dlq

# Filter DLQ by endpoint
harborctl delivery dlq --endpoint-id ep_456

# Replay a failed delivery
harborctl delivery replay del_789 --reason "endpoint was down"
```

## Command Reference

### Global Flags

- `--server`: Server address (default: localhost:8080)
- `--timeout`: Request timeout (default: 30s)
- `--http`: Use HTTP instead of gRPC
- `--json`: Output in JSON format
- `--pretty`: Use jq for pretty JSON formatting (requires jq)
- `--config`: Configuration file path

### Commands

#### Service Commands

- `harborctl ping` - Ping the service
- `harborctl health` - Check service health
- `harborctl version` - Show version information

#### Endpoint Management

- `harborctl endpoint create [tenant-id] [url]` - Create webhook endpoint
  - `--secret`: Custom webhook secret

#### Subscription Management

- `harborctl subscription create [tenant-id] [endpoint-id] [event-type]` - Create subscription

#### Event Management

- `harborctl event publish [tenant-id] [event-type] [payload-json]` - Publish event
  - `--idempotency-key`: Deduplication key

#### Delivery Management

- `harborctl delivery status [event-id]` - Get delivery status
  - `--endpoint-id`: Filter by endpoint
  - `--from`: Start time (RFC3339)
  - `--to`: End time (RFC3339)
  - `--limit`: Maximum results

- `harborctl delivery replay [delivery-id]` - Replay delivery
  - `--reason`: Reason for replay

- `harborctl delivery dlq` - List dead letter queue
  - `--endpoint-id`: Filter by endpoint
  - `--limit`: Maximum results

#### Configuration Management

- `harborctl config init` - Initialize config file
  - `--force`: Overwrite existing config
- `harborctl config view` - View current config
- `harborctl config set [key] [value]` - Set config value
- `harborctl config check` - Check configuration and dependencies

#### Utility Commands

- `harborctl completion [bash|zsh|fish|powershell]` - Generate shell completion

## Examples

### Complete Workflow

```bash
# 1. Initialize configuration
harborctl config init
harborctl config set server localhost:8080

# 2. Create endpoint
ENDPOINT_ID=$(harborctl endpoint create tn_123 https://webhook.example.com/hook --json | jq -r '.endpoint.id')

# 3. Create subscription
harborctl subscription create tn_123 $ENDPOINT_ID appointment.created

# 4. Publish event
EVENT_ID=$(harborctl event publish tn_123 appointment.created '{"id":"apt_123","patient":"John"}' --json | jq -r '.eventId')

# 5. Check delivery status
harborctl delivery status $EVENT_ID

# 6. If delivery failed, replay it
DELIVERY_ID=$(harborctl delivery status $EVENT_ID --json | jq -r '.attempts[0].deliveryId')
harborctl delivery replay $DELIVERY_ID --reason "manual retry"
```

### Monitoring Script

```bash
#!/bin/bash
# Monitor dead letter queue

echo "Checking DLQ entries..."
harborctl delivery dlq --limit 50 --json | jq '.dead[] | {
  deliveryId: .deliveryId,
  eventId: .eventId,
  endpointId: .endpointId,
  errorReason: .errorReason,
  dlqAt: .dlqAt
}'
```

### Bulk Operations

```bash
# Replay all failed deliveries for an endpoint
harborctl delivery dlq --endpoint-id ep_456 --json | \
  jq -r '.dead[].deliveryId' | \
  xargs -I {} harborctl delivery replay {} --reason "bulk retry"
```

## Shell Completion

Enable shell autocompletion for better user experience:

```bash
# Bash
source <(harborctl completion bash)

# Zsh
harborctl completion zsh > "${fpath[1]}/_harborctl"

# Fish
harborctl completion fish | source

# PowerShell
harborctl completion powershell | Out-String | Invoke-Expression
```

## JSON Output and Pretty Formatting

All commands support JSON output with the `--json` flag, making them suitable for scripting and automation:

```bash
# Get endpoint details in JSON
harborctl endpoint create tn_123 https://example.com/hook --json

# Parse with jq manually
harborctl delivery status evt_123 --json | jq '.attempts[] | select(.status == "DELIVERY_ATTEMPT_STATUS_FAILED")'
```

### Pretty JSON Formatting

Enable automatic jq formatting for all JSON output:

```bash
# Enable pretty formatting (requires jq)
harborctl config set pretty true

# Now all JSON output is automatically formatted
harborctl config view  # Beautiful, readable JSON

# Check if jq is available
harborctl config check

# Temporarily disable pretty formatting
harborctl config view --pretty=false
```

Benefits of pretty formatting:
- **Readable**: Proper indentation and key sorting
- **Colorized**: Syntax highlighting (if your terminal supports it)
- **Consistent**: Same formatting across all commands
- **Automatic**: No need to pipe to jq manually

## Error Handling

The CLI provides meaningful error messages and proper exit codes:

- Exit code 0: Success
- Exit code 1: General error
- Exit code 2: Command usage error

```bash
# Check if command succeeded
if harborctl ping; then
  echo "Service is running"
else
  echo "Service is down"
fi
```

## Debugging and Troubleshooting

Use the `--timeout` flag to adjust timeouts for slow networks:

```bash
harborctl ping --timeout 60s
```

Enable HTTP mode for debugging or when gRPC is not available:

```bash
harborctl ping --http
```

### Pretty Formatting Troubleshooting

If pretty formatting is enabled but not working:

```bash
# Check if jq is available
harborctl config check

# Install jq if missing (macOS)
brew install jq

# Install jq if missing (Ubuntu/Debian)
sudo apt-get install jq

# Disable pretty formatting if you don't want to install jq
harborctl config set pretty false
```

The CLI will automatically fall back to standard JSON formatting if jq is not available, with a warning message.
