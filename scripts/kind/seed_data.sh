#!/bin/bash
# Data Seeding Script for KinD Environment
# Generates realistic test data for demos and testing

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
RELEASE_NAME="${RELEASE_NAME:-harborhook}"
TENANT_ID="${TENANT_ID:-tn_demo}"
SEED_SCALE="${SEED_SCALE:-medium}"  # small, medium, large

# Scale configurations
case "$SEED_SCALE" in
    small)
        NUM_ENDPOINTS=5
        NUM_EVENT_TYPES=10
        NUM_EVENTS=100
        ;;
    medium)
        NUM_ENDPOINTS=15
        NUM_EVENT_TYPES=25
        NUM_EVENTS=500
        ;;
    large)
        NUM_ENDPOINTS=30
        NUM_EVENT_TYPES=50
        NUM_EVENTS=2000
        ;;
    *)
        print_error "Invalid SEED_SCALE: $SEED_SCALE (use: small, medium, large)"
        exit 1
        ;;
esac

FAKE_SECRET="demo_secret"
FAILURE_RATE=10  # 10% of events will be destined for failing endpoints

# Port forward cleanup tracking
PORT_FORWARD_PIDS=()

print_header "🌱 Harborhook Data Seeding Script"
echo "Configuration:"
echo "  • Scale: $SEED_SCALE"
echo "  • Endpoints: $NUM_ENDPOINTS"
echo "  • Event Types: $NUM_EVENT_TYPES"
echo "  • Events: $NUM_EVENTS"
echo "  • Tenant: $TENANT_ID"
echo ""

# Cleanup function
cleanup() {
    if [ ${#PORT_FORWARD_PIDS[@]} -gt 0 ]; then
        print_step "Cleaning up port forwards..."
        for pid in "${PORT_FORWARD_PIDS[@]}"; do
            kill "$pid" 2>/dev/null || true
        done
        wait "${PORT_FORWARD_PIDS[@]}" 2>/dev/null || true
    fi
}
trap cleanup EXIT

# Check dependencies
check_dependencies() {
    print_step "Checking dependencies..."

    local missing_deps=()

    if ! command -v kubectl &> /dev/null; then
        missing_deps+=("kubectl")
    fi

    if ! command -v jq &> /dev/null; then
        missing_deps+=("jq")
    fi

    if ! command -v curl &> /dev/null; then
        missing_deps+=("curl")
    fi

    if [ ${#missing_deps[@]} -gt 0 ]; then
        print_error "Missing required dependencies: ${missing_deps[*]}"
        echo "Please install missing dependencies and try again"
        exit 1
    fi

    print_success "All dependencies available"
}

# Verify Kubernetes cluster is accessible
verify_cluster() {
    print_step "Verifying Kubernetes cluster..."

    if ! kubectl cluster-info &> /dev/null; then
        print_error "Cannot connect to Kubernetes cluster"
        echo "Please ensure kubectl is configured correctly"
        exit 1
    fi

    # Check if Harborhook is deployed
    if ! kubectl get deployment "${RELEASE_NAME}-ingest" &> /dev/null; then
        print_error "Harborhook does not appear to be deployed"
        echo "Expected to find deployment: ${RELEASE_NAME}-ingest"
        echo "Please deploy Harborhook first with 'helm install'"
        exit 1
    fi

    print_success "Cluster is accessible and Harborhook is deployed"
}

# Setup port forwards
setup_port_forwards() {
    print_step "Setting up port forwards..."

    # Port forward Envoy (API gateway)
    kubectl port-forward "svc/${RELEASE_NAME}-envoy" 8443:8443 >/dev/null 2>&1 &
    PORT_FORWARD_PIDS+=($!)

    # Port forward JWKS server
    kubectl port-forward "svc/${RELEASE_NAME}-jwks-server" 8082:8082 >/dev/null 2>&1 &
    PORT_FORWARD_PIDS+=($!)

    # Give port forwards time to establish
    print_step "Waiting for port forwards to be ready..."
    sleep 5

    # Verify port forwards are working
    if ! curl -sk https://localhost:8443/v1/ping >/dev/null 2>&1; then
        print_error "Port forward to Envoy not working"
        exit 1
    fi

    if ! curl -s http://localhost:8082/healthz >/dev/null 2>&1; then
        print_error "Port forward to JWKS server not working"
        exit 1
    fi

    print_success "Port forwards established"
}

# Get JWT token
get_jwt_token() {
    local token_response
    token_response=$(curl -s -X POST "http://localhost:8082/token" \
        -H "Content-Type: application/json" \
        -d "{\"tenant_id\":\"$TENANT_ID\"}")

    echo "$token_response" | jq -r '.token' 2>/dev/null || {
        print_error "Failed to parse JWT token from response"
        echo "Response: $token_response"
        exit 1
    }
}

# Generate event types
generate_event_types() {
    local event_types=()

    # Common business event patterns
    local categories=("user" "order" "payment" "notification" "system" "integration")
    local actions=("created" "updated" "deleted" "processed" "failed" "completed")

    for ((i=0; i<NUM_EVENT_TYPES; i++)); do
        local category="${categories[$((RANDOM % ${#categories[@]}))]}"
        local action="${actions[$((RANDOM % ${#actions[@]}))]}"
        event_types+=("${category}.${action}")
    done

    printf '%s\n' "${event_types[@]}"
}

# Create endpoints
create_endpoints() {
    print_step "Creating $NUM_ENDPOINTS webhook endpoints..."

    local endpoints_file=$(mktemp)
    local success_count=0
    local fail_count=0

    for ((i=1; i<=NUM_ENDPOINTS; i++)); do
        # Determine if this should be a working or failing endpoint
        local is_failing=$((RANDOM % 100 < FAILURE_RATE))
        local url

        if [ $is_failing -eq 1 ]; then
            # Create failing endpoint
            local fail_types=("http://nonexistent-service:9999/webhook" \
                             "http://${RELEASE_NAME}-fake-receiver:8081/fail" \
                             "http://dead-endpoint:8888/hook")
            url="${fail_types[$((RANDOM % ${#fail_types[@]}))]}"
            fail_count=$((fail_count + 1))
        else
            # Create working endpoint
            url="http://${RELEASE_NAME}-fake-receiver:8081/hook"
            success_count=$((success_count + 1))
        fi

        # Create endpoint
        local endpoint_response
        endpoint_response=$(curl -sk -X POST "https://localhost:8443/v1/tenants/${TENANT_ID}/endpoints" \
            -H "Authorization: Bearer $JWT_TOKEN" \
            -H "Content-Type: application/json" \
            -d "{
                \"tenant_id\": \"$TENANT_ID\",
                \"url\": \"$url\",
                \"secret\": \"$FAKE_SECRET\"
            }" 2>/dev/null)

        local endpoint_id
        endpoint_id=$(echo "$endpoint_response" | jq -r '.endpoint.id' 2>/dev/null)

        if [[ "$endpoint_id" != "null" && -n "$endpoint_id" ]]; then
            echo "$endpoint_id" >> "$endpoints_file"

            # Progress indicator
            if [ $((i % 5)) -eq 0 ]; then
                echo -n "."
            fi
        else
            print_warning "Failed to create endpoint $i"
        fi
    done

    echo ""
    print_success "Created $((success_count + fail_count)) endpoints ($success_count working, $fail_count failing)"

    echo "$endpoints_file"
}

# Create subscriptions
create_subscriptions() {
    local endpoints_file="$1"
    local event_types_file="$2"

    print_step "Creating subscriptions..."

    # Verify input files exist
    if [ ! -f "$endpoints_file" ]; then
        print_error "Endpoints file not found: $endpoints_file"
        exit 1
    fi

    if [ ! -f "$event_types_file" ]; then
        print_error "Event types file not found: $event_types_file"
        exit 1
    fi

    # Read endpoints and event types into arrays
    # Using while loop for Bash 3.2 compatibility (macOS default)
    local endpoints=()
    while IFS= read -r line; do
        [[ -n "$line" ]] && endpoints+=("$line")
    done < "$endpoints_file"

    local event_types=()
    while IFS= read -r line; do
        [[ -n "$line" ]] && event_types+=("$line")
    done < "$event_types_file"

    if [ ${#endpoints[@]} -eq 0 ]; then
        print_error "No endpoints available for subscriptions"
        exit 1
    fi

    if [ ${#event_types[@]} -eq 0 ]; then
        print_error "No event types available for subscriptions"
        exit 1
    fi

    local subscriptions_file=$(mktemp)
    local sub_count=0

    # Create 1-3 subscriptions per event type
    for event_type in "${event_types[@]}"; do
        local num_subs=$((RANDOM % 3 + 1))

        for ((i=0; i<num_subs; i++)); do
            # Pick a random endpoint
            local endpoint_id="${endpoints[$((RANDOM % ${#endpoints[@]}))]}"

            # Create subscription
            local sub_response
            sub_response=$(curl -sk -X POST "https://localhost:8443/v1/tenants/${TENANT_ID}/subscriptions" \
                -H "Authorization: Bearer $JWT_TOKEN" \
                -H "Content-Type: application/json" \
                -d "{
                    \"tenant_id\": \"$TENANT_ID\",
                    \"event_type\": \"$event_type\",
                    \"endpoint_id\": \"$endpoint_id\"
                }" 2>/dev/null)

            local sub_id
            sub_id=$(echo "$sub_response" | jq -r '.subscription.id' 2>/dev/null)

            if [[ "$sub_id" != "null" && -n "$sub_id" ]]; then
                echo "${event_type}|${endpoint_id}|${sub_id}" >> "$subscriptions_file"
                sub_count=$((sub_count + 1))

                # Progress indicator
                if [ $((sub_count % 10)) -eq 0 ]; then
                    echo -n "."
                fi
            fi
        done
    done

    echo ""
    print_success "Created $sub_count subscriptions"

    echo "$subscriptions_file"
}

# Generate events
generate_events() {
    local event_types_file="$1"

    print_step "Publishing $NUM_EVENTS events..."

    # Verify input file exists
    if [ ! -f "$event_types_file" ]; then
        print_error "Event types file not found: $event_types_file"
        exit 1
    fi

    # Read event types into array (Bash 3.2 compatible)
    local event_types=()
    while IFS= read -r line; do
        [[ -n "$line" ]] && event_types+=("$line")
    done < "$event_types_file"

    if [ ${#event_types[@]} -eq 0 ]; then
        print_error "No event types available"
        exit 1
    fi

    local event_count=0
    local success_count=0
    local error_count=0

    for ((i=1; i<=NUM_EVENTS; i++)); do
        # Pick random event type
        local event_type="${event_types[$((RANDOM % ${#event_types[@]}))]}"

        # Generate realistic payload
        local payload
        payload=$(cat <<EOF
{
    "event_id": "evt_$(date +%s)_$i",
    "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
    "data": {
        "user_id": "user_$((RANDOM % 1000))",
        "resource_id": "res_$((RANDOM % 5000))",
        "action": "$(echo $event_type | cut -d. -f2)",
        "metadata": {
            "source": "seed_script",
            "version": "1.0",
            "region": "us-west-2"
        }
    },
    "seed_info": {
        "batch": "$(date +%Y%m%d_%H%M%S)",
        "scale": "$SEED_SCALE",
        "sequence": $i
    }
}
EOF
        )

        # Publish event
        local event_response
        event_response=$(curl -sk -X POST "https://localhost:8443/v1/tenants/${TENANT_ID}/events:publish" \
            -H "Authorization: Bearer $JWT_TOKEN" \
            -H "Content-Type: application/json" \
            -d "{
                \"tenant_id\": \"$TENANT_ID\",
                \"event_type\": \"$event_type\",
                \"payload\": $payload,
                \"idempotency_key\": \"seed_${TENANT_ID}_${i}_$(date +%s)\"
            }" 2>/dev/null)

        local event_id
        event_id=$(echo "$event_response" | jq -r '.eventId' 2>/dev/null)

        if [[ "$event_id" != "null" && -n "$event_id" ]]; then
            success_count=$((success_count + 1))
        else
            error_count=$((error_count + 1))
        fi

        event_count=$((event_count + 1))

        # Progress indicator
        if [ $((i % 50)) -eq 0 ]; then
            echo -n "."
        fi

        if [ $((i % 250)) -eq 0 ]; then
            echo " [$i/$NUM_EVENTS events]"
        fi

        # Small delay to avoid overwhelming the system
        # Adjust based on scale
        case "$SEED_SCALE" in
            small)
                sleep 0.05
                ;;
            medium)
                sleep 0.02
                ;;
            large)
                sleep 0.01
                ;;
        esac
    done

    echo ""
    print_success "Published $success_count/$event_count events successfully"

    if [ $error_count -gt 0 ]; then
        print_warning "$error_count events failed to publish"
    fi
}

# Show statistics
show_statistics() {
    print_header "📊 Database Statistics"

    print_step "Querying database for seeded data statistics..."

    # Query database via PostgreSQL pod
    local stats
    stats=$(kubectl exec "${RELEASE_NAME}-postgres-0" -- env PGPASSWORD=postgres psql -U postgres -d harborhook -t -c "
        SELECT
            (SELECT COUNT(*) FROM harborhook.events WHERE tenant_id = '$TENANT_ID') as events,
            (SELECT COUNT(*) FROM harborhook.endpoints WHERE tenant_id = '$TENANT_ID') as endpoints,
            (SELECT COUNT(*) FROM harborhook.subscriptions WHERE tenant_id = '$TENANT_ID') as subscriptions,
            (SELECT COUNT(*) FROM harborhook.deliveries WHERE event_id IN (SELECT id FROM harborhook.events WHERE tenant_id = '$TENANT_ID')) as deliveries,
            (SELECT COUNT(*) FROM harborhook.deliveries WHERE event_id IN (SELECT id FROM harborhook.events WHERE tenant_id = '$TENANT_ID') AND status = 'delivered') as delivered,
            (SELECT COUNT(*) FROM harborhook.deliveries WHERE event_id IN (SELECT id FROM harborhook.events WHERE tenant_id = '$TENANT_ID') AND status = 'failed') as failed,
            (SELECT COUNT(*) FROM harborhook.deliveries WHERE event_id IN (SELECT id FROM harborhook.events WHERE tenant_id = '$TENANT_ID') AND status = 'dead') as dead
    " 2>/dev/null)

    if [ -n "$stats" ]; then
        # Parse the pipe-separated stats
        IFS='|' read -r events endpoints subscriptions deliveries delivered failed dead <<< "$stats"

        # Trim whitespace
        events=$(echo "$events" | tr -d '[:space:]')
        endpoints=$(echo "$endpoints" | tr -d '[:space:]')
        subscriptions=$(echo "$subscriptions" | tr -d '[:space:]')
        deliveries=$(echo "$deliveries" | tr -d '[:space:]')
        delivered=$(echo "$delivered" | tr -d '[:space:]')
        failed=$(echo "$failed" | tr -d '[:space:]')
        dead=$(echo "$dead" | tr -d '[:space:]')

        echo ""
        echo "  Events:        $events"
        echo "  Endpoints:     $endpoints"
        echo "  Subscriptions: $subscriptions"
        echo "  Deliveries:    $deliveries"
        echo "    ├─ Delivered: $delivered"
        echo "    ├─ Failed:    $failed"
        echo "    └─ Dead (DLQ): $dead"
        echo ""
    else
        print_warning "Could not retrieve statistics from database"
    fi
}

# Show access instructions
show_access_instructions() {
    print_header "🎯 Next Steps"

    echo "Your Harborhook instance is now seeded with test data!"
    echo ""
    echo "To explore the data:"
    echo ""
    echo "1. Query events via API:"
    echo "   curl -sk 'https://localhost:8443/v1/events/<event_id>/deliveries' \\"
    echo "     -H 'Authorization: Bearer \$JWT_TOKEN'"
    echo ""
    echo "2. Check NSQ queue (in another terminal):"
    echo "   kubectl port-forward svc/${RELEASE_NAME}-nsq-nsqadmin 4171:4171"
    echo "   open http://localhost:4171"
    echo ""
    echo "3. View logs:"
    echo "   kubectl logs -l app.kubernetes.io/component=worker --tail=100"
    echo "   kubectl logs -l app.kubernetes.io/component=ingest --tail=100"
    echo ""
    echo "4. Query database directly:"
    echo "   kubectl exec ${RELEASE_NAME}-postgres-0 -- env PGPASSWORD=postgres \\"
    echo "     psql -U postgres -d harborhook -c 'SELECT * FROM harborhook.events LIMIT 10;'"
    echo ""

    print_info "Your JWT token (valid for 1 hour):"
    echo "export JWT_TOKEN='$JWT_TOKEN'"
    echo ""

    print_warning "Note: Webhook deliveries happen asynchronously. Some deliveries may still be in progress."
    print_info "Wait 30-60 seconds for all deliveries to complete, then check statistics again."
}

# Main execution
main() {
    print_header "🚀 Starting Data Seeding"

    # Phase 1: Validation
    check_dependencies
    verify_cluster

    # Phase 2: Setup
    setup_port_forwards

    # Get JWT token
    print_step "Obtaining JWT token..."
    JWT_TOKEN=$(get_jwt_token)
    if [[ -z "$JWT_TOKEN" || "$JWT_TOKEN" == "null" ]]; then
        print_error "Failed to obtain valid JWT token"
        exit 1
    fi
    print_success "JWT token obtained"

    # Phase 3: Generate data model
    print_step "Generating event types..."
    event_types_file=$(mktemp)
    generate_event_types > "$event_types_file"
    print_success "Generated $NUM_EVENT_TYPES event types"

    # Phase 4: Create resources
    endpoints_file=$(create_endpoints)
    subscriptions_file=$(create_subscriptions "$endpoints_file" "$event_types_file")

    # Phase 5: Generate events
    generate_events "$event_types_file"

    # Phase 6: Wait for deliveries to process
    print_step "Waiting for webhook deliveries to process (30 seconds)..."
    sleep 30

    # Phase 7: Show results
    show_statistics

    # Cleanup temp files
    rm -f "$endpoints_file" "$subscriptions_file" "$event_types_file"

    # Final output
    print_header "✅ Data Seeding Complete!"
    show_access_instructions
}

# Run the script
main "$@"
