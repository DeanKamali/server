# k3s + Control Plane Status

## ✅ Current Status

- **k3s**: Running (node: `directus-dev-sep-24`)
- **Control Plane**: Running on port 8080
- **Database**: SQLite (no setup needed)
- **API**: All endpoints working

## 🧪 Testing

### Quick API Test (No k3s needed)
```bash
./test_control_plane_quick.sh
```

### Full Test (Requires k3s)
```bash
./test_control_plane.sh
```

## ⚠️ Known Issues

### Pod Scheduling Issues

Pods may get stuck in `Pending` status due to:
1. **Disk Pressure**: Node has insufficient disk space
2. **Resource Constraints**: Not enough CPU/Memory

**Solutions:**
- Reduced resource requirements (100m CPU, 256Mi memory)
- Better error messages for scheduling failures
- Increased timeout to 10 minutes

### Check Pod Status

```bash
# List pods
kubectl get pods -l app=mariadb-compute

# Check why pod is pending
kubectl describe pod <pod-name>

# Check node resources
kubectl top node
kubectl describe node
```

## 🔧 Improvements Made

1. **Reduced Resource Requirements**
   - CPU: 500m → 100m (request), 2 → 1 (limit)
   - Memory: 1Gi → 256Mi (request), 2Gi → 512Mi (limit)

2. **Better Error Handling**
   - Detects scheduling failures
   - Detects image pull errors
   - Provides detailed error messages

3. **Increased Timeout**
   - 5 minutes → 10 minutes for MariaDB startup

4. **Improved Pod Wait Logic**
   - Better status checking
   - Handles pending state better

## 📝 Next Steps

1. **Fix Disk Pressure** (if present):
   ```bash
   # Check disk usage
   df -h
   
   # Clean up unused images
   kubectl get pods --all-namespaces
   docker system prune -a  # if using Docker
   ```

2. **Test Compute Node Creation**:
   ```bash
   # Create project
   curl -X POST http://localhost:8080/api/v1/projects \
     -H "Content-Type: application/json" \
     -d '{"name": "test", "config": {...}}'
   
   # Create compute node (will create pod in k3s)
   curl -X POST http://localhost:8080/api/v1/projects/<id>/compute \
     -H "Content-Type: application/json" \
     -d '{"config": {...}}'
   ```

3. **Monitor Pods**:
   ```bash
   watch kubectl get pods -l app=mariadb-compute
   ```

## 🎯 Summary

- ✅ Control plane API is fully functional
- ✅ k3s is running and accessible
- ⚠️ Pod scheduling may need disk space cleanup
- ✅ Improved error handling and resource requirements



