# Docker Images for Serverless Stack

## Overview

All components are containerized and available in the `stackblaze` Docker repository:

- **Control Plane**: `stackblaze/control-plane:latest`
- **Page Server**: `stackblaze/page-server:latest`
- **Safekeeper**: `stackblaze/safekeeper:latest`
- **MariaDB**: `stackblaze/mariadb-pageserver:latest`

## Building and Pushing Images

### Build All Images

```bash
./build-and-push-all.sh
```

This will:
1. Build binaries for each component (if not already built)
2. Create Docker images
3. Push to `docker.io/stackblaze/`

### Build Individual Images

```bash
# Page Server
cd page-server
docker build -f Dockerfile -t stackblaze/page-server:latest .
docker push stackblaze/page-server:latest

# Safekeeper
cd safekeeper
docker build -f Dockerfile -t stackblaze/safekeeper:latest .
docker push stackblaze/safekeeper:latest

# Control Plane
cd control-plane
docker build -f Dockerfile -t stackblaze/control-plane:latest .
docker push stackblaze/control-plane:latest
```

## Using Docker Images

### Option 1: Docker Compose (Recommended for Local Testing)

Create `docker-compose.yml`:

```yaml
version: '3.8'

services:
  page-server:
    image: stackblaze/page-server:latest
    ports:
      - "8081:8081"
    volumes:
      - page-server-data:/var/lib/page-server
    command: ["-port", "8081", "-data-dir", "/var/lib/page-server"]

  safekeeper:
    image: stackblaze/safekeeper:latest
    ports:
      - "8082:8082"
    volumes:
      - safekeeper-data:/var/lib/safekeeper
    command: ["-port", "8082", "-data-dir", "/var/lib/safekeeper"]

  control-plane:
    image: stackblaze/control-plane:latest
    ports:
      - "8080:8080"
    volumes:
      - control-plane-data:/var/lib/control-plane
      - ~/.kube/config:/root/.kube/config:ro
    environment:
      - MARIADB_PAGESERVER_IMAGE=stackblaze/mariadb-pageserver:latest
    command: ["-port", "8080", "-db-type", "sqlite", "-db-dsn", "/var/lib/control-plane/control_plane.db"]

volumes:
  page-server-data:
  safekeeper-data:
  control-plane-data:
```

Run:
```bash
docker-compose up -d
```

### Option 2: Kubernetes Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: page-server
spec:
  replicas: 1
  selector:
    matchLabels:
      app: page-server
  template:
    metadata:
      labels:
        app: page-server
    spec:
      containers:
      - name: page-server
        image: stackblaze/page-server:latest
        ports:
        - containerPort: 8081
        volumeMounts:
        - name: data
          mountPath: /var/lib/page-server
      volumes:
      - name: data
        emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: page-server
spec:
  selector:
    app: page-server
  ports:
  - port: 8081
    targetPort: 8081
```

### Option 3: Manual Docker Run

```bash
# Start Page Server
docker run -d \
  --name page-server \
  -p 8081:8081 \
  -v page-server-data:/var/lib/page-server \
  stackblaze/page-server:latest \
  -port 8081 -data-dir /var/lib/page-server

# Start Safekeeper
docker run -d \
  --name safekeeper \
  -p 8082:8082 \
  -v safekeeper-data:/var/lib/safekeeper \
  stackblaze/safekeeper:latest \
  -port 8082 -data-dir /var/lib/safekeeper

# Start Control Plane
docker run -d \
  --name control-plane \
  -p 8080:8080 \
  -v control-plane-data:/var/lib/control-plane \
  -v ~/.kube/config:/root/.kube/config:ro \
  -e MARIADB_PAGESERVER_IMAGE=stackblaze/mariadb-pageserver:latest \
  stackblaze/control-plane:latest \
  -port 8080 -db-type sqlite -db-dsn /var/lib/control-plane/control_plane.db
```

## Testing with Docker Images

### Full Integration Test

```bash
./test_full_integration_docker.sh
```

This test:
1. Pulls all images from stackblaze repository
2. Starts containers in a Docker network
3. Creates a project and compute node
4. Verifies all components work together

## Image Details

### Page Server Image

- **Base**: `debian:bookworm-slim`
- **Port**: 8081
- **Health Check**: `GET /api/v1/ping`
- **Data Volume**: `/var/lib/page-server`

### Safekeeper Image

- **Base**: `debian:bookworm-slim`
- **Port**: 8082
- **Health Check**: `GET /api/v1/ping`
- **Data Volume**: `/var/lib/safekeeper`

### Control Plane Image

- **Base**: `debian:bookworm-slim`
- **Port**: 8080
- **Health Check**: `GET /api/v1/projects`
- **Data Volume**: `/var/lib/control-plane`
- **Requires**: Kubernetes config mounted at `/root/.kube/config`

## Networking

### Docker Network

For local testing, all containers should be on the same Docker network:

```bash
docker network create serverless-test
docker run --network serverless-test ...
```

### Kubernetes Services

In Kubernetes, use Service names for inter-pod communication:

- Page Server: `http://page-server:8081`
- Safekeeper: `http://safekeeper:8082`
- Control Plane: `http://control-plane:8080`

### Host Access

For pods to access services on the host:

- Use host IP: `http://<host-ip>:8081`
- Or use `hostNetwork: true` in pod spec (not recommended for production)

## Environment Variables

### Control Plane

- `MARIADB_PAGESERVER_IMAGE`: MariaDB image to use (default: `stackblaze/mariadb-pageserver:latest`)

### Page Server

- `PAGE_SERVER_PORT`: Port to listen on (default: 8081)
- `PAGE_SERVER_DATA_DIR`: Data directory (default: `/var/lib/page-server`)

### Safekeeper

- `SAFEKEEPER_PORT`: Port to listen on (default: 8082)
- `SAFEKEEPER_DATA_DIR`: Data directory (default: `/var/lib/safekeeper`)

## Troubleshooting

### Image Not Found

```bash
# Pull image manually
docker pull stackblaze/page-server:latest

# Or build locally
cd page-server
docker build -f Dockerfile -t stackblaze/page-server:latest .
```

### Container Can't Connect

1. **Check network**: Ensure containers are on the same network
2. **Check ports**: Verify ports are exposed and not conflicting
3. **Check logs**: `docker logs <container-name>`

### Kubernetes Access Issues

1. **Service discovery**: Use Service names, not localhost
2. **Host access**: Use host IP for services running on host
3. **Network policies**: Check if network policies are blocking traffic

## Production Considerations

1. **Use Kubernetes Services**: Deploy as Kubernetes services for production
2. **Persistent Volumes**: Use PersistentVolumeClaims instead of emptyDir
3. **Resource Limits**: Set CPU and memory limits
4. **Health Checks**: Use Kubernetes liveness/readiness probes
5. **TLS/HTTPS**: Enable TLS for all services
6. **Authentication**: Enable API keys and authentication
7. **Monitoring**: Add Prometheus metrics and logging

