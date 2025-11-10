#!/bin/bash

# Start Control Plane with k3s

set -e

echo "🚀 Starting Control Plane with k3s..."

# Check if k3s kubeconfig exists
if [ ! -f "/etc/rancher/k3s/k3s.yaml" ]; then
    echo "❌ k3s kubeconfig not found at /etc/rancher/k3s/k3s.yaml"
    echo "   Is k3s installed?"
    exit 1
fi

# Copy k3s config if needed
if [ ! -f "$HOME/.kube/config-k3s" ]; then
    echo "📋 Copying k3s kubeconfig..."
    sudo cp /etc/rancher/k3s/k3s.yaml ~/.kube/config-k3s
    sudo chown $USER:$USER ~/.kube/config-k3s
fi

# Test k3s connection
echo "🔍 Testing k3s connection..."
if ! KUBECONFIG=~/.kube/config-k3s kubectl cluster-info > /dev/null 2>&1; then
    echo "⚠️  k3s may not be fully ready. Continuing anyway..."
else
    echo "✅ k3s is accessible"
    KUBECONFIG=~/.kube/config-k3s kubectl get nodes
fi

echo ""
echo "Starting control plane with k3s..."
echo ""

# Start control plane with k3s kubeconfig
KUBECONFIG=~/.kube/config-k3s \
DB_TYPE=sqlite \
./start_control_plane.sh



