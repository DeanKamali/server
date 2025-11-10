#!/bin/bash

# Build script for Page Server

set -e

echo "Building Page Server..."

# Clean previous build
if [ -f page-server ]; then
    rm page-server
fi

# Build from cmd/pageserver
go build -o page-server ./cmd/pageserver

if [ $? -eq 0 ]; then
    echo "✓ Page Server built successfully"
    echo "  Run with: ./page-server -port 8080"
else
    echo "✗ Build failed"
    exit 1
fi
