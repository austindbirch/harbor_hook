#!/bin/bash
# Observability Demo Script - Phase 5
# Demonstrates metrics, traces, logs, and alerts working together

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
PURPLE='\033[0;35m'
NC='\033[0m' # No Color

# Print colored output
print_header() {
    echo -e "\n${PURPLE}$1${NC}"
    echo "=============================================="
}

print_step() {
    echo -e "${BLUE}==> $1${NC}"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

print_info() {
    echo -e "${CYAN}ℹ $1${NC}"
}

# Configuration
TENANT_ID=${TENANT_ID:-"observability_demo"}
WEBHOOK_URL_SUCCESS="http://fake-receiver:8081/hook"
WEBHOOK_URL_FAIL="http://fake-receiver:8081/fail"  # fake-receiver has /fail endpoint
EVENT_TYPE="obs.demo.event"
SERVER_HOST=${SERVER_HOST:-"localhost:8443"}  # HTTPS gateway
JWKS_HOST=${JWKS_HOST:-"localhost:8082"}      # JWT token server
FAKE_SECRET="demo_secret"               # Shared secret for HMAC signing

# Demo configuration
TRAFFIC_DURATION=${TRAFFIC_DURATION:-120}     # 2 minutes of traffic by default
HIGH_TRAFFIC_RPS=${HIGH_TRAFFIC_RPS:-10}      # 10 requests per second
BURST_TRAFFIC_RPS=${BURST_TRAFFIC_RPS:-50}    # 50 requests per second for bursts
FAILURE_PERCENTAGE=${FAILURE_PERCENTAGE:-20}  # 20% of requests should fail

# Temp file to pass counts back from background subshell
COUNTS_FILE="$(mktemp -t harbor_counts.XXXXXX)"

# Safe percentage helper (avoids divide-by-zero and empty vars)
pct() {
  local num="${1:-0}"
  local den="${2:-0}"
  if [ "$den" -eq 0 ] 2>/dev/null; then
    echo "0.00"
  else
    echo "scale=2; ($num * 100) / $den" | bc
  fi
}

print_header "🔭 Harbor Hook Observability Demo (Phase 5)"
echo "This demo will:"
echo "• Generate high-volume traffic (success & failure scenarios)"
echo "• Create dead endpoints for fast DLQ testing (1-2 min vs 15 min)"
echo "• Demonstrate distributed tracing across services"
echo "• Trigger and resolve alerts based on SLO violations"
echo "• Show correlation between metrics, traces, and logs in Grafana"
echo ""
echo "Demo Configuration:"
echo "  • Traffic Duration: ${TRAFFIC_DURATION}s"
echo "  • High Traffic RPS: ${HIGH_TRAFFIC_RPS}"
echo "  • Burst Traffic RPS: ${BURST_TRAFFIC_RPS}"
echo "  • Expected Failure Rate: ${FAILURE_PERCENTAGE}%"

# Function to get JWT token
get_jwt_token() {
    local token_response
    token_response=$(curl -s -X POST "http://$JWKS_HOST/token" \
        -H "Content-Type: application/json" \
        -d "{\"tenant_id\":\"$TENANT_ID\"}" || {
        print_error "Failed to get JWT token from $JWKS_HOST"
        exit 1
    })
    
    # Extract token from JSON response
    echo "$token_response" | jq -r '.token' 2>/dev/null || {
        print_error "Failed to parse JWT token from response"
        echo "Response: $token_response"
        exit 1
    }
}

# Check dependencies
check_dependencies() {
    print_step "Checking dependencies..."
    
    if [ ! -f "./bin/harborctl" ]; then
        print_error "harborctl not found at ./bin/harborctl. Please build it first:"
        echo "   make build-cli"
        exit 1
    fi
    
    if ! command -v jq &> /dev/null; then
        print_error "jq is required but not installed"
        exit 1
    fi
    
    if ! command -v curl &> /dev/null; then
        print_error "curl is required but not installed"
        exit 1
    fi

    if ! command -v bc &> /dev/null; then
        print_error "bc is required but not installed"
        exit 1
    fi
    
    print_success "All dependencies available"
}

# Setup endpoints and subscriptions
setup_webhooks() {
    print_step "Setting up webhook endpoints and subscriptions..."
    
    # Get JWT token
    JWT_TOKEN=$(get_jwt_token)
    if [[ -z "$JWT_TOKEN" || "$JWT_TOKEN" == "null" ]]; then
        print_error "Failed to obtain valid JWT token"
        exit 1
    fi
    export JWT_TOKEN
    
    local harborctl="./bin/harborctl"
    
    # Check service health
    print_step "Verifying service health..."
    $harborctl ping --server "$SERVER_HOST"
    print_success "Services are healthy"
    
    # Create success endpoint
    print_step "Creating success webhook endpoint..."
    local success_resp=$($harborctl endpoint create "$TENANT_ID" "$WEBHOOK_URL_SUCCESS" --secret "$FAKE_SECRET" --server "$SERVER_HOST" --json)
    SUCCESS_ENDPOINT_ID=$(echo "$success_resp" | jq -r '.endpoint.id')
    print_success "Success endpoint created: $SUCCESS_ENDPOINT_ID"
    
    # Create failure endpoint  
    print_step "Creating failure webhook endpoint..."
    local fail_resp=$($harborctl endpoint create "$TENANT_ID" "$WEBHOOK_URL_FAIL" --secret "$FAKE_SECRET" --server "$SERVER_HOST" --json)
    FAIL_ENDPOINT_ID=$(echo "$fail_resp" | jq -r '.endpoint.id')
    print_success "Failure endpoint created: $FAIL_ENDPOINT_ID"
    
    # Create subscriptions
    print_step "Creating subscriptions..."
    local success_sub_resp=$($harborctl subscription create "$TENANT_ID" "$SUCCESS_ENDPOINT_ID" "${EVENT_TYPE}.success" --server "$SERVER_HOST" --json)
    SUCCESS_SUBSCRIPTION_ID=$(echo "$success_sub_resp" | jq -r '.subscription.id')
    
    local fail_sub_resp=$($harborctl subscription create "$TENANT_ID" "$FAIL_ENDPOINT_ID" "${EVENT_TYPE}.failure" --server "$SERVER_HOST" --json)
    FAIL_SUBSCRIPTION_ID=$(echo "$fail_sub_resp" | jq -r '.subscription.id')
    
    print_success "Subscriptions created: success=$SUCCESS_SUBSCRIPTION_ID, failure=$FAIL_SUBSCRIPTION_ID"
}

# Generate background traffic
generate_background_traffic() {
    print_step "Starting background traffic generation (RPS: $HIGH_TRAFFIC_RPS)..."
    
    local harborctl="./bin/harborctl"
    local start_time
    start_time=$(date +%s)
    local end_time=$((start_time + TRAFFIC_DURATION))
    local request_count=0
    local success_count=0
    local failure_count=0
    
    while [ "$(date +%s)" -lt "$end_time" ]; do
        # Determine if this request should succeed or fail
        local random=$((RANDOM % 100))
        local should_fail=$((random < FAILURE_PERCENTAGE))
        
        if [ "$should_fail" -eq 1 ]; then
            # Generate failure event
            local payload="{\"demo\": true, \"type\": \"failure\", \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\", \"request_id\": \"req-$request_count\", \"user_id\": \"user-$((RANDOM % 100))\"}"
            $harborctl event publish "$TENANT_ID" "${EVENT_TYPE}.failure" "$payload" --server "$SERVER_HOST" --json > /dev/null 2>&1 || true
            failure_count=$((failure_count + 1))
        else
            # Generate success event
            local payload="{\"demo\": true, \"type\": \"success\", \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\", \"request_id\": \"req-$request_count\", \"user_id\": \"user-$((RANDOM % 100))\"}"
            $harborctl event publish "$TENANT_ID" "${EVENT_TYPE}.success" "$payload" --server "$SERVER_HOST" --json > /dev/null 2>&1 || true
            success_count=$((success_count + 1))
        fi
        
        request_count=$((request_count + 1))
        
        # Rate limiting - sleep to maintain RPS
        sleep "$(echo "scale=3; 1.0 / $HIGH_TRAFFIC_RPS" | bc)"
        
        # Progress indicator every 50 requests
        if [ $((request_count % 50)) -eq 0 ]; then
            local elapsed=$(( $(date +%s) - start_time ))
            local remaining=$((TRAFFIC_DURATION - elapsed))
            echo -n "."
            if [ $((request_count % 250)) -eq 0 ]; then
                echo " [${request_count} reqs, ${remaining}s remaining]"
            fi
        fi
    done
    
    echo ""
    print_success "Background traffic completed: $request_count requests ($success_count success, $failure_count failures)"
    
    # Write counts to temp file for the parent shell to source
    {
      printf "TOTAL_REQUESTS=%d\n" "$request_count"
      printf "SUCCESS_REQUESTS=%d\n" "$success_count"
      printf "FAILURE_REQUESTS=%d\n" "$failure_count"
    } > "$COUNTS_FILE"
}

# Generate DLQ traffic - force messages to dead letter queue quickly
generate_dlq_traffic() {
    print_header "💀 Generating DLQ Traffic (Fast Failures)"
    print_step "Creating endpoints that will fail immediately to force DLQ scenarios..."

    local harborctl="./bin/harborctl"

    # Create endpoints that will always fail (dead/unreachable URLs)
    print_step "Creating dead endpoints..."
    local dead_url_1="http://nonexistent-service:9999/webhook"
    local dead_url_2="http://fake-receiver:8081/dead-endpoint"  # fake-receiver returns 404 for unknown paths
    local dead_url_3="http://timeout-service:8082/slow"         # non-existent service

    local dead_resp_1=$($harborctl endpoint create "$TENANT_ID" "$dead_url_1" --secret "$FAKE_SECRET" --server "$SERVER_HOST" --json)
    local dead_endpoint_1=$(echo "$dead_resp_1" | jq -r '.endpoint.id')

    local dead_resp_2=$($harborctl endpoint create "$TENANT_ID" "$dead_url_2" --secret "$FAKE_SECRET" --server "$SERVER_HOST" --json)
    local dead_endpoint_2=$(echo "$dead_resp_2" | jq -r '.endpoint.id')

    local dead_resp_3=$($harborctl endpoint create "$TENANT_ID" "$dead_url_3" --secret "$FAKE_SECRET" --server "$SERVER_HOST" --json)
    local dead_endpoint_3=$(echo "$dead_resp_3" | jq -r '.endpoint.id')

    print_success "Dead endpoints created: $dead_endpoint_1, $dead_endpoint_2, $dead_endpoint_3"

    # Create subscriptions for these dead endpoints
    print_step "Creating subscriptions for dead endpoints..."
    local dead_sub_1=$($harborctl subscription create "$TENANT_ID" "$dead_endpoint_1" "${EVENT_TYPE}.dlq_test" --server "$SERVER_HOST" --json)
    local dead_sub_2=$($harborctl subscription create "$TENANT_ID" "$dead_endpoint_2" "${EVENT_TYPE}.dlq_test" --server "$SERVER_HOST" --json)
    local dead_sub_3=$($harborctl subscription create "$TENANT_ID" "$dead_endpoint_3" "${EVENT_TYPE}.dlq_test" --server "$SERVER_HOST" --json)

    print_success "Dead subscriptions created"

    # Generate events that will immediately fail and go to DLQ
    print_step "Publishing events to dead endpoints (these will fail fast and hit DLQ)..."
    local dlq_count=0
    local dlq_events=30  # Generate 30 events that will fail quickly

    for i in $(seq 1 $dlq_events); do
        local payload="{\"demo\": true, \"type\": \"dlq_test\", \"attempt\": $i, \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\", \"expected_outcome\": \"DLQ\", \"reason\": \"dead_endpoint_test\"}"
        $harborctl event publish "$TENANT_ID" "${EVENT_TYPE}.dlq_test" "$payload" --server "$SERVER_HOST" --json > /dev/null 2>&1 || true
        dlq_count=$((dlq_count + 1))

        # Progress indicator
        if [ $((dlq_count % 10)) -eq 0 ]; then
            echo -n "💀"
        fi

        # Small delay to avoid overwhelming the system
        sleep 0.1
    done

    echo ""
    print_success "Generated $dlq_count events destined for DLQ (dead endpoints)"
    print_warning "These events will fail quickly and move to DLQ within 1-2 minutes instead of 15 minutes"
    print_info "Monitor NSQ admin at http://localhost:4171 to see DLQ message counts"
}

# Generate burst traffic to trigger alerts
generate_burst_traffic() {
    print_header "🚨 Generating Burst Traffic (Alert Trigger)"
    print_step "Generating high-failure burst traffic to trigger SLO alerts..."
    
    local harborctl="./bin/harborctl"
    local burst_duration=60  # 1 minute burst
    local start_time
    start_time=$(date +%s)
    local end_time=$((start_time + burst_duration))
    local burst_count=0
    
    # Generate mostly failures to trigger alerts
    while [ "$(date +%s)" -lt "$end_time" ]; do
        # 80% failure rate during burst to trigger alerts quickly
        local random=$((RANDOM % 100))
        local should_fail=$((random < 80))
        
        if [ "$should_fail" -eq 1 ]; then
            local payload="{\"demo\": true, \"type\": \"burst_failure\", \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\", \"burst_id\": \"burst-$burst_count\", \"severity\": \"high\"}"
            $harborctl event publish "$TENANT_ID" "${EVENT_TYPE}.failure" "$payload" --server "$SERVER_HOST" --json > /dev/null 2>&1 || true
        else
            local payload="{\"demo\": true, \"type\": \"burst_success\", \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\", \"burst_id\": \"burst-$burst_count\"}"
            $harborctl event publish "$TENANT_ID" "${EVENT_TYPE}.success" "$payload" --server "$SERVER_HOST" --json > /dev/null 2>&1 || true
        fi
        
        burst_count=$((burst_count + 1))
        
        # Higher RPS during burst
        sleep "$(echo "scale=3; 1.0 / $BURST_TRAFFIC_RPS" | bc)"
        
        if [ $((burst_count % 100)) -eq 0 ]; then
            echo -n "🔥"
        fi
    done
    
    echo ""
    print_success "Burst traffic completed: $burst_count requests with ~80% failure rate"
    print_warning "SLO burn rate alerts should trigger within 2-5 minutes"
}

# Demonstrate trace correlation
demonstrate_traces() {
    print_header "🔍 Demonstrating Distributed Tracing"
    print_step "Finding real trace ID from recent traces..."
    
    local harborctl="./bin/harborctl"
    
    # Get a real trace ID from recent logs or tempo
    local real_trace_id
    local start_time=$(( ($(date +%s) - 300) * 1000000000 ))
    local end_time=$(( $(date +%s) * 1000000000 ))
    real_trace_id=$(curl -s "http://localhost:3100/loki/api/v1/query_range?query=%7Bservice_name%3D~%22%2Fhh-%28ingest%7Cworker%29%22%7D%20%7C%20json%20%7C%20trace_id%20!%3D%20%22%22&start=${start_time}&end=${end_time}&limit=1" 2>/dev/null | jq -r '.data.result[0].values[0][1]' 2>/dev/null | jq -r '.trace_id' 2>/dev/null)
    
    # Fallback: try to get from Tempo
    if [[ -z "$real_trace_id" || "$real_trace_id" == "null" || "$real_trace_id" == "" ]]; then
        real_trace_id=$(curl -s 'http://localhost:3200/api/search?limit=1' 2>/dev/null | jq -r '.traces[0].traceID' 2>/dev/null)
    fi
    
    # Final fallback: generate events first, then find the trace
    if [[ -z "$real_trace_id" || "$real_trace_id" == "null" || "$real_trace_id" == "" ]]; then
        print_step "No recent traces found. Publishing events first to generate traces..."
        
        # Publish a few events to generate traces
        for i in {1..3}; do
            local payload="{\"demo\": true, \"type\": \"trace_generation\", \"step\": $i, \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}"
            $harborctl event publish "$TENANT_ID" "${EVENT_TYPE}.success" "$payload" --server "$SERVER_HOST" --json > /dev/null 2>&1 || true
            sleep 1
        done
        
        print_step "Waiting for traces to be processed..."
        sleep 5
        
        # Try again to get a real trace ID
        local retry_start_time=$(( ($(date +%s) - 60) * 1000000000 ))
        local retry_end_time=$(( $(date +%s) * 1000000000 ))
        real_trace_id=$(curl -s "http://localhost:3100/loki/api/v1/query_range?query=%7Bservice_name%3D~%22%2Fhh-%28ingest%7Cworker%29%22%7D%20%7C%20json%20%7C%20trace_id%20!%3D%20%22%22&start=${retry_start_time}&end=${retry_end_time}&limit=1" 2>/dev/null | jq -r '.data.result[0].values[0][1]' 2>/dev/null | jq -r '.trace_id' 2>/dev/null)
    fi
    
    if [[ -n "$real_trace_id" && "$real_trace_id" != "null" && "$real_trace_id" != "" ]]; then
        print_success "Found real trace ID from system: $real_trace_id"
        print_info "🎯 Use this trace ID in Grafana's Observability Correlation dashboard:"
        echo ""
        echo -e "   Trace ID: ${GREEN}$real_trace_id${NC}"
        echo ""
        print_info "Steps to explore in Grafana:"
        echo "1. Go to http://localhost:3000"
        echo "2. Navigate to the 'Observability Correlation' dashboard"  
        echo "3. Paste this trace ID: $real_trace_id"
        echo "4. View correlated logs, traces, and metrics"
    else
        print_warning "Could not find any real trace IDs from the system"
        print_info "Try running the demo again after some webhook traffic has been processed"
    fi
    
    # Also publish some additional correlated events for demo purposes
    print_step "Publishing additional demo events..."
    for i in {1..3}; do
        local payload="{\"demo\": true, \"type\": \"correlation_demo\", \"step\": $i, \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}"
        local event_resp
        event_resp=$($harborctl event publish "$TENANT_ID" "${EVENT_TYPE}.success" "$payload" --server "$SERVER_HOST" --json)
        local event_id
        event_id=$(echo "$event_resp" | jq -r '.eventId')
        
        print_info "Published demo event $i/3: $event_id"
        sleep 1
    done
}

# Check alert status
check_alerts() {
    print_header "🚨 Checking Alert Status"
    print_step "Querying Prometheus for active alerts..."
    
    # Query Prometheus alerts API
    local prometheus_url="http://localhost:9090"
    local alerts_response
    alerts_response=$(curl -s "$prometheus_url/api/v1/alerts" || echo '{"data":{"alerts":[]}}')
    local active_alerts
    active_alerts=$(echo "$alerts_response" | jq -r '.data.alerts | length' 2>/dev/null || echo "0")
    
    if [ "$active_alerts" -gt 0 ]; then
        print_warning "Found $active_alerts active alerts:"
        echo "$alerts_response" | jq -r '.data.alerts[] | "  • \(.labels.alertname): \(.annotations.summary)"' 2>/dev/null || echo "  • Unable to parse alert details"
    else
        print_info "No active alerts found (alerts may take 2-5 minutes to trigger)"
    fi
    
    # Check AlertManager
    print_step "Checking AlertManager status..."
    local alertmanager_url="http://localhost:9093"
    local am_response
    am_response=$(curl -s "$alertmanager_url/api/v1/alerts" || echo '{"data":[]}')
    local am_alerts
    am_alerts=$(echo "$am_response" | jq -r '.data | length' 2>/dev/null || echo "0")
    
    if [ "$am_alerts" -gt 0 ]; then
        print_warning "AlertManager has $am_alerts alerts"
    else
        print_info "AlertManager: No active alerts"
    fi
}

# Show observability stack URLs
show_observability_urls() {
    print_header "🌐 Observability Stack URLs"
    echo "Access these URLs to explore the generated data:"
    echo ""
    echo "📊 Grafana (Dashboards, Metrics, Logs, Traces):"
    echo "   http://localhost:3000"
    echo "   Default login: admin/admin"
    echo ""
    echo "📈 Prometheus (Raw Metrics & Alert Rules):" 
    echo "   http://localhost:9090"
    echo ""
    echo "🚨 AlertManager (Alert Management):"
    echo "   http://localhost:9093"
    echo ""
    echo "🔍 Tempo (Distributed Tracing - via Grafana):"
    echo "   http://localhost:3200"
    echo ""
    echo "📝 Loki (Log Aggregation - via Grafana):"
    echo "   http://localhost:3100"
    echo ""
    echo "🐰 NSQ Admin (Message Queue Monitoring):"
    echo "   http://localhost:4171"
    echo ""
    print_info "🎯 Recommended: Start with Grafana to see pre-configured dashboards"
}

# Generate realistic varied traffic
generate_realistic_traffic() {
    print_step "Generating realistic varied traffic patterns..."
    
    local harborctl="./bin/harborctl"
    local patterns=("user_signup" "order_placed" "payment_processed" "notification_sent" "data_sync")
    local tenants=("acme_corp" "beta_testing" "production_workload")
    
    # Generate different traffic patterns
    for pattern in "${patterns[@]}"; do
        for tenant in "${tenants[@]}"; do
            local payload="{\"demo\": true, \"pattern\": \"$pattern\", \"tenant\": \"$tenant\", \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\", \"metadata\": {\"version\": \"v1.2.3\", \"region\": \"us-west-2\"}}"
            
            # Vary success rate by pattern
            local success_rate=95
            case $pattern in
                "payment_processed") success_rate=99 ;;
                "data_sync") success_rate=90 ;;
                "notification_sent") success_rate=85 ;;
            esac
            
            local random=$((RANDOM % 100))
            if [ $random -lt $success_rate ]; then
                $harborctl event publish "$tenant" "${EVENT_TYPE}.success" "$payload" --server "$SERVER_HOST" --json > /dev/null 2>&1 || true
            else
                $harborctl event publish "$tenant" "${EVENT_TYPE}.failure" "$payload" --server "$SERVER_HOST" --json > /dev/null 2>&1 || true
            fi
            
            sleep 0.1  # Brief pause between events
        done
    done
    
    print_success "Generated varied traffic patterns across multiple tenants and event types"
}

# Main execution
main() {
    print_header "🚀 Starting Observability Demo"
    
    # Phase 1: Setup
    check_dependencies
    setup_webhooks
    
    # Phase 2: Generate baseline traffic  
    print_header "📈 Phase 1: Baseline Traffic Generation"
    generate_background_traffic &
    BACKGROUND_PID=$!
    
    # Phase 3: Generate varied realistic traffic
    generate_realistic_traffic
    
    # Wait for background traffic to complete and load its counts
    wait "$BACKGROUND_PID"
    if [ -s "$COUNTS_FILE" ]; then
      # shellcheck disable=SC1090
      source "$COUNTS_FILE"
    else
      print_warning "No counts file produced by background traffic; defaulting to zeros."
      TOTAL_REQUESTS=0
      SUCCESS_REQUESTS=0
      FAILURE_REQUESTS=0
    fi
    rm -f "$COUNTS_FILE" || true
    
    # Phase 4: Generate DLQ traffic for fast failures
    generate_dlq_traffic

    # Phase 5: Demonstrate tracing
    demonstrate_traces

    # Phase 6: Generate burst to trigger alerts
    generate_burst_traffic

    # Phase 7: Wait for metrics to propagate
    print_step "Waiting for metrics and alerts to propagate..."
    sleep 30
    
    # Phase 8: Check results
    check_alerts

    # Phase 9: Show access information
    show_observability_urls
    
    # Final summary
    print_header "✅ Demo Complete!"
    print_success "Generated ${TOTAL_REQUESTS:-0} total requests"
    print_success "Success rate: $(pct "${SUCCESS_REQUESTS:-0}" "${TOTAL_REQUESTS:-0}")%"
    print_success "Failure rate: $(pct "${FAILURE_REQUESTS:-0}" "${TOTAL_REQUESTS:-0}")%"
    
    echo ""
    print_info "🎯 Next Steps:"
    echo "1. Visit Grafana (http://localhost:3000) to explore dashboards"
    echo "2. Check Prometheus (http://localhost:9090) for alert status"
    echo "3. Look for traces in Grafana's Explore → Tempo"
    echo "4. Check logs in Grafana's Explore → Loki"
    echo "5. Monitor the NSQ queues at http://localhost:4171"
    
    print_warning "Note: Alerts may take 2-5 minutes to appear due to evaluation intervals"
}

# Handle cleanup on script exit
cleanup() {
    if [ -n "${BACKGROUND_PID:-}" ]; then
        kill "$BACKGROUND_PID" 2>/dev/null || true
    fi
    rm -f "$COUNTS_FILE" 2>/dev/null || true
}
trap cleanup EXIT

# Run the demo
main "$@"