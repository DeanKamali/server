#!/bin/bash
# Quick setup script for control plane database

set -e

echo "Setting up control plane database..."

# Check if PostgreSQL is running
if ! systemctl is-active --quiet postgresql 2>/dev/null; then
    echo "PostgreSQL is not running. Starting it..."
    sudo systemctl start postgresql
fi

# Create database
echo "Creating database 'control_plane'..."
sudo -u postgres psql -c "CREATE DATABASE control_plane;" 2>/dev/null || echo "Database may already exist"

echo "Database setup complete!"
echo ""
echo "You can now start the control plane with:"
echo "  ./start_control_plane.sh"
