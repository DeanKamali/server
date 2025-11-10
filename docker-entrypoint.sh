#!/bin/bash
set -e

# Entrypoint script for MariaDB with Page Server support

# Create my.cnf directory if it doesn't exist
mkdir -p /etc/mysql

# Create socket directory
mkdir -p /var/run/mysqld
chown mysql:mysql /var/run/mysqld

# Initialize database if needed (WITHOUT Page Server config to avoid crashes)
if [ ! -d "/var/lib/mysql/mysql" ]; then
    echo "Initializing MariaDB database..."
    
    # Ensure data directory exists and has correct permissions
    mkdir -p /var/lib/mysql
    chown -R mysql:mysql /var/lib/mysql
    
    # Create minimal config for initialization (no Page Server)
    cat > /etc/mysql/my.cnf.init <<'EOF'
[mysqld]
user = mysql
datadir = /var/lib/mysql
socket = /var/run/mysqld/mysqld.sock
pid-file = /var/run/mysqld/mysqld.pid
skip-networking
innodb_page_server_enabled=0
innodb_safekeeper_enabled=0
EOF
    
    # Try to find mariadb-install-db script
    MARIADB_INSTALL_DB=""
    for path in mariadb-install-db /usr/local/bin/mariadb-install-db /usr/local/scripts/mariadb-install-db /usr/local/mysql/scripts/mariadb-install-db /usr/bin/mariadb-install-db; do
        if command -v "$path" > /dev/null 2>&1 || [ -f "$path" ]; then
            MARIADB_INSTALL_DB="$path"
            break
        fi
    done
    
    if [ -n "$MARIADB_INSTALL_DB" ]; then
        # Use mariadb-install-db (traditional method)
        echo "Using mariadb-install-db for initialization..."
        "$MARIADB_INSTALL_DB" \
            --defaults-file=/etc/mysql/my.cnf.init \
            --datadir=/var/lib/mysql \
            --basedir=/usr/local/mysql \
            --user=mysql \
            --skip-test-db \
            --force \
            --auth-root-authentication-method=socket 2>&1 && {
            echo "Database initialized successfully with mariadb-install-db"
            MARIADB_INSTALL_DB_SUCCESS=true
        } || {
            echo "WARNING: mariadb-install-db failed, will try bootstrap method"
            MARIADB_INSTALL_DB=""
        }
    fi
    
    if [ -z "$MARIADB_INSTALL_DB" ]; then
        # If mariadb-install-db not found, mysqld will auto-initialize on first start
        # We'll handle this by starting mysqld once without Page Server
        echo "Note: Database will be auto-initialized on first mysqld start"
        echo "Database directory prepared successfully"
    else
        echo "Database initialized successfully"
    fi
fi

# If database wasn't initialized and we have Page Server configured,
# we need to initialize first without Page Server using bootstrap mode
# (This should only run if mariadb-install-db didn't work above)
if [ ! -d "/var/lib/mysql/mysql" ] && [ -n "$PAGE_SERVER_URL" ] && [ "$MARIADB_INSTALL_DB_SUCCESS" != "true" ]; then
    echo "Initializing database without Page Server using bootstrap mode..."
    
    # Use mysqld --bootstrap to create mysql system database
    # This requires SQL commands piped to stdin
    # For now, we'll create a minimal bootstrap that just creates the mysql database
    # The actual system tables will be created when mysqld starts normally
    echo "create database if not exists mysql;" | mysqld \
        --defaults-file=/etc/mysql/my.cnf.init \
        --user=mysql \
        --datadir=/var/lib/mysql \
        --bootstrap \
        --skip-networking 2>&1 || {
        echo "Bootstrap with minimal SQL failed, trying normal start..."
        # Fallback: start mysqld normally and let it create mysql database
        mysqld --defaults-file=/etc/mysql/my.cnf.init \
            --user=mysql \
            --datadir=/var/lib/mysql \
            --skip-networking 2>&1 > /tmp/mysql-init.log &
        INIT_PID=$!
        
        # Wait up to 60 seconds for mysql database to be created
        for i in {1..60}; do
            if [ -d "/var/lib/mysql/mysql" ]; then
                echo "MySQL system database created"
                # Give it a moment to finish initialization
                sleep 3
                break
            fi
            sleep 1
        done
        
        # Stop the initialization mysqld
        kill $INIT_PID 2>/dev/null || true
        wait $INIT_PID 2>/dev/null || true
    }
    
    if [ ! -d "/var/lib/mysql/mysql" ]; then
        echo "ERROR: Failed to create mysql system database"
        exit 1
    fi
    
    echo "Database initialization completed"
fi

# Now create full config with Page Server support
cat > /etc/mysql/my.cnf <<'EOF'
[mysqld]
user = mysql
datadir = /var/lib/mysql
socket = /var/run/mysqld/mysqld.sock
pid-file = /var/run/mysqld/mysqld.pid
bind-address = 0.0.0.0
port = 3306
EOF

# Note: Root password is set via MYSQL_ROOT_PASSWORD environment variable
# MariaDB will use it during initialization or we can set it after first start

# Configure Page Server if environment variables are set
if [ -n "$PAGE_SERVER_URL" ]; then
    echo "Configuring Page Server: $PAGE_SERVER_URL"
    PAGE_SERVER_ADDRESS=$(echo $PAGE_SERVER_URL | sed 's|http://||' | sed 's|https://||' | cut -d: -f1)
    PAGE_SERVER_PORT=$(echo $PAGE_SERVER_URL | sed 's|http://||' | sed 's|https://||' | cut -d: -f2 | cut -d/ -f1)
    
    # Append Page Server configuration
    cat >> /etc/mysql/my.cnf <<EOF

# Page Server Configuration
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
    
    # Append Safekeeper configuration
    cat >> /etc/mysql/my.cnf <<EOF

# Safekeeper Configuration
innodb_safekeeper_enabled=1
innodb_safekeeper_address=$SAFEKEEPER_ADDRESS
innodb_safekeeper_port=${SAFEKEEPER_PORT:-8082}
EOF
fi

# Ensure socket directory exists
mkdir -p /var/run/mysqld
chown mysql:mysql /var/run/mysqld

# If this is first start, we need to set up remote root access
if [ ! -f "/var/lib/mysql/.remote_root_configured" ]; then
    echo "Configuring remote root access..."
    # Start MariaDB temporarily in background
    mysqld --user=mysql --datadir=/var/lib/mysql &
    MYSQLD_PID=$!
    
    # Wait for MariaDB to be ready
    for i in {1..30}; do
        if mysqladmin ping --socket=/var/run/mysqld/mysqld.sock 2>/dev/null; then
            echo "MariaDB is ready"
            break
        fi
        sleep 1
    done
    
    # Create root user for remote access
    mysql --socket=/var/run/mysqld/mysqld.sock -u root <<EOF
GRANT ALL PRIVILEGES ON *.* TO 'root'@'%' IDENTIFIED BY '$MYSQL_ROOT_PASSWORD' WITH GRANT OPTION;
FLUSH PRIVILEGES;
EOF
    
    # Create database for project if PROJECT_ID is set
    if [ -n "$PROJECT_ID" ]; then
        echo "Creating database for project: $PROJECT_ID"
        mysql --socket=/var/run/mysqld/mysqld.sock -u root <<EOF
CREATE DATABASE IF NOT EXISTS \`$PROJECT_ID\`;
EOF
        echo "Database '$PROJECT_ID' created successfully"
    fi
    
    # Create database from MYSQL_DATABASE if set (for compatibility)
    if [ -n "$MYSQL_DATABASE" ] && [ "$MYSQL_DATABASE" != "$PROJECT_ID" ]; then
        echo "Creating database: $MYSQL_DATABASE"
        mysql --socket=/var/run/mysqld/mysqld.sock -u root <<EOF
CREATE DATABASE IF NOT EXISTS \`$MYSQL_DATABASE\`;
EOF
        echo "Database '$MYSQL_DATABASE' created successfully"
    fi
    
    # Mark as configured
    touch /var/lib/mysql/.remote_root_configured
    
    # Stop temporary MariaDB
    kill $MYSQLD_PID
    wait $MYSQLD_PID 2>/dev/null || true
    
    echo "Remote root access configured"
fi

# Start MariaDB
echo "Starting MariaDB..."
exec mysqld --user=mysql --datadir=/var/lib/mysql


