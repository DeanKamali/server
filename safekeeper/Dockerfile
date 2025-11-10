# Dockerfile for Safekeeper
FROM debian:bookworm-slim

# Install runtime dependencies
RUN apt-get update && apt-get install -y \
    ca-certificates \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Copy binary
COPY safekeeper /usr/local/bin/safekeeper
RUN chmod +x /usr/local/bin/safekeeper

# Create data directory
RUN mkdir -p /var/lib/safekeeper

# Expose port
EXPOSE 8082

# Health check
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:8082/api/v1/ping || exit 1

# Run Safekeeper
ENTRYPOINT ["/usr/local/bin/safekeeper"]
CMD ["-port", "8082", "-data-dir", "/var/lib/safekeeper"]

