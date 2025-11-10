#!/bin/bash

# Fix kubeconfig to use local k3s instead of remote cluster

set -e

K3S_CONFIG="/etc/rancher/k3s/k3s.yaml"
USER_CONFIG="$HOME/.kube/config"
BACKUP_CONFIG="$HOME/.kube/config.backup.$(date +%s)"

echo "Fixing kubeconfig for k3s..."

# Check if k3s config exists
if [ ! -f "$K3S_CONFIG" ]; then
    echo "Error: k3s config not found at $K3S_CONFIG"
    echo "Is k3s installed and running?"
    exit 1
fi

# Backup current config
if [ -f "$USER_CONFIG" ]; then
    echo "Backing up current config to $BACKUP_CONFIG"
    cp "$USER_CONFIG" "$BACKUP_CONFIG"
fi

# Copy k3s config
echo "Copying k3s config to $USER_CONFIG"
sudo cp "$K3S_CONFIG" "$USER_CONFIG"
sudo chown $USER:$USER "$USER_CONFIG"

# Fix server URL if it's localhost
echo "Fixing server URL..."
sed -i 's|https://127.0.0.1:6443|https://localhost:6443|g' "$USER_CONFIG" || true

echo ""
echo "✅ kubeconfig updated!"
echo ""
echo "Testing connection..."
kubectl cluster-info
kubectl get nodes

echo ""
echo "You can now restart the control plane to use k3s"



