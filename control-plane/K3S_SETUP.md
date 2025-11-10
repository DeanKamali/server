# k3s Setup for Control Plane

## k3s Integration

k3s is a lightweight Kubernetes distribution perfect for local development. The control plane can use k3s to manage MariaDB compute nodes.

## Quick Setup

### 1. Verify k3s is Running

```bash
kubectl cluster-info
kubectl get nodes
```

### 2. Start Control Plane with k3s

The control plane should automatically detect k3s if your `KUBECONFIG` points to it:

```bash
# Default k3s kubeconfig location
export KUBECONFIG=~/.kube/config

# Or if k3s uses a different location
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml

# Start control plane
./start_control_plane.sh
```

### 3. Test Compute Node Creation

Once the control plane is running, you can test creating a compute node:

```bash
# Create a project first
curl -X POST http://localhost:8080/api/v1/projects \
  -H "Content-Type: application/json" \
  -d '{
    "name": "test-project",
    "config": {
      "page_server_url": "http://localhost:8081",
      "safekeeper_url": "http://localhost:8082",
      "idle_timeout": 300,
      "max_connections": 100
    }
  }' | jq

# Get project ID from response, then create compute node
PROJECT_ID="<project-id>"
curl -X POST "http://localhost:8080/api/v1/projects/$PROJECT_ID/compute" \
  -H "Content-Type: application/json" \
  -d '{
    "config": {
      "image": "mariadb:latest",
      "resources": {
        "cpu": "500m",
        "memory": "1Gi"
      }
    }
  }' | jq
```

### 4. Verify Pod Creation

```bash
# Check if MariaDB pod was created
kubectl get pods -l app=mariadb-compute

# Check pod details
kubectl describe pod <pod-name>
```

## k3s Configuration

### Default k3s kubeconfig Location

k3s typically stores its kubeconfig at:
- `/etc/rancher/k3s/k3s.yaml` (when installed as root)
- `~/.kube/config` (if you've copied it there)

### Copy k3s kubeconfig (if needed)

```bash
# If k3s kubeconfig is at /etc/rancher/k3s/k3s.yaml
sudo cp /etc/rancher/k3s/k3s.yaml ~/.kube/config
sudo chown $USER:$USER ~/.kube/config
```

### Set KUBECONFIG Environment Variable

```bash
# For current session
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml

# Or add to ~/.bashrc for persistence
echo 'export KUBECONFIG=/etc/rancher/k3s/k3s.yaml' >> ~/.bashrc
```

## Troubleshooting

### Issue: "Kubernetes may not be accessible"

**Solution**: Check kubeconfig path:

```bash
# Check if k3s is running
sudo systemctl status k3s

# Check kubeconfig location
ls -la /etc/rancher/k3s/k3s.yaml

# Copy to user location
sudo cp /etc/rancher/k3s/k3s.yaml ~/.kube/config
sudo chown $USER:$USER ~/.kube/config
```

### Issue: "Failed to get in-cluster config"

**Solution**: Use kubeconfig flag:

```bash
cd control-plane
./control-plane \
  -port 8080 \
  -db-type sqlite \
  -kubeconfig /etc/rancher/k3s/k3s.yaml \
  -namespace default
```

### Issue: Pod creation fails

**Solution**: Check k3s permissions and namespace:

```bash
# Check if namespace exists
kubectl get namespace default

# Create namespace if needed
kubectl create namespace default

# Check RBAC permissions
kubectl auth can-i create pods --namespace default
```

## Testing with k3s

### Full Test Flow

1. **Start control plane** (already running ✅)
2. **Run test script** in another terminal:
   ```bash
   ./test_control_plane.sh
   ```

3. **Monitor k3s pods**:
   ```bash
   watch kubectl get pods -l app=mariadb-compute
   ```

### Manual Testing

```bash
# 1. Create project
PROJECT_RESPONSE=$(curl -s -X POST http://localhost:8080/api/v1/projects \
  -H "Content-Type: application/json" \
  -d '{
    "name": "k3s-test",
    "config": {
      "page_server_url": "http://localhost:8081",
      "safekeeper_url": "http://localhost:8082",
      "idle_timeout": 300
    }
  }')

PROJECT_ID=$(echo $PROJECT_RESPONSE | jq -r '.id')
echo "Project ID: $PROJECT_ID"

# 2. Create compute node (will create pod in k3s)
COMPUTE_RESPONSE=$(curl -s -X POST "http://localhost:8080/api/v1/projects/$PROJECT_ID/compute" \
  -H "Content-Type: application/json" \
  -d '{
    "config": {
      "image": "mariadb:latest",
      "resources": {
        "cpu": "500m",
        "memory": "1Gi"
      }
    }
  }')

COMPUTE_ID=$(echo $COMPUTE_RESPONSE | jq -r '.id')
echo "Compute ID: $COMPUTE_ID"

# 3. Check pod in k3s
kubectl get pods -l compute-id=$COMPUTE_ID

# 4. Suspend compute node
curl -X POST "http://localhost:8080/api/v1/compute/$COMPUTE_ID/suspend"

# 5. Verify pod is deleted
kubectl get pods -l compute-id=$COMPUTE_ID

# 6. Resume compute node
curl -X POST "http://localhost:8080/api/v1/compute/$COMPUTE_ID/resume"

# 7. Verify pod is recreated
kubectl get pods -l compute-id=$COMPUTE_ID
```

## k3s vs Full Kubernetes

| Feature | k3s | Full Kubernetes |
|---------|-----|------------------|
| **Size** | ~50MB | ~1GB+ |
| **Startup** | < 30s | 1-5 min |
| **Resources** | Minimal | Higher |
| **Compatibility** | ✅ Full K8s API | ✅ Full |
| **Production** | ⚠️ Light workloads | ✅ Enterprise |

k3s is perfect for local development and testing!



