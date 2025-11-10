# Dockerfile for Control Plane
FROM debian:bookworm-slim

# Install runtime dependencies
RUN apt-get update && apt-get install -y \
    ca-certificates \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Install kubectl
RUN curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" \
    && chmod +x kubectl \
    && mv kubectl /usr/local/bin/

# Copy binary
COPY control-plane /usr/local/bin/control-plane
RUN chmod +x /usr/local/bin/control-plane

# Create data directory for SQLite
RUN mkdir -p /var/lib/control-plane

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:8080/api/v1/projects || exit 1

# Run Control Plane
ENTRYPOINT ["/usr/local/bin/control-plane"]
CMD ["-port", "8080", "-db-type", "sqlite", "-db-dsn", "/var/lib/control-plane/control_plane.db"]

