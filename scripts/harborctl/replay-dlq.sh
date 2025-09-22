#!/bin/bash
# replay-dlq.sh - Bulk replay DLQ entries with JWT authentication

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m' # No Color

# Print colored output
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

# Configuration
BATCH_SIZE=${BATCH_SIZE:-10}
MAX_ENTRIES=${MAX_ENTRIES:-100}
CONFIRM=${CONFIRM:-true}
SERVER_HOST=${SERVER_HOST:-"localhost:8443"}  # HTTPS gateway
JWKS_HOST=${JWKS_HOST:-"localhost:8082"}      # JWT token server
TENANT_ID=${TENANT_ID:-"demo_tenant"}
HARBORCTL="${HARBORCTL:-./bin/harborctl}"
ENDPOINT_ID=""
REASON="Bulk replay via script with JWT auth"

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

# Usage function
usage() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  -e, --endpoint-id ID   Only replay deliveries for this endpoint"
    echo "  -r, --reason TEXT      Reason for replay (default: '$REASON')"
    echo "  -l, --limit NUM        Maximum entries to process (default: $MAX_ENTRIES)"
    echo "  --batch-size N         Process N entries at a time (default: $BATCH_SIZE)"
    echo "  --no-confirm          Skip confirmation prompt"
    echo "  --server HOST         Server address (default: $SERVER_HOST)"
    echo "  --tenant TENANT       Tenant ID (default: $TENANT_ID)"
    echo "  --help                Show this help message"
    echo ""
    echo "Environment variables:"
    echo "  BATCH_SIZE        Batch size for processing"
    echo "  MAX_ENTRIES       Maximum entries to process"
    echo "  CONFIRM           Set to 'false' to skip confirmation"
    echo "  SERVER_HOST       Server address"
    echo "  TENANT_ID         Tenant ID"
    echo "  JWKS_HOST         JWKS server address"
    exit 0
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
            MAX_ENTRIES="$2"
            shift 2
            ;;
        --batch-size)
            BATCH_SIZE="$2"
            shift 2
            ;;
        --no-confirm)
            CONFIRM=false
            shift
            ;;
        --server)
            SERVER_HOST="$2"
            shift 2
            ;;
        --tenant)
            TENANT_ID="$2"
            shift 2
            ;;
        --help)
            usage
            ;;
        *)
            print_error "Unknown option: $1"
            usage
            ;;
    esac
done

print_step "Harborhook DLQ Replay Tool (with JWT Authentication)"
echo "====================================================="
echo "Server: $SERVER_HOST (HTTPS)"
echo "JWKS server: $JWKS_HOST"
echo "Tenant: $TENANT_ID"
echo "Batch size: $BATCH_SIZE"
echo "Max entries: $MAX_ENTRIES"
echo "Reason: $REASON"
if [[ -n "$ENDPOINT_ID" ]]; then
    echo "Endpoint filter: $ENDPOINT_ID"
else
    echo "Endpoint filter: none (all endpoints)"
fi
echo ""

# Get JWT token
print_step "Obtaining JWT token"
JWT_TOKEN=$(get_jwt_token)
if [[ -z "$JWT_TOKEN" || "$JWT_TOKEN" == "null" ]]; then
    print_error "Failed to obtain valid JWT token"
    exit 1
fi
export JWT_TOKEN
print_success "JWT token obtained successfully"

# Check service health
print_step "Checking service health"
if ! $HARBORCTL ping --server "$SERVER_HOST" >/dev/null 2>&1; then
    print_error "Service is not available at $SERVER_HOST"
    exit 1
fi
print_success "Service is healthy"

# Get DLQ entries
print_step "Checking DLQ entries"
DLQ_ARGS="--limit $MAX_ENTRIES --server $SERVER_HOST"
if [[ -n "$ENDPOINT_ID" ]]; then
    DLQ_ARGS="$DLQ_ARGS --endpoint-id $ENDPOINT_ID"
fi

DLQ_OUTPUT=$($HARBORCTL delivery dlq $DLQ_ARGS 2>/dev/null || {
    print_error "Failed to fetch DLQ entries"
    exit 1
})

if [[ -z "$DLQ_OUTPUT" || "$DLQ_OUTPUT" == *"No DLQ entries found"* ]]; then
    print_success "No DLQ entries found - nothing to replay"
    exit 0
fi

# Count entries
ENTRY_COUNT=$(echo "$DLQ_OUTPUT" | grep -c "Event ID:" || echo "0")
print_warning "Found $ENTRY_COUNT DLQ entries"

if [[ "$ENTRY_COUNT" -eq 0 ]]; then
    print_success "No entries to process"
    exit 0
fi

# Confirmation
if [[ "$CONFIRM" == "true" ]]; then
    echo ""
    echo -e "${YELLOW}This will attempt to replay up to $ENTRY_COUNT DLQ entries in batches of $BATCH_SIZE${NC}"
    echo -e "${YELLOW}Do you want to continue? (y/N)${NC}"
    read -r response
    if [[ ! "$response" =~ ^[Yy]$ ]]; then
        print_warning "Operation cancelled by user"
        exit 0
    fi
fi

# Extract event IDs from DLQ output
EVENT_IDS=($(echo "$DLQ_OUTPUT" | grep "Event ID:" | awk '{print $3}' | head -n "$MAX_ENTRIES"))

if [[ ${#EVENT_IDS[@]} -eq 0 ]]; then
    print_error "No event IDs found in DLQ output"
    exit 1
fi

print_step "Processing ${#EVENT_IDS[@]} entries in batches of $BATCH_SIZE"

# Process in batches
TOTAL_PROCESSED=0
TOTAL_SUCCESS=0
TOTAL_FAILED=0

for ((i=0; i<${#EVENT_IDS[@]}; i+=BATCH_SIZE)); do
    BATCH_START=$((i+1))
    BATCH_END=$((i+BATCH_SIZE))
    if [[ $BATCH_END -gt ${#EVENT_IDS[@]} ]]; then
        BATCH_END=${#EVENT_IDS[@]}
    fi
    
    print_step "Processing batch $BATCH_START-$BATCH_END of ${#EVENT_IDS[@]}"
    
    # Process current batch
    for ((j=i; j<BATCH_END && j<${#EVENT_IDS[@]}; j++)); do
        EVENT_ID="${EVENT_IDS[j]}"
        printf "  Replaying event %s... " "$EVENT_ID"
        
        if $HARBORCTL delivery replay "$EVENT_ID" --reason "$REASON" --server "$SERVER_HOST" >/dev/null 2>&1; then
            echo -e "${GREEN}✓${NC}"
            TOTAL_SUCCESS=$((TOTAL_SUCCESS+1))
        else
            echo -e "${RED}✗${NC}"
            TOTAL_FAILED=$((TOTAL_FAILED+1))
        fi
        
        TOTAL_PROCESSED=$((TOTAL_PROCESSED+1))
    done
    
    # Brief pause between batches
    if [[ $BATCH_END -lt ${#EVENT_IDS[@]} ]]; then
        sleep 1
    fi
done

echo ""
print_step "Replay Summary"
echo "=============="
echo "Total processed: $TOTAL_PROCESSED"
print_success "Successful: $TOTAL_SUCCESS"
if [[ $TOTAL_FAILED -gt 0 ]]; then
    print_error "Failed: $TOTAL_FAILED"
else
    print_success "Failed: $TOTAL_FAILED"
fi

if [[ $TOTAL_SUCCESS -gt 0 ]]; then
    echo ""
    print_success "DLQ replay completed successfully!"
    echo "Monitor delivery status to verify successful reprocessing."
else
    print_warning "No entries were successfully replayed"
fi
