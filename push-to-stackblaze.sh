#!/bin/bash

# Quick script to push MariaDB Page Server image to stackblaze repository

set -e

echo "🚀 Pushing MariaDB Page Server image to stackblaze repository..."
echo ""

# Use stackblaze as the repository
REGISTRY_USER=stackblaze ./build-and-push-image.sh

echo ""
echo "✅ Done! Image pushed to: stackblaze/mariadb-pageserver:latest"
echo ""
echo "To use this image in the control plane:"
echo "  export MARIADB_PAGESERVER_IMAGE=stackblaze/mariadb-pageserver:latest"
echo "  ./start_control_plane.sh"

