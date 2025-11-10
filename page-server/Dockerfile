# Dockerfile for Page Server
FROM debian:bookworm-slim

# Install runtime dependencies
RUN apt-get update && apt-get install -y \
    ca-certificates \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Copy binary
COPY page-server /usr/local/bin/page-server
RUN chmod +x /usr/local/bin/page-server

# Create data directory
RUN mkdir -p /var/lib/page-server

# Expose port
EXPOSE 8081

# Health check
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:8081/api/v1/ping || exit 1

# Run Page Server
ENTRYPOINT ["/usr/local/bin/page-server"]
CMD ["-port", "8081", "-data-dir", "/var/lib/page-server"]

