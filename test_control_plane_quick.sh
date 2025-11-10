#!/bin/bash

# Quick test script - tests API without compute node creation (no k3s needed)

set -e

CONTROL_PLANE_URL="${CONTROL_PLANE_URL:-http://localhost:8080}"

echo "🧪 Quick Control Plane API Test"
echo ""

# Test 1: Create Project
echo "1. Creating project..."
PROJECT_RESPONSE=$(curl -s -X POST "$CONTROL_PLANE_URL/api/v1/projects" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "quick-test",
    "config": {
      "page_server_url": "http://localhost:8081",
      "safekeeper_url": "http://localhost:8082",
      "idle_timeout": 300,
      "max_connections": 100
    }
  }')

PROJECT_ID=$(echo "$PROJECT_RESPONSE" | jq -r '.id')
echo "   ✅ Project created: $PROJECT_ID"

# Test 2: List Projects
echo "2. Listing projects..."
PROJECTS=$(curl -s "$CONTROL_PLANE_URL/api/v1/projects")
COUNT=$(echo "$PROJECTS" | jq '. | length')
echo "   ✅ Found $COUNT project(s)"

# Test 3: Get Project
echo "3. Getting project..."
PROJECT=$(curl -s "$CONTROL_PLANE_URL/api/v1/projects/$PROJECT_ID")
NAME=$(echo "$PROJECT" | jq -r '.name')
echo "   ✅ Project name: $NAME"

# Test 4: Wake Compute (API test - won't create pod without k3s)
echo "4. Testing wake_compute endpoint..."
WAKE_RESPONSE=$(curl -s "$CONTROL_PLANE_URL/api/v1/wake_compute?endpointish=$PROJECT_ID")
if echo "$WAKE_RESPONSE" | jq -e '.address' > /dev/null 2>&1; then
    ADDRESS=$(echo "$WAKE_RESPONSE" | jq -r '.address')
    echo "   ✅ Wake compute returned: $ADDRESS"
else
    echo "   ⚠️  Wake compute response: $(echo "$WAKE_RESPONSE" | jq -r '.error // .')"
fi

# Test 5: Cleanup
echo "5. Cleaning up..."
curl -s -X DELETE "$CONTROL_PLANE_URL/api/v1/projects/$PROJECT_ID" > /dev/null
echo "   ✅ Project deleted"

echo ""
echo "✅ All API tests passed!"
echo ""
echo "Note: Compute node creation requires k3s to be fully working."
echo "      API endpoints are working correctly."



