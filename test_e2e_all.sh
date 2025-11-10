#!/bin/bash

# Comprehensive End-to-End Test Script for Page Server
# Tests all storage backends: File, S3, and Hybrid (Neon-style tiered caching)

set -e

# Configuration
PAGE_SERVER_PORT=${PAGE_SERVER_PORT:-8090}
TEST_DATA_DIR=${TEST_DATA_DIR:-/tmp/test-page-server-e2e}
TEST_API_KEY=${TEST_API_KEY:-test-e2e-key-$(date +%s)}
PAGE_SERVER_PID=""
PAGE_SERVER_LOG="/tmp/page-server-e2e.log"

# Wasabi S3 Configuration (for S3 and Hybrid tests)
S3_ENDPOINT="https://s3.wasabisys.com"
S3_BUCKET="sb-mariadb"
S3_REGION="us-east-1"
S3_ACCESS_KEY="X7SMWFBIMHK761MZDCM4"
S3_SECRET_KEY="HeCjI9zsWe6lemh42fmCCugfyF06f7zXlyb9VY0G"

# Kill any existing Page Server on this port
lsof -ti:${PAGE_SERVER_PORT} | xargs kill -9 2>/dev/null || true
sleep 1

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test counters
TESTS_PASSED=0
TESTS_FAILED=0

# Helper functions
log_info() {
    echo -e "${GREEN}✓${NC} $1"
}

log_error() {
    echo -e "${RED}✗${NC} $1"
    TESTS_FAILED=$((TESTS_FAILED + 1))
}

log_warn() {
    echo -e "${YELLOW}⚠${NC} $1"
}

test_pass() {
    TESTS_PASSED=$((TESTS_PASSED + 1))
    log_info "$1"
}

test_fail() {
    log_error "$1"
}

# Cleanup function
cleanup() {
    if [ -n "$PAGE_SERVER_PID" ]; then
        echo ""
        echo "Stopping Page Server (PID: $PAGE_SERVER_PID)..."
        kill $PAGE_SERVER_PID 2>/dev/null || true
        wait $PAGE_SERVER_PID 2>/dev/null || true
    fi
}

trap cleanup EXIT

# Start Page Server with specified backend
start_page_server() {
    local backend=$1
    echo "=== Starting Page Server (Backend: $backend) ==="
    
    # Check if Page Server binary exists
    if [ ! -f "./page-server/page-server" ]; then
        log_error "Page Server binary not found. Building..."
        cd page-server
        go build -o page-server *.go
        cd ..
    fi
    
    # Clean up old test data
    rm -rf "$TEST_DATA_DIR"
    
    # Build command based on backend
    local cmd="./page-server/page-server -port $PAGE_SERVER_PORT -data-dir \"$TEST_DATA_DIR\" -cache-size 100 -api-key \"$TEST_API_KEY\""
    
    case "$backend" in
        "file")
            # File storage (default)
            ;;
        "s3")
            cmd="$cmd -storage-backend s3 -s3-endpoint $S3_ENDPOINT -s3-bucket $S3_BUCKET -s3-region $S3_REGION -s3-access-key $S3_ACCESS_KEY -s3-secret-key $S3_SECRET_KEY -s3-use-ssl true"
            ;;
        "hybrid")
            cmd="$cmd -storage-backend hybrid -s3-endpoint $S3_ENDPOINT -s3-bucket $S3_BUCKET -s3-region $S3_REGION -s3-access-key $S3_ACCESS_KEY -s3-secret-key $S3_SECRET_KEY -s3-use-ssl true"
            ;;
    esac
    
    # Start Page Server
    eval "$cmd > \"$PAGE_SERVER_LOG\" 2>&1 &"
    PAGE_SERVER_PID=$!
    echo "Page Server started (PID: $PAGE_SERVER_PID, Backend: $backend)"
    
    # Wait for server to start
    echo "Waiting for Page Server to start..."
    for i in {1..10}; do
        if curl -s http://localhost:${PAGE_SERVER_PORT}/api/v1/ping > /dev/null 2>&1; then
            log_info "Page Server is ready"
            return 0
        fi
        sleep 1
    done
    
    log_error "Page Server failed to start"
    cat "$PAGE_SERVER_LOG"
    exit 1
}

# Common test functions
test_ping() {
    RESPONSE=$(curl -s http://localhost:${PAGE_SERVER_PORT}/api/v1/ping)
    if echo "$RESPONSE" | grep -q '"status":"ok"'; then
        test_pass "Ping endpoint working"
    else
        test_fail "Ping endpoint failed: $RESPONSE"
    fi
}

test_authentication() {
    HTTP_CODE=$(curl -s -o /tmp/auth_test_response.txt -w "%{http_code}" -X POST http://localhost:${PAGE_SERVER_PORT}/api/v1/get_page \
        -H "Content-Type: application/json" \
        -d '{"space_id":1,"page_no":42,"lsn":1000}')
    BODY=$(cat /tmp/auth_test_response.txt)
    
    if [ "$HTTP_CODE" = "401" ] && echo "$BODY" | grep -q "Authentication required"; then
        test_pass "Authentication required (correctly blocks unauthorized)"
    else
        test_fail "Authentication not working: HTTP $HTTP_CODE"
    fi
}

test_wal_and_page() {
    # Stream WAL
    WAL_DATA=$(echo -n "Test WAL Data" | base64)
    RESPONSE=$(curl -s -H "X-API-Key: $TEST_API_KEY" \
        -X POST http://localhost:${PAGE_SERVER_PORT}/api/v1/stream_wal \
        -H "Content-Type: application/json" \
        -d "{\"lsn\":1000,\"wal_data\":\"$WAL_DATA\",\"space_id\":1,\"page_no\":42}")
    
    if echo "$RESPONSE" | grep -q '"status":"success"'; then
        test_pass "WAL streaming successful"
    else
        test_fail "WAL streaming failed: $RESPONSE"
        return
    fi
    
    sleep 1
    
    # Get page
    RESPONSE=$(curl -s -H "X-API-Key: $TEST_API_KEY" \
        -X POST http://localhost:${PAGE_SERVER_PORT}/api/v1/get_page \
        -H "Content-Type: application/json" \
        -d '{"space_id":1,"page_no":42,"lsn":1000}')
    
    if echo "$RESPONSE" | grep -q '"status":"success"'; then
        test_pass "Page retrieval successful"
    else
        test_fail "Page retrieval failed: $RESPONSE"
    fi
}

test_batch_operations() {
    # Create pages
    WAL_DATA1=$(echo -n "Batch Test 1" | base64)
    curl -s -H "X-API-Key: $TEST_API_KEY" \
        -X POST http://localhost:${PAGE_SERVER_PORT}/api/v1/stream_wal \
        -H "Content-Type: application/json" \
        -d "{\"lsn\":5000,\"wal_data\":\"$WAL_DATA1\",\"space_id\":1,\"page_no\":200}" > /dev/null
    
    WAL_DATA2=$(echo -n "Batch Test 2" | base64)
    curl -s -H "X-API-Key: $TEST_API_KEY" \
        -X POST http://localhost:${PAGE_SERVER_PORT}/api/v1/stream_wal \
        -H "Content-Type: application/json" \
        -d "{\"lsn\":6000,\"wal_data\":\"$WAL_DATA2\",\"space_id\":1,\"page_no\":201}" > /dev/null
    
    sleep 1
    
    # Test batch
    RESPONSE=$(curl -s -H "X-API-Key: $TEST_API_KEY" \
        -X POST http://localhost:${PAGE_SERVER_PORT}/api/v1/get_pages \
        -H "Content-Type: application/json" \
        -d '{"pages":[{"space_id":1,"page_no":200,"lsn":5000},{"space_id":1,"page_no":201,"lsn":6000}]}')
    
    SUCCESS_COUNT=$(echo "$RESPONSE" | python3 -c "import sys, json; d=json.load(sys.stdin); print(sum(1 for p in d.get('pages', []) if p.get('status') == 'success'))" 2>/dev/null || echo "0")
    if [ "$SUCCESS_COUNT" -eq 2 ]; then
        test_pass "Batch operations successful ($SUCCESS_COUNT/2)"
    else
        test_fail "Batch operations failed: $SUCCESS_COUNT/2"
    fi
}

test_metrics() {
    RESPONSE=$(curl -s -H "X-API-Key: $TEST_API_KEY" \
        http://localhost:${PAGE_SERVER_PORT}/api/v1/metrics)
    
    if echo "$RESPONSE" | grep -q '"cache"'; then
        test_pass "Metrics endpoint working"
    else
        test_fail "Metrics endpoint failed: $RESPONSE"
    fi
}

test_time_travel() {
    WAL_DATA1=$(echo -n "Time Travel 1" | base64)
    curl -s -H "X-API-Key: $TEST_API_KEY" \
        -X POST http://localhost:${PAGE_SERVER_PORT}/api/v1/stream_wal \
        -H "Content-Type: application/json" \
        -d "{\"lsn\":7000,\"wal_data\":\"$WAL_DATA1\",\"space_id\":1,\"page_no\":300}" > /dev/null
    
    sleep 1
    
    RESPONSE=$(curl -s -H "X-API-Key: $TEST_API_KEY" \
        -X POST http://localhost:${PAGE_SERVER_PORT}/api/v1/time_travel \
        -H "Content-Type: application/json" \
        -d '{"space_id":1,"page_no":300,"lsn":7000}')
    
    if echo "$RESPONSE" | grep -q '"status":"success"'; then
        test_pass "Time-travel query successful"
    else
        test_fail "Time-travel query failed: $RESPONSE"
    fi
}

test_snapshots() {
    RESPONSE=$(curl -s -H "X-API-Key: $TEST_API_KEY" \
        -X POST http://localhost:${PAGE_SERVER_PORT}/api/v1/snapshots/create \
        -H "Content-Type: application/json" \
        -d '{"description":"E2E test snapshot"}')
    
    if echo "$RESPONSE" | grep -q '"status":"success"'; then
        test_pass "Snapshot creation successful"
    else
        test_fail "Snapshot creation failed: $RESPONSE"
    fi
}

# Test suite for file storage
test_file_storage() {
    echo ""
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║     File Storage Backend Tests                              ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    
    start_page_server "file"
    
    test_ping
    test_authentication
    test_wal_and_page
    test_batch_operations
    test_metrics
    test_time_travel
    test_snapshots
    
    cleanup
    sleep 2
}

# Test suite for S3 storage
test_s3_storage() {
    echo ""
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║     S3 Storage Backend Tests                               ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    
    start_page_server "s3"
    
    test_ping
    test_authentication
    test_wal_and_page
    test_batch_operations
    test_metrics
    test_time_travel
    test_snapshots
    
    cleanup
    sleep 2
}

# Test suite for hybrid storage
test_hybrid_storage() {
    echo ""
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║     Hybrid Storage (Neon-Style Tiered Caching) Tests        ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    
    start_page_server "hybrid"
    
    test_ping
    test_authentication
    test_wal_and_page
    test_batch_operations
    test_metrics
    test_time_travel
    test_snapshots
    
    # Test tiered storage metrics
    RESPONSE=$(curl -s -H "X-API-Key: $TEST_API_KEY" \
        http://localhost:${PAGE_SERVER_PORT}/api/v1/metrics)
    
    if echo "$RESPONSE" | grep -q '"tiered_storage"'; then
        test_pass "Tiered storage metrics available"
    else
        test_fail "Tiered storage metrics not available"
    fi
    
    cleanup
    sleep 2
}

# Main test execution
main() {
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║     Page Server Comprehensive E2E Test Suite                ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    echo ""
    echo "Testing all storage backends:"
    echo "  1. File Storage"
    echo "  2. S3 Storage (Wasabi)"
    echo "  3. Hybrid Storage (Neon-style tiered caching)"
    echo ""
    
    # Run test suites
    test_file_storage
    test_s3_storage
    test_hybrid_storage
    
    # Print summary
    echo ""
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║                    Test Summary                              ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    echo ""
    echo "Tests Passed: $TESTS_PASSED"
    echo "Tests Failed: $TESTS_FAILED"
    echo ""
    
    if [ $TESTS_FAILED -eq 0 ]; then
        echo -e "${GREEN}✓ All tests passed!${NC}"
        exit 0
    else
        echo -e "${RED}✗ Some tests failed${NC}"
        exit 1
    fi
}

# Run main function
main

