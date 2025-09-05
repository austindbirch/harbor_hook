#!/bin/bash
# monitor.sh - Monitoring script for Harbor Hook

set -e

# Configuration
REFRESH_INTERVAL=5
ENDPOINT_ID=""
LIMIT=10

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

usage() {
    echo "Usage: $0 [OPTIONS]"
    echo "Monitor Harbor Hook delivery status"
    echo ""
    echo "Options:"
    echo "  -e, --endpoint-id ID    Filter by endpoint ID"
    echo "  -i, --interval SEC      Refresh interval in seconds (default: 5)"
    echo "  -l, --limit NUM         Limit number of results (default: 10)"
    echo "  -h, --help              Show this help"
    exit 1
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -e|--endpoint-id)
            ENDPOINT_ID="$2"
            shift 2
            ;;
        -i|--interval)
            REFRESH_INTERVAL="$2"
            shift 2
            ;;
        -l|--limit)
            LIMIT="$2"
            shift 2
            ;;
        -h|--help)
            usage
            ;;
        *)
            echo "Unknown option: $1"
            usage
            ;;
    esac
done

check_harborctl() {
    # Check for local binary first, then global
    if [ -f "./bin/harborctl" ]; then
        HARBORCTL="./bin/harborctl"
        echo -e "${GREEN}Using local harborctl binary${NC}"
    elif command -v harborctl &> /dev/null; then
        HARBORCTL="harborctl"
        echo -e "${GREEN}Using global harborctl binary${NC}"
    else
        echo -e "${RED}❌ harborctl not found${NC}"
        echo "Please build and install it first: make build-cli && make install-cli"
        exit 1
    fi
}

check_service() {
    if ! $HARBORCTL ping >/dev/null 2>&1; then
        echo -e "${RED}❌ Harbor Hook service is not responding${NC}"
        return 1
    fi
    return 0
}

get_dlq_stats() {
    local dlq_args="--limit $LIMIT --json"
    if [[ -n "$ENDPOINT_ID" ]]; then
        dlq_args="$dlq_args --endpoint-id $ENDPOINT_ID"
    fi
    
    $HARBORCTL delivery dlq $dlq_args | jq -r '
        .dead | length as $count |
        if $count > 0 then
            group_by(.endpointId) | map({
                endpoint: .[0].endpointId,
                count: length
            }) | "DLQ Entries: \($count) total | " + (map("\(.endpoint): \(.count)") | join(", "))
        else
            "DLQ Entries: 0"
        end
    '
}

display_header() {
    clear
    echo -e "${BLUE}===========================================${NC}"
    echo -e "${BLUE}        Harbor Hook Monitor${NC}"
    echo -e "${BLUE}===========================================${NC}"
    echo -e "Refresh interval: ${REFRESH_INTERVAL}s | Limit: ${LIMIT}"
    if [[ -n "$ENDPOINT_ID" ]]; then
        echo -e "Filtering by endpoint: ${ENDPOINT_ID}"
    fi
    echo -e "Press Ctrl+C to exit"
    echo ""
}

display_service_status() {
    if check_service; then
        echo -e "${GREEN}✅ Service Status: HEALTHY${NC}"
    else
        echo -e "${RED}❌ Service Status: UNHEALTHY${NC}"
    fi
}

display_dlq_summary() {
    echo -e "\n${YELLOW}📊 Dead Letter Queue Summary${NC}"
    echo "$(get_dlq_stats)"
}

display_recent_dlq() {
    echo -e "\n${YELLOW}💀 Recent DLQ Entries${NC}"
    local dlq_args="--limit 5 --json"
    if [[ -n "$ENDPOINT_ID" ]]; then
        dlq_args="$dlq_args --endpoint-id $ENDPOINT_ID"
    fi
    
    local dlq_entries=$($HARBORCTL delivery dlq $dlq_args)
    local count=$(echo "$dlq_entries" | jq '.dead | length')
    
    if [[ "$count" -eq 0 ]]; then
        echo "  No entries in DLQ"
    else
        echo "$dlq_entries" | jq -r '
            .dead[] | 
            "  • \(.deliveryId) | Event: \(.eventId) | Error: \(.errorReason // "unknown") | DLQ: \(.dlqAt)"
        '
    fi
}

monitor_loop() {
    while true; do
        display_header
        display_service_status
        display_dlq_summary
        display_recent_dlq
        
        echo -e "\n${BLUE}Last updated: $(date)${NC}"
        sleep "$REFRESH_INTERVAL"
    done
}

main() {
    check_harborctl
    
    echo -e "${GREEN}Starting Harbor Hook monitor...${NC}"
    sleep 1
    
    # Set up trap to clean up on exit
    trap 'echo -e "\n${YELLOW}Monitor stopped${NC}"; exit 0' INT TERM
    
    monitor_loop
}

main "$@"
