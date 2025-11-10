#!/bin/bash

# Build and push custom MariaDB Docker image to registry

set -e

# Configuration
IMAGE_NAME="${IMAGE_NAME:-mariadb-pageserver}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
REGISTRY="${REGISTRY:-docker.io}"  # Change to your registry
REGISTRY_USER="${REGISTRY_USER:-stackblaze}"  # Repository/username (default: stackblaze)

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}🐳 Building and pushing MariaDB Page Server image${NC}"
echo ""

# Step 1: Find built binaries
echo "📦 Step 1: Locating built MariaDB binaries..."

BUILD_DIR=""
MYSQLD_BIN=""
MYSQL_BIN=""
MYSQL_LIB_DIR=""

# Check common locations (check for mariadbd first, then mysqld)
if [ -f "./build-debug/sql/mariadbd" ] || [ -f "./build-debug/sql/mysqld" ]; then
    BUILD_DIR="./build-debug"
    echo "   ✅ Found: ./build-debug"
elif [ -f "./build/sql/mariadbd" ] || [ -f "./build/sql/mysqld" ]; then
    BUILD_DIR="./build"
    echo "   ✅ Found: ./build"
elif [ -f "/usr/local/mysql/bin/mariadbd" ] || [ -f "/usr/local/mysql/bin/mysqld" ]; then
    BUILD_DIR="/usr/local/mysql"
    echo "   ✅ Found: /usr/local/mysql"
elif [ -n "$MYSQLD_PATH" ] && [ -d "$MYSQLD_PATH" ]; then
    BUILD_DIR="$MYSQLD_PATH"
    echo "   ✅ Using: $MYSQLD_PATH"
elif command -v mysqld > /dev/null 2>&1; then
    BUILD_DIR=$(dirname $(dirname $(which mysqld)))
    echo "   ✅ Found: $BUILD_DIR"
else
    echo "   ❌ Error: Could not find built MariaDB binaries"
    echo "   Searched: ./build-debug, ./build, /usr/local/mysql"
    echo "   Please specify MYSQLD_PATH environment variable"
    exit 1
fi

# Find the actual binary (mariadbd or mysqld)
if [ -f "$BUILD_DIR/sql/mariadbd" ]; then
    MYSQLD_BIN="$BUILD_DIR/sql/mariadbd"
elif [ -f "$BUILD_DIR/sql/mysqld" ]; then
    MYSQLD_BIN="$BUILD_DIR/sql/mysqld"
elif [ -f "$BUILD_DIR/bin/mariadbd" ]; then
    MYSQLD_BIN="$BUILD_DIR/bin/mariadbd"
elif [ -f "$BUILD_DIR/bin/mysqld" ]; then
    MYSQLD_BIN="$BUILD_DIR/bin/mysqld"
else
    echo "   ❌ Error: Could not find mysqld/mariadbd binary in $BUILD_DIR"
    exit 1
fi

MYSQL_BIN="$BUILD_DIR/client/mysql"
MYSQL_LIB_DIR="$BUILD_DIR/libmysql"

echo "   ✅ Binary: $MYSQLD_BIN"

# Check for optional files
MYSQL_CLIENT_EXISTS=false
if [ -f "$MYSQL_BIN" ]; then
    MYSQL_CLIENT_EXISTS=true
    echo "   ✅ MySQL client found: $MYSQL_BIN"
fi

LIB_DIR_EXISTS=false
if [ -d "$MYSQL_LIB_DIR" ] && [ "$(ls -A $MYSQL_LIB_DIR 2>/dev/null)" ]; then
    LIB_DIR_EXISTS=true
    echo "   ✅ Libraries found: $MYSQL_LIB_DIR"
fi

# Step 2: Create temporary Dockerfile
echo ""
echo "📝 Step 2: Creating Dockerfile..."

# Create Dockerfile
cat > Dockerfile.tmp <<EOF
FROM debian:bookworm-slim

# Install runtime dependencies
RUN apt-get update && apt-get install -y \\
    libssl3 \\
    libncurses6 \\
    libzstd1 \\
    libbz2-1.0 \\
    liblzma5 \\
    libsnappy1v5 \\
    zlib1g \\
    libcurl4 \\
    ca-certificates \\
    && rm -rf /var/lib/apt/lists/*

# Create mysql user
RUN groupadd -r mysql && useradd -r -g mysql mysql

# Create directories
RUN mkdir -p /var/lib/mysql /usr/local/mysql/bin /usr/local/mysql/lib /etc/mysql

# Copy MariaDB binaries (mariadbd is the actual binary)
COPY ${MYSQLD_BIN} /usr/local/mysql/bin/mariadbd
RUN chmod +x /usr/local/mysql/bin/mariadbd && \
    ln -sf /usr/local/mysql/bin/mariadbd /usr/local/mysql/bin/mysqld
EOF

# Add optional mysql client if it exists
if [ "$MYSQL_CLIENT_EXISTS" = true ]; then
    cat >> Dockerfile.tmp <<EOF

# Copy mysql client
COPY ${MYSQL_BIN} /usr/local/mysql/bin/mysql
RUN chmod +x /usr/local/mysql/bin/mysql
EOF
fi

# Add libraries if they exist
if [ "$LIB_DIR_EXISTS" = true ]; then
    cat >> Dockerfile.tmp <<EOF

# Copy libraries
COPY ${MYSQL_LIB_DIR}/ /usr/local/mysql/lib/
EOF
fi

# Continue with rest of Dockerfile
cat >> Dockerfile.tmp <<EOF

# Copy entrypoint
COPY docker-entrypoint.sh /usr/local/bin/
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# Set environment
ENV PATH="/usr/local/mysql/bin:\$PATH"
ENV MYSQL_ROOT_PASSWORD=root
ENV MYSQL_DATABASE=test
ENV PAGE_SERVER_URL=""
ENV SAFEKEEPER_URL=""

# Expose port
EXPOSE 3306

# Create data directory
RUN mkdir -p /var/lib/mysql && chown -R mysql:mysql /var/lib/mysql

ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["mysqld"]
EOF

echo "   ✅ Dockerfile created"

# Step 3: Build image
echo ""
echo "🔨 Step 3: Building Docker image..."
FULL_IMAGE_NAME="$IMAGE_NAME:$IMAGE_TAG"

if [ -n "$REGISTRY_USER" ]; then
    FULL_IMAGE_NAME="$REGISTRY/$REGISTRY_USER/$IMAGE_NAME:$IMAGE_TAG"
else
    FULL_IMAGE_NAME="$REGISTRY/$IMAGE_NAME:$IMAGE_TAG"
fi

docker build -f Dockerfile.tmp -t "$FULL_IMAGE_NAME" .
echo -e "   ${GREEN}✅ Image built: $FULL_IMAGE_NAME${NC}"

# Step 4: Tag as latest if not already
if [ "$IMAGE_TAG" != "latest" ]; then
    LATEST_TAG="${FULL_IMAGE_NAME%:*}:latest"
    docker tag "$FULL_IMAGE_NAME" "$LATEST_TAG"
    echo "   ✅ Also tagged as: $LATEST_TAG"
fi

# Step 5: Push to registry
echo ""
echo "📤 Step 4: Pushing to registry..."

# Check if logged in
if ! docker info | grep -q "Username"; then
    echo "   ⚠️  Not logged into Docker registry"
    echo "   Run: docker login $REGISTRY"
    read -p "   Do you want to login now? (y/n) " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        docker login "$REGISTRY"
    else
        echo "   Skipping push. You can push later with:"
        echo "   docker push $FULL_IMAGE_NAME"
        exit 0
    fi
fi

docker push "$FULL_IMAGE_NAME"
echo -e "   ${GREEN}✅ Pushed: $FULL_IMAGE_NAME${NC}"

if [ "$IMAGE_TAG" != "latest" ]; then
    docker push "$LATEST_TAG"
    echo -e "   ${GREEN}✅ Pushed: $LATEST_TAG${NC}"
fi

# Cleanup
rm -f Dockerfile.tmp

echo ""
echo -e "${GREEN}✅ Done!${NC}"
echo ""
echo "To use this image in the control plane:"
echo "  export MARIADB_PAGESERVER_IMAGE=$FULL_IMAGE_NAME"
echo "  ./start_control_plane.sh"
echo ""
echo "Or specify in API:"
echo "  {\"config\": {\"image\": \"$FULL_IMAGE_NAME\"}}"

