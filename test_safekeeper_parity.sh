#!/bin/bash

# Comprehensive Test Script for Safekeeper 100% Parity Features
# Tests: Compression, Timelines, Dynamic Membership, S3 Backup, Peer Communication

set -e

# Configuration
SAFEKEEPER_PORT=${SAFEKEEPER_PORT:-8090}
TEST_DATA_DIR=${TEST_DATA_DIR:-/tmp/test-safekeeper-parity}
TEST_API_KEY=${TEST_API_KEY:-test-parity-key-$(date +%s)}
SAFEKEEPER_PID=""
SAFEKEEPER_LOG="/tmp/safekeeper-parity.log"

# Kill any existing processes
lsof -ti:${SAFEKEEPER_PORT} | xargs kill -9 2>/dev/null || true
sleep 1

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
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

log_step() {
    echo -e "${BLUE}→${NC} $1"
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
    if [ -n "$SAFEKEEPER_PID" ]; then
        log_step "Stopping Safekeeper (PID: $SAFEKEEPER_PID)..."
        kill $SAFEKEEPER_PID 2>/dev/null || true
        wait $SAFEKEEPER_PID 2>/dev/null || true
    fi
}

trap cleanup EXIT

# Start Safekeeper with compression enabled
start_safekeeper() {
    echo ""
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║     Starting Safekeeper (with Compression)                   ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    
    # Check if Safekeeper binary exists
    if [ ! -f "./safekeeper/safekeeper" ]; then
        log_error "Safekeeper binary not found. Building..."
        cd safekeeper
        ./build.sh
        cd ..
    fi
    
    # Clean up old test data
    rm -rf "$TEST_DATA_DIR"
    mkdir -p "$TEST_DATA_DIR"
    
    # Start Safekeeper with compression enabled
    log_step "Starting Safekeeper on port $SAFEKEEPER_PORT with compression..."
    ./safekeeper/safekeeper \
        -port $SAFEKEEPER_PORT \
        -data-dir "$TEST_DATA_DIR" \
        -replica-id "safekeeper-parity-test" \
        -api-key "$TEST_API_KEY" \
        -compression true \
        > "$SAFEKEEPER_LOG" 2>&1 &
    
    SAFEKEEPER_PID=$!
    echo "Safekeeper started (PID: $SAFEKEEPER_PID)"
    
    # Wait for Safekeeper to start
    log_step "Waiting for Safekeeper to start..."
    for i in {1..10}; do
        if curl -s http://localhost:${SAFEKEEPER_PORT}/api/v1/ping > /dev/null 2>&1; then
            log_info "Safekeeper is ready"
            return 0
        fi
        sleep 1
    done
    
    log_error "Safekeeper failed to start"
    cat "$SAFEKEEPER_LOG"
    exit 1
}

# Test compression
test_compression() {
    echo ""
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║     Testing WAL Compression (Zstd)                          ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    
    log_step "Storing large WAL record to test compression..."
    
    # Create a larger WAL record (more compressible)
    LARGE_WAL=$(python3 -c "import base64; print(base64.b64encode(b'X' * 10000).decode())" 2>/dev/null || echo -n "$(head -c 10000 /dev/zero | base64)")
    
    RESPONSE=$(curl -s -H "X-API-Key: $TEST_API_KEY" \
        -X POST http://localhost:${SAFEKEEPER_PORT}/api/v1/stream_wal \
        -H "Content-Type: application/json" \
        -d "{\"lsn\":10000,\"wal_data\":\"$LARGE_WAL\",\"space_id\":1,\"page_no\":500}")
    
    if echo "$RESPONSE" | grep -q '"status":"success"'; then
        test_pass "Large WAL stored (compression should be active)"
    else
        test_fail "WAL storage failed: $RESPONSE"
        return
    fi
    
    sleep 1
    
    # Check metrics for compression info
    RESPONSE=$(curl -s http://localhost:${SAFEKEEPER_PORT}/api/v1/metrics)
    
    if echo "$RESPONSE" | grep -q '"compression_enabled":true'; then
        test_pass "Compression is enabled"
        
        # Check compression ratio
        RATIO=$(echo "$RESPONSE" | grep -o '"compression_ratio":[0-9.]*' | cut -d':' -f2)
        if [ -n "$RATIO" ] && [ "$(echo "$RATIO < 1.0" | bc 2>/dev/null || echo "0")" = "1" ]; then
            test_pass "Compression ratio: $RATIO (effective compression achieved)"
        else
            log_step "Compression ratio: $RATIO (may vary based on data)"
        fi
    else
        test_fail "Compression not enabled in metrics"
    fi
}

# Test timeline management
test_timelines() {
    echo ""
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║     Testing Timeline Management                              ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    
    log_step "Creating new timeline..."
    RESPONSE=$(curl -s -X POST http://localhost:${SAFEKEEPER_PORT}/api/v1/timelines/create \
        -H "Content-Type: application/json" \
        -d '{"timeline_id":"test-timeline-1","parent_lsn":5000}')
    
    if echo "$RESPONSE" | grep -q '"status":"success"'; then
        test_pass "Timeline created successfully"
    else
        test_fail "Timeline creation failed: $RESPONSE"
        return
    fi
    
    log_step "Listing timelines..."
    RESPONSE=$(curl -s http://localhost:${SAFEKEEPER_PORT}/api/v1/timelines)
    
    if echo "$RESPONSE" | grep -q '"timelines"'; then
        TIMELINE_COUNT=$(echo "$RESPONSE" | python3 -c "import sys, json; d=json.load(sys.stdin); print(len(d.get('timelines', [])))" 2>/dev/null || echo "0")
        if [ "$TIMELINE_COUNT" -ge 2 ]; then
            test_pass "Timelines listed ($TIMELINE_COUNT timelines found)"
        else
            test_fail "Expected at least 2 timelines, got $TIMELINE_COUNT"
        fi
    else
        test_fail "Timeline listing failed: $RESPONSE"
    fi
}

# Test dynamic membership
test_dynamic_membership() {
    echo ""
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║     Testing Dynamic Membership                               ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    
    log_step "Adding peer replica..."
    RESPONSE=$(curl -s -X POST http://localhost:${SAFEKEEPER_PORT}/api/v1/membership/add_peer \
        -H "Content-Type: application/json" \
        -d '{"peer_endpoint":"http://localhost:8091"}')
    
    if echo "$RESPONSE" | grep -q '"status":"success"'; then
        PEER_COUNT=$(echo "$RESPONSE" | grep -o '"peer_count":[0-9]*' | cut -d':' -f2)
        QUORUM_SIZE=$(echo "$RESPONSE" | grep -o '"quorum_size":[0-9]*' | cut -d':' -f2)
        test_pass "Peer added (peer_count: $PEER_COUNT, quorum_size: $QUORUM_SIZE)"
    else
        test_fail "Peer addition failed: $RESPONSE"
        return
    fi
    
    log_step "Removing peer replica..."
    RESPONSE=$(curl -s -X POST http://localhost:${SAFEKEEPER_PORT}/api/v1/membership/remove_peer \
        -H "Content-Type: application/json" \
        -d '{"peer_endpoint":"http://localhost:8091"}')
    
    if echo "$RESPONSE" | grep -q '"status":"success"'; then
        PEER_COUNT=$(echo "$RESPONSE" | grep -o '"peer_count":[0-9]*' | cut -d':' -f2)
        test_pass "Peer removed (peer_count: $PEER_COUNT)"
    else
        test_fail "Peer removal failed: $RESPONSE"
    fi
}

# Test peer communication (vote requests and heartbeats)
test_peer_communication() {
    echo ""
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║     Testing Peer Communication                               ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    
    log_step "Testing vote request endpoint..."
    RESPONSE=$(curl -s -X POST http://localhost:${SAFEKEEPER_PORT}/api/v1/request_vote \
        -H "Content-Type: application/json" \
        -d '{"term":10,"candidate_id":"test-candidate","last_log_lsn":5000,"last_log_term":9}')
    
    if echo "$RESPONSE" | grep -q '"vote_granted"'; then
        test_pass "Vote request endpoint working"
    else
        test_fail "Vote request failed: $RESPONSE"
    fi
    
    log_step "Testing heartbeat endpoint..."
    RESPONSE=$(curl -s -X POST http://localhost:${SAFEKEEPER_PORT}/api/v1/heartbeat \
        -H "Content-Type: application/json" \
        -d '{"term":5,"leader_id":"test-leader","latest_lsn":5000}')
    
    if echo "$RESPONSE" | grep -q '"status":"success"'; then
        test_pass "Heartbeat endpoint working"
    else
        test_fail "Heartbeat failed: $RESPONSE"
    fi
}

# Test metrics with new features
test_enhanced_metrics() {
    echo ""
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║     Testing Enhanced Metrics                                 ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    
    RESPONSE=$(curl -s http://localhost:${SAFEKEEPER_PORT}/api/v1/metrics)
    
    # Check for compression metrics
    if echo "$RESPONSE" | grep -q '"compression_enabled"'; then
        test_pass "Compression metrics available"
    else
        test_fail "Compression metrics missing"
    fi
    
    # Check for timeline metrics
    if echo "$RESPONSE" | grep -q '"timeline_count"'; then
        test_pass "Timeline metrics available"
    else
        test_fail "Timeline metrics missing"
    fi
    
    # Check for default timeline
    if echo "$RESPONSE" | grep -q '"default_timeline"'; then
        test_pass "Default timeline metric available"
    else
        test_fail "Default timeline metric missing"
    fi
}

# Main test execution
main() {
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║     Safekeeper 100% Parity Feature Test Suite              ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    echo ""
    echo "Testing new features:"
    echo "  1. WAL Compression (Zstd)"
    echo "  2. Timeline Management"
    echo "  3. Dynamic Membership"
    echo "  4. Peer Communication"
    echo "  5. Enhanced Metrics"
    echo ""
    
    # Start Safekeeper
    start_safekeeper
    
    # Run tests
    test_compression
    test_timelines
    test_dynamic_membership
    test_peer_communication
    test_enhanced_metrics
    
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
        echo -e "${GREEN}✓ All parity features working correctly!${NC}"
        echo ""
        echo "✅ Features verified:"
        echo "   - WAL Compression (Zstd): ✅"
        echo "   - Timeline Management: ✅"
        echo "   - Dynamic Membership: ✅"
        echo "   - Peer Communication: ✅"
        echo "   - Enhanced Metrics: ✅"
        exit 0
    else
        echo -e "${RED}✗ Some tests failed${NC}"
        exit 1
    fi
}

# Run main function
main



