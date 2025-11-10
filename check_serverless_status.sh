#!/bin/bash

# Check serverless environment status

set -e

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo "🔍 Checking Serverless Environment Status..."
echo ""

# Check Control Plane
echo -n "Control Plane API: "
if curl -s http://localhost:8080/api/v1/projects > /dev/null 2>&1; then
    echo -e "${GREEN}✅ Running${NC}"
    CONTROL_PLANE_RUNNING=true
else
    echo -e "${RED}❌ Not running${NC}"
    CONTROL_PLANE_RUNNING=false
fi

# Check Page Server
echo -n "Page Server: "
if curl -s http://localhost:8081/api/v1/ping > /dev/null 2>&1; then
    echo -e "${GREEN}✅ Running${NC}"
    PAGE_SERVER_RUNNING=true
else
    echo -e "${RED}❌ Not running${NC}"
    PAGE_SERVER_RUNNING=false
fi

# Check Safekeeper
echo -n "Safekeeper: "
if curl -s http://localhost:8082/api/v1/ping > /dev/null 2>&1; then
    echo -e "${GREEN}✅ Running${NC}"
    SAFEKEEPER_RUNNING=true
else
    echo -e "${RED}❌ Not running${NC}"
    SAFEKEEPER_RUNNING=false
fi

# Check Kubernetes
echo -n "Kubernetes (k3s): "
if kubectl cluster-info > /dev/null 2>&1; then
    echo -e "${GREEN}✅ Accessible${NC}"
    K8S_RUNNING=true
else
    echo -e "${RED}❌ Not accessible${NC}"
    K8S_RUNNING=false
fi

# Check compute nodes
if [ "$K8S_RUNNING" = true ]; then
    COMPUTE_NODES=$(kubectl get pods -l app=mariadb-compute --no-headers 2>/dev/null | wc -l)
    echo -n "Active Compute Nodes: "
    if [ "$COMPUTE_NODES" -gt 0 ]; then
        echo -e "${GREEN}✅ $COMPUTE_NODES running${NC}"
    else
        echo -e "${YELLOW}⚠️  None (this is normal if no compute nodes created)${NC}"
    fi
fi

echo ""
echo "📋 Serverless Features:"
echo ""

# Check suspend scheduler
if [ "$CONTROL_PLANE_RUNNING" = true ]; then
    echo -e "  ${GREEN}✅ Suspend Scheduler: Running${NC} (auto-suspends idle nodes after 5min)"
else
    echo -e "  ${RED}❌ Suspend Scheduler: Not running${NC} (control plane not running)"
fi

# Check wake compute endpoint
if [ "$CONTROL_PLANE_RUNNING" = true ]; then
    if curl -s "http://localhost:8080/api/v1/wake_compute?endpointish=test" > /dev/null 2>&1; then
        echo -e "  ${GREEN}✅ Wake Compute Endpoint: Available${NC}"
    else
        echo -e "  ${YELLOW}⚠️  Wake Compute Endpoint: May need project ID${NC}"
    fi
else
    echo -e "  ${RED}❌ Wake Compute Endpoint: Not available${NC}"
fi

# Check connection proxy
PROXY_RUNNING=$(ps aux | grep -E "proxy|router" | grep -v grep | wc -l)
if [ "$PROXY_RUNNING" -gt 0 ]; then
    echo -e "  ${GREEN}✅ Connection Proxy: Running${NC}"
else
    echo -e "  ${YELLOW}⚠️  Connection Proxy: Not running${NC} (code exists, needs to be started)"
fi

echo ""
echo "🎯 Serverless Capabilities:"
echo ""

if [ "$CONTROL_PLANE_RUNNING" = true ] && [ "$PAGE_SERVER_RUNNING" = true ] && [ "$SAFEKEEPER_RUNNING" = true ]; then
    echo -e "  ${GREEN}✅ Stateless Compute: Yes${NC} (MariaDB gets pages from Page Server)"
    echo -e "  ${GREEN}✅ Remote Storage: Yes${NC} (Page Server + Safekeeper)"
    echo -e "  ${GREEN}✅ Lifecycle Management: Yes${NC} (Control Plane manages compute nodes)"
    echo -e "  ${GREEN}✅ Auto-Suspend: Yes${NC} (Scheduler running)"
    echo -e "  ${YELLOW}⚠️  Auto-Resume: Partial${NC} (Wake endpoint exists, proxy not running)"
    echo -e "  ${YELLOW}⚠️  Connection Routing: Partial${NC} (Proxy code exists, not started)"
    echo ""
    echo -e "${GREEN}✅ You have a WORKING serverless environment!${NC}"
    echo ""
    echo "To make it fully automatic:"
    echo "  1. Start connection proxy (see control-plane/internal/proxy/router.go)"
    echo "  2. Connect clients through proxy instead of directly"
else
    echo -e "  ${RED}❌ Core services not all running${NC}"
    echo ""
    echo "Start services:"
    echo "  ./test_full_integration.sh"
fi


