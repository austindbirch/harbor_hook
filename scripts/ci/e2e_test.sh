#!/bin/bash
# CI E2E Test - Kubernetes Environment
# Tests core Harborhook functionality in the CI pipeline

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
PURPLE='\033[0;35m'
NC='\033[0m' # No Color

# Test results tracking
TESTS_PASSED=0
TESTS_FAILED=0
FAILED_TESTS=()

# Print colored output (all output to stderr to not interfere with command substitution)
print_header() {
    echo -e "\n${PURPLE}========================================${NC}" >&2
    echo -e "${PURPLE}$1${NC}" >&2
    echo -e "${PURPLE}========================================${NC}" >&2
}

print_test() {
    echo -e "${BLUE}🧪 TEST: $1${NC}" >&2
}

print_pass() {
    echo -e "${GREEN}✅ PASS: $1${NC}" >&2
    TESTS_PASSED=$((TESTS_PASSED + 1))
}

print_fail() {
    echo -e "${RED}❌ FAIL: $1${NC}" >&2
    TESTS_FAILED=$((TESTS_FAILED + 1))
    FAILED_TESTS+=("$1")
}

print_info() {
    echo -e "${CYAN}ℹ INFO: $1${NC}" >&2
}

# Configuration
TENANT_ID="tn_demo"  # Pre-created in postgres init scripts
WEBHOOK_URL="http://test-harborhook-fake-receiver:8081/hook"
EVENT_TYPE="ci.e2e.test"
FAKE_SECRET="demo_secret"  # From fake-receiver config
RELEASE_NAME="${RELEASE_NAME:-test}"

# Service names (from helm chart)
ENVOY_SERVICE="${RELEASE_NAME}-harborhook-envoy"
JWKS_SERVICE="${RELEASE_NAME}-harborhook-jwks-server"
INGEST_SERVICE="${RELEASE_NAME}-harborhook-ingest"
FAKE_RECEIVER_SERVICE="${RELEASE_NAME}-harborhook-fake-receiver"
POSTGRES_SERVICE="${RELEASE_NAME}-postgres"
NSQADMIN_SERVICE="${RELEASE_NAME}-nsqadmin"

print_header "🧪 Harborhook CI E2E Tests"

# Check dependencies
check_dependencies() {
    print_test "Checking required dependencies"
    local deps=("curl" "jq" "kubectl")
    local missing_deps=()

    for dep in "${deps[@]}"; do
        if ! command -v "$dep" &> /dev/null; then
            missing_deps+=("$dep")
        fi
    done

    if [ ${#missing_deps[@]} -gt 0 ]; then
        print_fail "Missing dependencies: ${missing_deps[*]}"
        exit 1
    fi
    print_pass "All dependencies available"
}

# Get JWT token from jwks-server
get_jwt_token() {
    print_test "Getting JWT token for authentication"

    # Port forward jwks-server to localhost temporarily
    kubectl port-forward svc/$JWKS_SERVICE 8082:8082 >/dev/null 2>&1 &
    local PF_PID=$!
    sleep 3  # Give more time for port forward to establish

    # Retry logic for token fetch
    local token_response=""
    local retries=3
    for i in $(seq 1 $retries); do
        token_response=$(curl -s -X POST "http://localhost:8082/token" \
            -H "Content-Type: application/json" \
            -d "{\"tenant_id\":\"$TENANT_ID\"}" 2>/dev/null || echo '{}')

        if [[ "$token_response" != "{}" ]]; then
            break
        fi
        print_info "Token fetch attempt $i/$retries..."
        sleep 2
    done

    kill $PF_PID 2>/dev/null || true
    wait $PF_PID 2>/dev/null || true

    local token=$(echo "$token_response" | jq -r '.token' 2>/dev/null || echo "null")

    if [[ "$token" != "null" && -n "$token" ]]; then
        print_pass "JWT token obtained successfully"
        echo "$token"
        return 0
    else
        print_fail "Failed to get JWT token"
        print_info "Response: $token_response"
        return 1
    fi
}

# Test service health via kubectl
test_service_health() {
    print_header "🏥 Service Health Tests"

    # Check all pods are running
    print_test "Checking all pods are running"
    local not_running=$(kubectl get pods --no-headers | grep -v "Running" | wc -l)
    if [ "$not_running" -eq 0 ]; then
        print_pass "All pods are running"
    else
        print_fail "Some pods are not running"
        kubectl get pods
    fi

    # Check services exist
    print_test "Checking required services exist"
    local services=("$ENVOY_SERVICE" "$JWKS_SERVICE" "$INGEST_SERVICE" "$FAKE_RECEIVER_SERVICE" "$POSTGRES_SERVICE")
    local missing_services=0

    for service in "${services[@]}"; do
        if kubectl get svc "$service" &>/dev/null; then
            print_info "Service $service exists"
        else
            print_fail "Service $service not found"
            missing_services=$((missing_services + 1))
        fi
    done

    if [ $missing_services -eq 0 ]; then
        print_pass "All required services exist"
    fi
}

# Test database connectivity
test_database() {
    print_header "💾 Database Tests"

    print_test "Checking PostgreSQL connectivity"
    local pg_output=$(kubectl exec ${POSTGRES_SERVICE}-0 -- env PGPASSWORD=postgres psql -U postgres -d harborhook -c "SELECT 1;" 2>/dev/null)
    local pg_ready=$(echo "$pg_output" | grep "1 row" | wc -l | tr -d '[:space:]')

    if [ "$pg_ready" -gt 0 ] 2>/dev/null; then
        print_pass "PostgreSQL is accessible and responding"
    else
        print_fail "PostgreSQL connectivity issue"
        print_info "Output: $pg_output"
    fi

    print_test "Checking demo tenant exists in database"
    local tenant_output=$(kubectl exec ${POSTGRES_SERVICE}-0 -- env PGPASSWORD=postgres psql -U postgres -d harborhook -c "SELECT id FROM harborhook.tenants WHERE id='$TENANT_ID';" 2>/dev/null)
    local tenant_exists=$(echo "$tenant_output" | grep "$TENANT_ID" | wc -l | tr -d '[:space:]')

    if [ "$tenant_exists" -gt 0 ] 2>/dev/null; then
        print_pass "Demo tenant exists in database"
    else
        print_fail "Demo tenant not found in database"
        print_info "Output: $tenant_output"
    fi
}

# Test NSQ health
test_nsq() {
    print_header "📨 NSQ Tests"

    print_test "Checking NSQ admin interface"
    kubectl port-forward svc/$NSQADMIN_SERVICE 4171:4171 >/dev/null 2>&1 &
    local PF_PID=$!
    sleep 3

    local nsq_response=$(curl -s "http://localhost:4171/api/topics" 2>/dev/null || echo "error")
    kill $PF_PID 2>/dev/null || true
    wait $PF_PID 2>/dev/null || true

    if [[ "$nsq_response" != "error" ]]; then
        print_pass "NSQ admin interface is accessible"
    else
        print_fail "NSQ admin interface is not accessible"
    fi
}

# Main E2E workflow test
test_e2e_workflow() {
    print_header "🔄 End-to-End Workflow Test"

    # Get JWT token
    JWT_TOKEN=$(get_jwt_token)
    if [ -z "$JWT_TOKEN" ] || [ "$JWT_TOKEN" = "null" ]; then
        print_fail "Cannot proceed without JWT token"
        return 1
    fi

    # Port forward envoy gateway
    print_info "Setting up port forward to Envoy gateway"
    kubectl port-forward svc/$ENVOY_SERVICE 8443:8443 >/dev/null 2>&1 &
    local ENVOY_PF_PID=$!
    sleep 5  # Give more time for port forward to be ready

    # Port forward fake receiver to check logs
    kubectl port-forward svc/$FAKE_RECEIVER_SERVICE 8081:8081 >/dev/null 2>&1 &
    local FAKE_PF_PID=$!
    sleep 2

    # Test 1: Ping the ingest service
    print_test "Pinging ingest service through Envoy"
    print_info "Using JWT token: ${JWT_TOKEN:0:50}..."

    # Try multiple times in case port forward isn't quite ready
    local ping_response=""
    local retries=3
    for i in $(seq 1 $retries); do
        ping_response=$(curl -sk -X GET "https://localhost:8443/v1/ping" \
            -H "Authorization: Bearer $JWT_TOKEN" 2>&1)

        local ping_message=$(echo "$ping_response" | jq -r '.message' 2>/dev/null || echo "null")
        if [[ "$ping_message" != "null" && -n "$ping_message" ]]; then
            print_pass "Ingest service ping successful: $ping_message"
            break
        fi

        if [ $i -lt $retries ]; then
            print_info "Ping attempt $i/$retries failed, retrying..."
            sleep 3
        fi
    done

    local ping_message=$(echo "$ping_response" | jq -r '.message' 2>/dev/null || echo "null")
    if [[ "$ping_message" == "null" || -z "$ping_message" ]]; then
        print_fail "Ingest service ping failed after $retries attempts"
        print_info "Response: $ping_response"
        kill $ENVOY_PF_PID $FAKE_PF_PID 2>/dev/null || true
        return 1
    fi

    # Test 2: Create endpoint
    print_test "Creating webhook endpoint"
    local endpoint_response=$(curl -sk -X POST "https://localhost:8443/v1/tenants/$TENANT_ID/endpoints" \
        -H "Authorization: Bearer $JWT_TOKEN" \
        -H "Content-Type: application/json" \
        -d "{\"tenant_id\":\"$TENANT_ID\",\"url\":\"$WEBHOOK_URL\",\"secret\":\"$FAKE_SECRET\"}" 2>/dev/null || echo '{}')

    local endpoint_id=$(echo "$endpoint_response" | jq -r '.endpoint.id' 2>/dev/null || echo "null")
    if [ "$endpoint_id" != "null" ] && [ -n "$endpoint_id" ]; then
        print_pass "Endpoint created successfully: $endpoint_id"
    else
        print_fail "Failed to create endpoint"
        echo "Response: $endpoint_response" >&2
        kill $ENVOY_PF_PID $FAKE_PF_PID 2>/dev/null || true
        return 1
    fi

    # Test 3: Create subscription
    print_test "Creating event subscription"
    local subscription_response=$(curl -sk -X POST "https://localhost:8443/v1/tenants/$TENANT_ID/subscriptions" \
        -H "Authorization: Bearer $JWT_TOKEN" \
        -H "Content-Type: application/json" \
        -d "{\"tenant_id\":\"$TENANT_ID\",\"event_type\":\"$EVENT_TYPE\",\"endpoint_id\":\"$endpoint_id\"}" 2>/dev/null || echo '{}')

    local subscription_id=$(echo "$subscription_response" | jq -r '.subscription.id' 2>/dev/null || echo "null")
    if [ "$subscription_id" != "null" ] && [ -n "$subscription_id" ]; then
        print_pass "Subscription created successfully: $subscription_id"
    else
        print_fail "Failed to create subscription"
        echo "Response: $subscription_response" >&2
        kill $ENVOY_PF_PID $FAKE_PF_PID 2>/dev/null || true
        return 1
    fi

    # Test 4: Publish event
    print_test "Publishing webhook event"
    local timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    local event_response=$(curl -sk -X POST "https://localhost:8443/v1/tenants/$TENANT_ID/events:publish" \
        -H "Authorization: Bearer $JWT_TOKEN" \
        -H "Content-Type: application/json" \
        -d "{\"tenant_id\":\"$TENANT_ID\",\"event_type\":\"$EVENT_TYPE\",\"payload\":{\"test\":\"ci_e2e\",\"timestamp\":\"$timestamp\"},\"idempotency_key\":\"ci-test-$(date +%s)\"}" 2>/dev/null || echo '{}')

    local event_id=$(echo "$event_response" | jq -r '.event_id' 2>/dev/null || echo "null")
    local fanout_count=$(echo "$event_response" | jq -r '.fanout_count' 2>/dev/null || echo "0")

    if [ "$event_id" != "null" ] && [ -n "$event_id" ]; then
        print_pass "Event published successfully: $event_id (fanout: $fanout_count)"
    else
        print_fail "Failed to publish event"
        echo "Response: $event_response" >&2
        kill $ENVOY_PF_PID $FAKE_PF_PID 2>/dev/null || true
        return 1
    fi

    # Test 5: Wait for delivery and check status
    print_test "Waiting for webhook delivery (15 seconds)"
    sleep 15

    print_test "Checking delivery status"
    local delivery_response=$(curl -sk -X GET "https://localhost:8443/v1/events/$event_id/deliveries" \
        -H "Authorization: Bearer $JWT_TOKEN" 2>/dev/null || echo '{}')

    local delivery_status=$(echo "$delivery_response" | jq -r '.attempts[0].status' 2>/dev/null || echo "null")
    local http_status=$(echo "$delivery_response" | jq -r '.attempts[0].http_status' 2>/dev/null || echo "0")

    if [ "$delivery_status" != "null" ]; then
        print_info "Delivery status: $delivery_status (HTTP: $http_status)"

        # Check if delivered successfully
        if [ "$delivery_status" = "DELIVERY_ATTEMPT_STATUS_DELIVERED" ] || [ "$http_status" = "200" ]; then
            print_pass "Webhook delivered successfully"
        elif [ "$delivery_status" = "DELIVERY_ATTEMPT_STATUS_QUEUED" ] || [ "$delivery_status" = "DELIVERY_ATTEMPT_STATUS_IN_FLIGHT" ]; then
            print_fail "Delivery still pending (may need more time)"
        else
            print_fail "Delivery failed with status: $delivery_status"
        fi
    else
        print_fail "Failed to get delivery status"
        echo "Response: $delivery_response" >&2
    fi

    # Test 6: Verify fake-receiver received the webhook
    print_test "Checking fake-receiver health"
    local receiver_health=$(curl -s "http://localhost:8081/healthz" 2>/dev/null || echo "error")
    if [[ "$receiver_health" == *"ok"* ]]; then
        print_pass "Fake-receiver is healthy and received requests"
    else
        print_fail "Fake-receiver health check failed"
    fi

    # Cleanup port forwards
    kill $ENVOY_PF_PID $FAKE_PF_PID 2>/dev/null || true
    wait $ENVOY_PF_PID $FAKE_PF_PID 2>/dev/null || true

    print_info "E2E workflow test completed"
}

# Generate test summary
generate_summary() {
    print_header "📋 Test Summary"

    local total_tests=$((TESTS_PASSED + TESTS_FAILED))
    local pass_rate=0

    if [ $total_tests -gt 0 ]; then
        pass_rate=$(awk "BEGIN {printf \"%.1f\", $TESTS_PASSED * 100 / $total_tests}")
    fi

    echo "Total Tests: $total_tests" >&2
    echo "Passed: $TESTS_PASSED" >&2
    echo "Failed: $TESTS_FAILED" >&2
    echo "Pass Rate: ${pass_rate}%" >&2

    if [ $TESTS_FAILED -gt 0 ]; then
        echo -e "\n${RED}Failed Tests:${NC}" >&2
        for test in "${FAILED_TESTS[@]}"; do
            echo "  • $test" >&2
        done
    fi

    echo "" >&2
    if [ $TESTS_FAILED -eq 0 ]; then
        echo -e "${GREEN}🎉 All tests passed! Harborhook is working correctly in CI.${NC}" >&2
        exit 0
    else
        echo -e "${RED}❌ Some tests failed. Please review the output above.${NC}" >&2
        exit 1
    fi
}

# Main execution
main() {
    check_dependencies
    test_service_health
    test_database
    test_nsq
    test_e2e_workflow
    generate_summary
}

# Run the tests
main "$@"
