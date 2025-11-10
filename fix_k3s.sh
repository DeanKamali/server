#!/bin/bash

# Fix k3s issues and configure for control plane

set -e

echo "🔧 Fixing k3s for control plane..."

# 1. Check if k3s is running
if ! sudo systemctl is-active --quiet k3s; then
    echo "⚠️  k3s is not running. Starting..."
    sudo systemctl start k3s
    sleep 5
fi

# 2. Check for port conflicts
echo "Checking for port conflicts..."
if sudo lsof -i :10257 > /dev/null 2>&1; then
    echo "⚠️  Port 10257 is in use. This may cause k3s issues."
    echo "   Consider stopping the conflicting service."
fi

# 3. Wait for k3s to be ready
echo "Waiting for k3s to be ready..."
for i in {1..30}; do
    if KUBECONFIG=/etc/rancher/k3s/k3s.yaml kubectl cluster-info > /dev/null 2>&1; then
        echo "✅ k3s is ready!"
        break
    fi
    echo "   Attempt $i/30..."
    sleep 1
done

# 4. Test connection
echo ""
echo "Testing k3s connection..."
KUBECONFIG=/etc/rancher/k3s/k3s.yaml kubectl cluster-info
KUBECONFIG=/etc/rancher/k3s/k3s.yaml kubectl get nodes

# 5. Copy kubeconfig for user
echo ""
echo "Setting up kubeconfig..."
sudo cp /etc/rancher/k3s/k3s.yaml ~/.kube/config-k3s
sudo chown $USER:$USER ~/.kube/config-k3s

echo ""
echo "✅ k3s is configured!"
echo ""
echo "To use k3s with control plane:"
echo "  KUBECONFIG=~/.kube/config-k3s ./start_control_plane.sh"
echo ""
echo "Or update your default kubeconfig:"
echo "  sudo cp /etc/rancher/k3s/k3s.yaml ~/.kube/config"
echo "  sudo chown \$USER:\$USER ~/.kube/config"



