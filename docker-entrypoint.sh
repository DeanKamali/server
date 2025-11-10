#!/bin/bash
set -e

# Entrypoint script for MariaDB with Page Server support

# Configure Page Server if environment variables are set
if [ -n "$PAGE_SERVER_URL" ]; then
    echo "Configuring Page Server: $PAGE_SERVER_URL"
    # Set system variable for Page Server
    PAGE_SERVER_ENABLED=1
    PAGE_SERVER_ADDRESS=$(echo $PAGE_SERVER_URL | sed 's|http://||' | sed 's|https://||' | cut -d: -f1)
    PAGE_SERVER_PORT=$(echo $PAGE_SERVER_URL | sed 's|http://||' | sed 's|https://||' | cut -d: -f2 | cut -d/ -f1)
    
    # Create my.cnf with Page Server configuration
    cat >> /etc/mysql/my.cnf <<EOF

# Page Server Configuration
[mysqld]
innodb_page_server_enabled=1
innodb_page_server_address=$PAGE_SERVER_ADDRESS
innodb_page_server_port=${PAGE_SERVER_PORT:-8081}
EOF
fi

# Configure Safekeeper if environment variable is set
if [ -n "$SAFEKEEPER_URL" ]; then
    echo "Configuring Safekeeper: $SAFEKEEPER_URL"
    SAFEKEEPER_ADDRESS=$(echo $SAFEKEEPER_URL | sed 's|http://||' | sed 's|https://||' | cut -d: -f1)
    SAFEKEEPER_PORT=$(echo $SAFEKEEPER_URL | sed 's|http://||' | sed 's|https://||' | cut -d: -f2 | cut -d/ -f1)
    
    cat >> /etc/mysql/my.cnf <<EOF

# Safekeeper Configuration
innodb_safekeeper_address=$SAFEKEEPER_ADDRESS
innodb_safekeeper_port=${SAFEKEEPER_PORT:-8082}
EOF
fi

# Initialize database if needed
if [ ! -d "/var/lib/mysql/mysql" ]; then
    echo "Initializing MariaDB database..."
    mysqld --initialize-insecure --user=mysql --datadir=/var/lib/mysql
fi

# Start MariaDB
exec mysqld --user=mysql --datadir=/var/lib/mysql

