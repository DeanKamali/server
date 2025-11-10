#!/bin/bash

# Start Control Plane Server
# This script starts the control plane with default or custom configuration

set -e

# Default configuration
PORT="${PORT:-8080}"
DB_TYPE="${DB_TYPE:-sqlite}"
DB_DSN="${DB_DSN:-}"

# Auto-detect k3s kubeconfig if available
if [ -f "/etc/rancher/k3s/k3s.yaml" ] && [ -z "$KUBECONFIG" ]; then
    # Use k3s config if user config doesn't exist or points to remote
    if [ ! -f "$HOME/.kube/config" ] || grep -q "104.167.198.243" "$HOME/.kube/config" 2>/dev/null; then
        KUBECONFIG="/etc/rancher/k3s/k3s.yaml"
        echo "Auto-detected k3s kubeconfig"
    else
        KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config}"
    fi
else
    KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config}"
fi

NAMESPACE="${NAMESPACE:-default}"
IDLE_TIMEOUT="${IDLE_TIMEOUT:-5m}"
CHECK_INTERVAL="${CHECK_INTERVAL:-30s}"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${GREEN}Starting Control Plane Server${NC}"
echo ""
echo "Configuration:"
echo "  Port: $PORT"
echo "  Database Type: $DB_TYPE"
if [ "$DB_TYPE" = "postgres" ]; then
    if [ -z "$DB_DSN" ]; then
        DB_DSN="postgres://postgres:postgres@localhost:5432/control_plane?sslmode=disable"
    fi
    echo "  Database DSN: $DB_DSN"
    # Check if database is accessible
    echo -e "${YELLOW}Checking PostgreSQL connection...${NC}"
    if ! psql "$DB_DSN" -c "SELECT 1;" > /dev/null 2>&1; then
        echo -e "${RED}Error: Cannot connect to PostgreSQL${NC}"
        echo ""
        echo "Please ensure PostgreSQL is running and accessible."
        echo "You can create the database with:"
        echo "  createdb control_plane"
        echo ""
        exit 1
    fi
    echo -e "${GREEN}PostgreSQL connection OK${NC}"
else
    if [ -z "$DB_DSN" ]; then
        DB_DSN="./control_plane.db"
    fi
    echo "  SQLite Database: $DB_DSN"
    echo -e "${GREEN}Using SQLite (no setup required)${NC}"
fi
echo "  Kubeconfig: $KUBECONFIG"
echo "  Namespace: $NAMESPACE"
echo "  Idle Timeout: $IDLE_TIMEOUT"
echo "  Check Interval: $CHECK_INTERVAL"
echo ""

# Check if binary exists
if [ ! -f "control-plane/control-plane" ]; then
    echo -e "${YELLOW}Binary not found. Building...${NC}"
    cd control-plane
    go build -o control-plane ./cmd/api
    cd ..
fi

# Check if Kubernetes is accessible
echo -e "${YELLOW}Checking Kubernetes connection...${NC}"
if [ -f "$KUBECONFIG" ]; then
    if kubectl --kubeconfig="$KUBECONFIG" cluster-info > /dev/null 2>&1; then
        echo -e "${GREEN}Kubernetes connection OK${NC}"
    else
        echo -e "${YELLOW}Warning: Kubernetes may not be accessible${NC}"
    fi
else
    echo -e "${YELLOW}Warning: Kubeconfig not found, using in-cluster config${NC}"
fi

echo ""
echo -e "${GREEN}Starting control plane server...${NC}"
echo ""

# Start the control plane
cd control-plane
./control-plane \
    -port "$PORT" \
    -db-type "$DB_TYPE" \
    -db-dsn "$DB_DSN" \
    -kubeconfig "$KUBECONFIG" \
    -namespace "$NAMESPACE" \
    -idle-timeout "$IDLE_TIMEOUT" \
    -check-interval "$CHECK_INTERVAL"

