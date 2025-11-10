package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/linux/projects/server/page-server/internal/api"
	"github.com/linux/projects/server/page-server/internal/server"
)

var (
	port      = flag.Int("port", 8080, "The server port")
	dataDir   = flag.String("data-dir", "./page-server-data", "Data directory for persistent storage")
	cacheSize = flag.Int("cache-size", 1000, "Maximum number of pages in cache")
	
	// S3/Object Storage flags
	storageBackend = flag.String("storage-backend", "file", "Storage backend: file, s3, or hybrid")
	s3Endpoint     = flag.String("s3-endpoint", "", "S3 endpoint (e.g., https://s3.amazonaws.com or http://minio:9000)")
	s3Bucket       = flag.String("s3-bucket", "", "S3 bucket name")
	s3Region       = flag.String("s3-region", "us-east-1", "AWS region")
	s3AccessKey    = flag.String("s3-access-key", "", "S3 access key ID")
	s3SecretKey    = flag.String("s3-secret-key", "", "S3 secret access key")
	s3Prefix       = flag.String("s3-prefix", "", "Optional prefix for S3 objects")
	s3UseSSL       = flag.Bool("s3-use-ssl", true, "Use SSL/TLS for S3 connections")
	
	// Authentication flags
	apiKey     = flag.String("api-key", "", "API key for authentication (optional)")
	authTokens = flag.String("auth-tokens", "", "Comma-separated list of auth tokens")
	
	// TLS flags
	tlsEnabled  = flag.Bool("tls", false, "Enable TLS/HTTPS")
	tlsCertFile = flag.String("tls-cert", "", "Path to TLS certificate file")
	tlsKeyFile  = flag.String("tls-key", "", "Path to TLS private key file")
)

func main() {
	flag.Parse()

	// Create data directory if it doesn't exist
	if err := os.MkdirAll(*dataDir, 0755); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}

	absDataDir, err := filepath.Abs(*dataDir)
	if err != nil {
		log.Fatalf("Failed to get absolute path: %v", err)
	}

	// Create Page Server configuration
	cfg := server.Config{
		DataDir:     absDataDir,
		CacheSize:   *cacheSize,
		StorageType: *storageBackend,
		S3Endpoint:  *s3Endpoint,
		S3Bucket:    *s3Bucket,
		S3Region:    *s3Region,
		S3AccessKey: *s3AccessKey,
		S3SecretKey: *s3SecretKey,
		S3Prefix:    *s3Prefix,
		S3UseSSL:    *s3UseSSL,
		APIKey:      *apiKey,
		AuthTokens:  *authTokens,
	}

	// Create Page Server
	pageServer, err := server.NewPageServer(cfg)
	if err != nil {
		log.Fatalf("Failed to create Page Server: %v", err)
	}

	// Create HTTP server
	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", *port),
		Handler: nil,
	}
	
	// Configure TLS if enabled
	if err := server.ConfigureTLS(httpServer, *tlsEnabled, *tlsCertFile, *tlsKeyFile); err != nil {
		log.Fatalf("Failed to configure TLS: %v", err)
	}
	
	// Register HTTP handlers
	api.RegisterHandlers(pageServer)

	log.Printf("Page Server starting...")
	log.Printf("  Port: %d", *port)
	log.Printf("  Data Directory: %s", absDataDir)
	log.Printf("  Cache Size: %d pages", *cacheSize)
	
	if pageServer.Auth.IsEnabled() {
		log.Printf("  Authentication: ENABLED")
		if *apiKey != "" {
			log.Printf("    API Key: configured")
		}
		if *authTokens != "" {
			log.Printf("    Auth Tokens: configured")
		}
	} else {
		log.Printf("  Authentication: DISABLED")
	}
	
	if *tlsEnabled {
		log.Printf("  TLS: ENABLED")
		log.Printf("    Certificate: %s", *tlsCertFile)
		log.Printf("    Private Key: %s", *tlsKeyFile)
	} else {
		log.Printf("  TLS: DISABLED")
	}
	
	log.Printf("Endpoints:")
	log.Printf("  POST /api/v1/get_page (auth required)")
	log.Printf("  POST /api/v1/get_pages (auth required, batch)")
	log.Printf("  POST /api/v1/stream_wal (auth required)")
	log.Printf("  GET  /api/v1/ping (no auth)")
	log.Printf("  GET  /api/v1/metrics (auth required)")
	log.Printf("  POST /api/v1/time_travel (auth required)")
	log.Printf("  POST /api/v1/snapshots/create (auth required)")
	log.Printf("  GET  /api/v1/snapshots/list (auth required)")
	log.Printf("  GET  /api/v1/snapshots/get (auth required)")
	log.Printf("  POST /api/v1/snapshots/restore (auth required)")
	
	// Start server with or without TLS
	if *tlsEnabled {
		if err := httpServer.ListenAndServeTLS(*tlsCertFile, *tlsKeyFile); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	} else {
		if err := httpServer.ListenAndServe(); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}
}

