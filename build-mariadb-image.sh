#!/bin/bash

# Build custom MariaDB Docker image with Page Server patches

set -e

IMAGE_NAME="${IMAGE_NAME:-mariadb-pageserver}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
BUILD_DIR="${BUILD_DIR:-$(pwd)}"

echo "🔨 Building MariaDB image with Page Server patches..."
echo "   Image: $IMAGE_NAME:$IMAGE_TAG"
echo "   Build dir: $BUILD_DIR"
echo ""

# Check if we're in the right directory
if [ ! -f "storage/innobase/include/page_server.h" ]; then
    echo "❌ Error: page_server.h not found. Are you in the MariaDB source root?"
    exit 1
fi

# Build the image
docker build \
    -f Dockerfile.mariadb-pageserver \
    -t "$IMAGE_NAME:$IMAGE_TAG" \
    .

echo ""
echo "✅ Image built successfully: $IMAGE_NAME:$IMAGE_TAG"
echo ""
echo "To use this image in the control plane:"
echo "  export MARIADB_IMAGE=$IMAGE_NAME:$IMAGE_TAG"
echo "  ./start_control_plane.sh"
echo ""
echo "Or update the default in control-plane/internal/compute/manager.go:"
echo "  config.Image = \"$IMAGE_NAME:$IMAGE_TAG\""


