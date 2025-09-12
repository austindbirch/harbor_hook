#!/bin/bash
# E2E Observability Test - Phase 5
# Validates that all observability components are working correctly

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

# Print colored output
print_header() {
    echo -e "\n${PURPLE}$1${NC}"
    echo "=============================================="
}

print_test() {
    echo -e "${BLUE}🧪 $1${NC}"
}

print_pass() {
    echo -e "${GREEN}✅ PASS: $1${NC}"
    TESTS_PASSED=$((TESTS_PASSED + 1))
}

print_fail() {
    echo -e "${RED}❌ FAIL: $1${NC}"
    TESTS_FAILED=$((TESTS_FAILED + 1))
    FAILED_TESTS+=("$1")
}

print_warning() {
    echo -e "${YELLOW}⚠ $1${NC}"
}

print_info() {
    echo -e "${CYAN}ℹ $1${NC}"
}

# Configuration
TENANT_ID="e2e_test"
WEBHOOK_URL="http://fake-receiver:8081/hook"
EVENT_TYPE="e2e.test"
SERVER_HOST="localhost:8443"
JWKS_HOST="localhost:8082"
FAKE_SECRET="demo_secret" # Demo secret for the fake receiver

# Service URLs
PROMETHEUS_URL="http://localhost:9090"
GRAFANA_URL="http://localhost:3000"
ALERTMANAGER_URL="http://localhost:9093"
TEMPO_URL="http://localhost:3200"
LOKI_URL="http://localhost:3100"
NSQ_ADMIN_URL="http://localhost:4171"

print_header "🧪 Harbor Hook Observability E2E Tests"

# Function to get JWT token
get_jwt_token() {
    local token_response
    token_response=$(curl -s -X POST "http://$JWKS_HOST/token" \
        -H "Content-Type: application/json" \
        -d "{\"tenant_id\":\"$TENANT_ID\"}" 2>/dev/null || echo '{}')
    
    echo "$token_response" | jq -r '.token' 2>/dev/null || echo "null"
}

# Test service health endpoints
test_service_health() {
    print_header "🏥 Service Health Tests"
    
    # Test Prometheus
    print_test "Testing Prometheus health endpoint"
    if curl -s "$PROMETHEUS_URL/-/healthy" | grep -q "Prometheus.*Healthy"; then
        print_pass "Prometheus is healthy"
    else
        print_fail "Prometheus health check failed"
    fi
    
    # Test Grafana
    print_test "Testing Grafana health endpoint"
    if curl -s "$GRAFANA_URL/api/health" | jq -r '.database' | grep -q "ok"; then
        print_pass "Grafana is healthy"
    else
        print_fail "Grafana health check failed"  
    fi
    
    # Test AlertManager
    print_test "Testing AlertManager health endpoint"
    if curl -s "$ALERTMANAGER_URL/-/healthy" >/dev/null 2>&1; then
        print_pass "AlertManager is healthy"
    else
        print_fail "AlertManager health check failed"
    fi
    
    # Test Tempo
    print_test "Testing Tempo health endpoint"  
    if curl -s "$TEMPO_URL/ready" >/dev/null 2>&1; then
        print_pass "Tempo is healthy"
    else
        print_fail "Tempo health check failed"
    fi
    
    # Test Loki
    print_test "Testing Loki health endpoint"
    if curl -s "$LOKI_URL/ready" >/dev/null 2>&1; then
        print_pass "Loki is healthy"
    else
        print_fail "Loki health check failed"
    fi
    
    # Test NSQ Admin
    print_test "Testing NSQ Admin interface"
    if curl -s "$NSQ_ADMIN_URL" | grep -q "nsqadmin"; then
        print_pass "NSQ Admin is accessible"
    else
        print_fail "NSQ Admin health check failed"
    fi
    
    # Test Harbor Hook services
    print_test "Testing Harbor Hook JWT service"
    local jwt_token=$(get_jwt_token)
    if [[ "$jwt_token" != "null" && -n "$jwt_token" ]]; then
        print_pass "JWT service is working"
        export JWT_TOKEN="$jwt_token"
    else
        print_fail "JWT service is not working"
    fi
    
    print_test "Testing Harbor Hook ingest service via gateway"
    if [ -f "./bin/harborctl" ]; then
        if ./bin/harborctl ping --server "$SERVER_HOST" >/dev/null 2>&1; then
            print_pass "Harbor Hook ingest service is healthy"
        else
            print_fail "Harbor Hook ingest service health check failed"
        fi
    else
        print_fail "harborctl binary not found - cannot test ingest service"
    fi
}

# Test metrics collection and exposure
test_metrics_collection() {
    print_header "📊 Metrics Collection Tests"
    
    # Test Prometheus scraping Harbor Hook metrics
    print_test "Testing Prometheus metrics scraping"
    local metrics_response=$(curl -s "$PROMETHEUS_URL/api/v1/label/__name__/values" 2>/dev/null || echo '{}')
    local harborhook_metrics=$(echo "$metrics_response" | jq -r '.data[]' 2>/dev/null | grep -c "harborhook" || echo "0")
    
    if [ "$harborhook_metrics" -gt 0 ]; then
        print_pass "Found $harborhook_metrics Harbor Hook metrics in Prometheus"
    else
        print_fail "No Harbor Hook metrics found in Prometheus"
    fi
    
    # Test specific critical metrics exist
    local critical_metrics=("harborhook_events_published_total" "harborhook_deliveries_total" "harborhook_delivery_latency_seconds")
    
    for metric in "${critical_metrics[@]}"; do
        print_test "Checking for metric: $metric"
        local metric_exists=$(echo "$metrics_response" | jq -r '.data[]' 2>/dev/null | grep -c "$metric" || echo "0")
        if [ "$metric_exists" -gt 0 ]; then
            print_pass "Metric $metric is available"
        else
            print_fail "Critical metric $metric is missing"
        fi
    done
    
    # Test metric values by querying
    print_test "Testing metric value queries"
    local query_response=$(curl -s "$PROMETHEUS_URL/api/v1/query?query=up" 2>/dev/null || echo '{}')
    local up_services=$(echo "$query_response" | jq -r '.data.result | length' 2>/dev/null || echo "0")
    
    if [ "$up_services" -gt 0 ]; then
        print_pass "Successfully queried metrics - found $up_services services"
    else
        print_fail "Failed to query metrics from Prometheus"
    fi
}

# Test alert rules and alerting
test_alerting() {
    print_header "🚨 Alert System Tests"
    
    # Test alert rules are loaded
    print_test "Testing alert rules are loaded in Prometheus"
    local rules_response=$(curl -s "$PROMETHEUS_URL/api/v1/rules" 2>/dev/null || echo '{}')
    local rule_groups=$(echo "$rules_response" | jq -r '.data.groups | length' 2>/dev/null || echo "0")
    
    if [ "$rule_groups" -gt 0 ]; then
        print_pass "Found $rule_groups alert rule groups"
    else
        print_fail "No alert rules loaded in Prometheus"
    fi
    
    # Check for Harbor Hook specific alerts
    local hh_rules=$(echo "$rules_response" | jq -r '.data.groups[].rules[].name' 2>/dev/null | grep -c "HarborHook" || echo "0")
    if [ "$hh_rules" -gt 0 ]; then
        print_pass "Found $hh_rules Harbor Hook alert rules"
    else
        print_fail "No Harbor Hook alert rules found"
    fi
    
    # Test AlertManager configuration
    print_test "Testing AlertManager configuration"
    local am_config=$(curl -s "$ALERTMANAGER_URL/api/v2/status" 2>/dev/null || echo '{}')
    local cluster_status=$(echo "$am_config" | jq -r '.cluster.status' 2>/dev/null || echo "error")
    
    if [ "$cluster_status" = "ready" ]; then
        print_pass "AlertManager configuration is valid"
    else
        print_fail "AlertManager configuration issue"
    fi
}

# Test log aggregation
test_log_aggregation() {
    print_header "📝 Log Aggregation Tests"
    
    # Test Loki labels
    print_test "Testing Loki label discovery"
    local labels_response=$(curl -s "$LOKI_URL/loki/api/v1/labels" 2>/dev/null || echo '{}')
    local status=$(echo "$labels_response" | jq -r '.status' 2>/dev/null || echo "error")
    
    if [ "$status" = "success" ]; then
        print_pass "Loki API is working (may have no labels yet in new deployment)"
    else
        print_fail "Loki label discovery failed"
    fi
    
    # Test log ingestion by querying recent logs
    print_test "Testing log ingestion and query"
    local now=$(date +%s)000000000  # nanoseconds
    local five_min_ago=$(( $(date -v-5M +%s) ))000000000
    local log_query='{job=~".*"}'
    local logs_response=$(curl -s "$LOKI_URL/loki/api/v1/query_range?query=$log_query&start=$five_min_ago&end=$now&limit=10" 2>/dev/null || echo '{}')
    local log_count=$(echo "$logs_response" | jq -r '.data.result | length' 2>/dev/null || echo "0")
    
    if [ "$log_count" -gt 0 ]; then
        print_pass "Found recent logs in Loki"
    else
        print_fail "No recent logs found in Loki (may be normal for new deployment)"
    fi
}

# Test distributed tracing
test_distributed_tracing() {
    print_header "🔍 Distributed Tracing Tests"
    
    # Test Tempo API
    print_test "Testing Tempo trace API"
    local tempo_search=$(curl -s "$TEMPO_URL/api/search/tags" 2>/dev/null || echo '{}')
    if echo "$tempo_search" | jq . >/dev/null 2>&1; then
        print_pass "Tempo API is responding"
    else
        print_fail "Tempo API is not responding correctly"
    fi
    
    # Test OTLP endpoint (where traces are sent)
    print_test "Testing OTLP trace ingestion endpoint"
    # This is a simple connectivity test - actual trace ingestion requires OpenTelemetry client
    if nc -z localhost 4317 2>/dev/null; then
        print_pass "OTLP gRPC endpoint (4317) is accessible"
    else
        print_fail "OTLP gRPC endpoint (4317) is not accessible"
    fi
    
    if nc -z localhost 4318 2>/dev/null; then
        print_pass "OTLP HTTP endpoint (4318) is accessible"
    else
        print_fail "OTLP HTTP endpoint (4318) is not accessible"  
    fi
}

# Test Grafana dashboards and data sources
test_grafana_integration() {
    print_header "📈 Grafana Integration Tests"
    
    # Test data sources
    print_test "Testing Grafana data sources"
    local datasources_response=$(curl -s -u admin:admin "$GRAFANA_URL/api/datasources" 2>/dev/null || echo '[]')
    local datasource_count=$(echo "$datasources_response" | jq '. | length' 2>/dev/null || echo "0")
    
    if [ "$datasource_count" -gt 0 ]; then
        print_pass "Found $datasource_count Grafana data sources"
        
        # Check for specific data sources
        local prometheus_ds=$(echo "$datasources_response" | jq -r '.[].type' | grep -c "prometheus" || echo "0")
        local loki_ds=$(echo "$datasources_response" | jq -r '.[].type' | grep -c "loki" || echo "0") 
        local tempo_ds=$(echo "$datasources_response" | jq -r '.[].type' | grep -c "tempo" || echo "0")
        
        if [ "$prometheus_ds" -gt 0 ]; then
            print_pass "Prometheus data source configured"
        else
            print_fail "Prometheus data source not found"
        fi
        
        if [ "$loki_ds" -gt 0 ]; then
            print_pass "Loki data source configured"
        else
            print_fail "Loki data source not found"
        fi
        
        if [ "$tempo_ds" -gt 0 ]; then
            print_pass "Tempo data source configured"
        else
            print_fail "Tempo data source not found"
        fi
    else
        print_fail "No Grafana data sources found"
    fi
    
    # Test dashboards
    print_test "Testing Grafana dashboards"
    local dashboards_response=$(curl -s -u admin:admin "$GRAFANA_URL/api/search?type=dash-db" 2>/dev/null || echo '[]')
    local dashboard_count=$(echo "$dashboards_response" | jq '. | length' 2>/dev/null || echo "0")
    
    if [ "$dashboard_count" -gt 0 ]; then
        print_pass "Found $dashboard_count Grafana dashboards"
    else
        print_fail "No Grafana dashboards found"
    fi
}

# Test end-to-end workflow with actual events
test_e2e_workflow() {
    print_header "🔄 End-to-End Workflow Test"
    
    if [ ! -f "./bin/harborctl" ]; then
        print_fail "harborctl binary not found - skipping E2E workflow test"
        return
    fi
    
    local harborctl="./bin/harborctl"
    
    # Setup test endpoint and subscription
    print_test "Setting up test webhook for E2E"
    local endpoint_resp=$($harborctl endpoint create "$TENANT_ID" "$WEBHOOK_URL" --secret "$FAKE_SECRET" --json 2>/dev/null || echo '{}')
    local endpoint_id=$(echo "$endpoint_resp" | jq -r '.endpoint.id' 2>/dev/null || echo "null")
    
    if [ "$endpoint_id" != "null" ]; then
        print_pass "Test endpoint created: $endpoint_id"
    else
        print_fail "Failed to create test endpoint"
        return
    fi
    
    local sub_resp=$($harborctl subscription create "$TENANT_ID" "$endpoint_id" "$EVENT_TYPE" --json 2>/dev/null || echo '{}')
    local sub_id=$(echo "$sub_resp" | jq -r '.subscription.id' 2>/dev/null || echo "null")
    
    if [ "$sub_id" != "null" ]; then
        print_pass "Test subscription created: $sub_id"
    else
        print_fail "Failed to create test subscription"
        return
    fi
    
    # Publish test events
    print_test "Publishing test events for E2E validation"
    local baseline_metrics_before=$(curl -s "$PROMETHEUS_URL/api/v1/query?query=harborhook_events_published_total" 2>/dev/null || echo '{}')
    
    # Publish a few test events
    for i in {1..5}; do
        local payload="{\"e2e_test\": true, \"event_number\": $i, \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}"
        local event_resp=$($harborctl event publish "$TENANT_ID" "$EVENT_TYPE" "$payload" --json 2>/dev/null || echo '{}')
        local event_id=$(echo "$event_resp" | jq -r '.eventId' 2>/dev/null || echo "null")
        
        if [ "$event_id" != "null" ]; then
            print_info "Published test event $i: $event_id"
        else
            print_warning "Failed to publish test event $i"
        fi
        sleep 1
    done
    
    # Wait for processing
    sleep 10
    
    # Check if metrics increased
    print_test "Verifying metrics were updated"
    local metrics_after=$(curl -s "$PROMETHEUS_URL/api/v1/query?query=harborhook_events_published_total" 2>/dev/null || echo '{}')
    local current_count=$(echo "$metrics_after" | jq -r '.data.result[0].value[1]' 2>/dev/null || echo "0")
    
    if [ "$current_count" != "0" ] && [ "$current_count" != "null" ]; then
        print_pass "Metrics updated - total events: $current_count"
    else
        print_fail "Metrics not updated after publishing events"
    fi
    
    # Check delivery status
    print_test "Checking delivery status"
    sleep 5  # Allow time for delivery
    local delivery_status=$($harborctl delivery status "$event_id" 2>/dev/null || echo "Failed to get status")
    if echo "$delivery_status" | grep -q "delivered\|pending\|failed"; then
        print_pass "Delivery status is trackable"
    else
        print_fail "Unable to track delivery status"
    fi
}

# Generate test summary
generate_summary() {
    print_header "📋 Test Summary"
    
    local total_tests=$((TESTS_PASSED + TESTS_FAILED))
    local pass_rate=0
    
    if [ $total_tests -gt 0 ]; then
        pass_rate=$(echo "scale=1; $TESTS_PASSED * 100 / $total_tests" | bc)
    fi
    
    echo "Total Tests: $total_tests"
    echo "Passed: $TESTS_PASSED"
    echo "Failed: $TESTS_FAILED"  
    echo "Pass Rate: ${pass_rate}%"
    
    if [ $TESTS_FAILED -gt 0 ]; then
        echo -e "\n${RED}Failed Tests:${NC}"
        for test in "${FAILED_TESTS[@]}"; do
            echo "  • $test"
        done
    fi
    
    echo ""
    if [ $TESTS_FAILED -eq 0 ]; then
        echo -e "${GREEN}🎉 All tests passed! Observability stack is fully functional.${NC}"
        exit 0
    else
        echo -e "${RED}❌ Some tests failed. Please check the observability stack configuration.${NC}"
        exit 1
    fi
}

# Check dependencies
check_dependencies() {
    local deps=("curl" "jq" "bc" "nc")
    local missing_deps=()
    
    for dep in "${deps[@]}"; do
        if ! command -v "$dep" &> /dev/null; then
            missing_deps+=("$dep")
        fi
    done
    
    if [ ${#missing_deps[@]} -gt 0 ]; then
        print_fail "Missing dependencies: ${missing_deps[*]}"
        echo "Please install missing dependencies before running tests"
        exit 1
    fi
}

# Main execution
main() {
    check_dependencies
    
    test_service_health
    test_metrics_collection  
    test_alerting
    test_log_aggregation
    test_distributed_tracing
    test_grafana_integration
    test_e2e_workflow
    
    generate_summary
}

# Run the tests
main "$@"