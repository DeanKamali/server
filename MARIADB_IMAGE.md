# Custom MariaDB Docker Image for Page Server

## Overview

The control plane needs a **custom MariaDB Docker image** that includes the Page Server patches. The standard `mariadb:latest` image won't work because it doesn't have the InnoDB modifications.

## Current Status

- ✅ **Patches exist** in the source code (`storage/innobase/`)
- ❌ **No Docker image built** yet
- ⚠️ **Control plane uses** `mariadb-pageserver:latest` (needs to be built)

## Building the Custom Image

### Quick Build

```bash
./build-mariadb-image.sh
```

This will:
1. Build MariaDB from source with Page Server patches
2. Create a Docker image: `mariadb-pageserver:latest`
3. Include the patched InnoDB storage engine

### Custom Image Name/Tag

```bash
IMAGE_NAME=my-mariadb IMAGE_TAG=v1.0 ./build-mariadb-image.sh
```

### Build Requirements

- Docker installed
- ~10GB disk space (for build)
- ~30 minutes build time (first time)
- MariaDB source code with patches

## Using the Custom Image

### Option 1: Environment Variable

```bash
export MARIADB_PAGESERVER_IMAGE=mariadb-pageserver:latest
./start_control_plane.sh
```

### Option 2: Update Default in Code

Edit `control-plane/internal/compute/manager.go`:

```go
if config.Image == "" {
    config.Image = "mariadb-pageserver:latest" // Your custom image
}
```

### Option 3: Specify in API Request

When creating a compute node:

```json
{
  "config": {
    "image": "mariadb-pageserver:latest",
    "page_server_url": "http://localhost:8081",
    "safekeeper_url": "http://localhost:8082"
  }
}
```

## What's Different in the Custom Image?

The custom image includes:

1. **Page Server Client** (`storage/innobase/page_server/`)
   - Redirects page reads to Page Server
   - Handles page versioning

2. **WAL Streaming** (`storage/innobase/log/log0log.cc`)
   - Streams redo log to Safekeeper
   - Async WAL streaming

3. **InnoDB Patches** (`storage/innobase/buf/buf0rea.cc`)
   - Buffer pool integration
   - Page I/O redirection

4. **Configuration Support**
   - `innodb_page_server_enabled`
   - `innodb_page_server_address`
   - `innodb_page_server_port`

## Testing the Image

### Build and Test Locally

```bash
# Build image
./build-mariadb-image.sh

# Run container
docker run -d \
  --name mariadb-test \
  -e PAGE_SERVER_URL=http://localhost:8081 \
  -e SAFEKEEPER_URL=http://localhost:8082 \
  -e MYSQL_ROOT_PASSWORD=root \
  -p 3306:3306 \
  mariadb-pageserver:latest

# Test connection
mysql -h 127.0.0.1 -u root -proot -e "SELECT @@version;"

# Check Page Server config
mysql -h 127.0.0.1 -u root -proot -e "SHOW VARIABLES LIKE 'innodb_page_server%';"
```

### Verify Patches Are Active

```sql
-- Should show Page Server variables
SHOW VARIABLES LIKE 'innodb_page_server%';

-- Should show Page Server status
SHOW STATUS LIKE 'Page_server%';
```

## Troubleshooting

### Image Not Found

**Error:** `Error: pull access denied for mariadb-pageserver`

**Solution:** Build the image first:
```bash
./build-mariadb-image.sh
```

### Build Fails

**Error:** `CMake error` or `make error`

**Solution:** Check build dependencies:
```bash
# Install build tools
sudo apt-get update
sudo apt-get install -y build-essential cmake git
```

### MariaDB Won't Start

**Error:** `mysqld: unknown option '--innodb-page-server-enabled'`

**Solution:** The patches may not be compiled. Rebuild:
```bash
docker rmi mariadb-pageserver:latest
./build-mariadb-image.sh
```

## Alternative: Use Pre-built Image

If you have a pre-built image in a registry:

```bash
# Pull from registry
docker pull your-registry/mariadb-pageserver:latest

# Tag locally
docker tag your-registry/mariadb-pageserver:latest mariadb-pageserver:latest
```

## Next Steps

1. **Build the image:**
   ```bash
   ./build-mariadb-image.sh
   ```

2. **Test locally:**
   ```bash
   docker run -d --name mariadb-test \
     -e PAGE_SERVER_URL=http://localhost:8081 \
     -e SAFEKEEPER_URL=http://localhost:8082 \
     mariadb-pageserver:latest
   ```

3. **Update control plane to use it:**
   ```bash
   export MARIADB_PAGESERVER_IMAGE=mariadb-pageserver:latest
   ./start_control_plane.sh
   ```

4. **Create compute node:**
   ```bash
   curl -X POST http://localhost:8080/api/v1/projects/<id>/compute \
     -H "Content-Type: application/json" \
     -d '{"config": {"image": "mariadb-pageserver:latest"}}'
   ```


