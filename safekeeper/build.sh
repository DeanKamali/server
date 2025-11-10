#!/bin/bash

# Build script for Safekeeper

set -e

echo "Building Safekeeper..."

# Clean previous build
if [ -f safekeeper ]; then
    rm safekeeper
fi

# Build from cmd
go build -o safekeeper ./cmd

if [ $? -eq 0 ]; then
    echo "✓ Safekeeper built successfully"
    echo "  Run with: ./safekeeper -port 8090 -replica-id safekeeper-1"
else
    echo "✗ Build failed"
    exit 1
fi



