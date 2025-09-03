#!/bin/bash
# replay-dlq.sh - Bulk replay script for DLQ entries

set -e

ENDPOINT_ID=""
REASON="Bulk replay via script"
DRY_RUN=false
LIMIT=50

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

usage() {
    echo "Usage: $0 [OPTIONS]"
    echo "Bulk replay deliveries from the dead letter queue"
    echo ""
    echo "Options:"
    echo "  -e, --endpoint-id ID    Only replay deliveries for this endpoint"
    echo "  -r, --reason TEXT       Reason for replay (default: 'Bulk replay via script')"
    echo "  -l, --limit NUM         Maximum number of deliveries to replay (default: 50)"
    echo "  -n, --dry-run           Show what would be replayed without actually doing it"
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
        -r|--reason)
            REASON="$2"
            shift 2
            ;;
        -l|--limit)
            LIMIT="$2"
            shift 2
            ;;
        -n|--dry-run)
            DRY_RUN=true
            shift
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
    if ! command -v harborctl &> /dev/null; then
        echo -e "${RED}❌ harborctl not found${NC}"
        echo "Please build and install it first: make build-cli && make install-cli"
        exit 1
    fi
}

get_dlq_entries() {
    local dlq_args="--limit $LIMIT --json"
    if [[ -n "$ENDPOINT_ID" ]]; then
        dlq_args="$dlq_args --endpoint-id $ENDPOINT_ID"
    fi
    
    harborctl delivery dlq $dlq_args | jq -r '.dead[].deliveryId'
}

replay_delivery() {
    local delivery_id="$1"
    
    if [[ "$DRY_RUN" == "true" ]]; then
        echo -e "  ${BLUE}[DRY RUN]${NC} Would replay: $delivery_id"
        return 0
    fi
    
    if harborctl delivery replay "$delivery_id" --reason "$REASON" >/dev/null 2>&1; then
        echo -e "  ${GREEN}✅${NC} Replayed: $delivery_id"
        return 0
    else
        echo -e "  ${RED}❌${NC} Failed to replay: $delivery_id"
        return 1
    fi
}

main() {
    check_harborctl
    
    echo -e "${BLUE}Harbor Hook DLQ Replay Tool${NC}"
    echo "============================"
    
    if [[ "$DRY_RUN" == "true" ]]; then
        echo -e "${YELLOW}🔍 DRY RUN MODE - No actual replays will be performed${NC}"
    fi
    
    echo "Configuration:"
    echo "  Limit: $LIMIT"
    echo "  Reason: $REASON"
    if [[ -n "$ENDPOINT_ID" ]]; then
        echo "  Endpoint filter: $ENDPOINT_ID"
    else
        echo "  Endpoint filter: none (all endpoints)"
    fi
    echo ""
    
    echo "Fetching DLQ entries..."
    deliveries=($(get_dlq_entries))
    
    if [[ ${#deliveries[@]} -eq 0 ]]; then
        echo -e "${GREEN}✅ No entries found in DLQ${NC}"
        exit 0
    fi
    
    echo -e "Found ${#deliveries[@]} entries to process"
    echo ""
    
    if [[ "$DRY_RUN" == "false" ]]; then
        echo -e "${YELLOW}⚠️  This will replay ${#deliveries[@]} failed deliveries.${NC}"
        read -p "Continue? (y/N): " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            echo "Aborted"
            exit 0
        fi
        echo ""
    fi
    
    echo "Processing deliveries..."
    
    success_count=0
    failure_count=0
    
    for delivery_id in "${deliveries[@]}"; do
        if replay_delivery "$delivery_id"; then
            ((success_count++))
        else
            ((failure_count++))
        fi
    done
    
    echo ""
    echo "Summary:"
    if [[ "$DRY_RUN" == "true" ]]; then
        echo -e "  ${BLUE}Would process:${NC} ${#deliveries[@]} deliveries"
    else
        echo -e "  ${GREEN}Successful:${NC} $success_count"
        echo -e "  ${RED}Failed:${NC} $failure_count"
        echo -e "  ${BLUE}Total:${NC} $((success_count + failure_count))"
    fi
}

main "$@"
