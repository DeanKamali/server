#!/bin/bash
# Quick script to use k3s with control plane

# Copy k3s config
sudo cp /etc/rancher/k3s/k3s.yaml ~/.kube/config-k3s
sudo chown $USER:$USER ~/.kube/config-k3s

# Test k3s connection
echo "Testing k3s connection..."
KUBECONFIG=~/.kube/config-k3s kubectl cluster-info
KUBECONFIG=~/.kube/config-k3s kubectl get nodes

# Start control plane with k3s
echo ""
echo "Starting control plane with k3s..."
KUBECONFIG=~/.kube/config-k3s ./start_control_plane.sh
