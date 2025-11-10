# Quick Start Guide - Control Plane

## Prerequisites

1. **Database** (choose one):
   - **SQLite** (default, recommended for testing) - No setup needed!
   - **PostgreSQL** (for production) - Requires setup:
     ```bash
     # Install PostgreSQL (if not installed)
     sudo apt-get install postgresql postgresql-contrib
     
     # Create database
     createdb control_plane
     ```

2. **Kubernetes Cluster** (optional for testing)
   - Local: `minikube` or `kind`
   - Or use `-kubeconfig` flag to point to your cluster

3. **Go 1.21+** (already installed)

## Step 1: Build Control Plane

```bash
cd control-plane
go build -o control-plane ./cmd/api
cd ..
```

## Step 2: Start Control Plane

### Option A: Using SQLite (Default - No Setup Required!) ✅

```bash
chmod +x start_control_plane.sh
./start_control_plane.sh
```

This uses SQLite by default - **no database setup needed!** The database file will be created at `./control_plane.db`.

### Option B: Using PostgreSQL

```bash
DB_TYPE=postgres \
DB_DSN="postgres://postgres:postgres@localhost:5432/control_plane?sslmode=disable" \
./start_control_plane.sh
```

### Option C: Manual start with SQLite

```bash
cd control-plane
./control-plane \
  -port 8080 \
  -db-type sqlite \
  -db-dsn "./control_plane.db" \
  -kubeconfig ~/.kube/config \
  -namespace default
```

### Option D: Manual start with PostgreSQL

```bash
cd control-plane
./control-plane \
  -port 8080 \
  -db-type postgres \
  -db-dsn "postgres://postgres:postgres@localhost:5432/control_plane?sslmode=disable" \
  -kubeconfig ~/.kube/config \
  -namespace default
```

### Option E: Without Kubernetes (for testing API only)

```bash
cd control-plane
./control-plane \
  -port 8080 \
  -db-type sqlite \
  -db-dsn "./control_plane.db" \
  -kubeconfig "" \
  -namespace default
```

**Note**: Without Kubernetes, compute node creation will fail, but you can test the API endpoints.

## Step 3: Run Tests

In a **new terminal**:

```bash
./test_control_plane.sh
```

## Troubleshooting

### Database Connection Issues

```bash
# Check if PostgreSQL is running
sudo systemctl status postgresql

# Check if database exists
psql -l | grep control_plane

# Create database if missing
createdb control_plane
```

### Kubernetes Issues

```bash
# Check if Kubernetes is accessible
kubectl cluster-info

# For local testing, use minikube or kind
minikube start
# or
kind create cluster
```

### Port Already in Use

```bash
# Check what's using port 8080
sudo lsof -i :8080

# Use a different port
./control-plane -port 8081 ...
```

## Quick Test (Without Full Setup)

If you just want to test the API without Kubernetes:

1. **Start control plane** (without Kubernetes):
   ```bash
   cd control-plane
   ./control-plane -port 8080 \
     -db-dsn "postgres://postgres:postgres@localhost:5432/control_plane?sslmode=disable" \
     -kubeconfig "" \
     -namespace default
   ```

2. **Test API endpoints**:
   ```bash
   # Create project
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
   
   # List projects
   curl http://localhost:8080/api/v1/projects | jq
   ```

## Expected Output

When control plane starts successfully, you should see:

```
Starting control plane API server on :8080
```

The server will keep running until you stop it (Ctrl+C).

## Next Steps

Once the control plane is running:

1. **Run the test script** in another terminal
2. **Or test manually** using curl commands
3. **Check logs** for any errors

