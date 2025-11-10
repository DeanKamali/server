package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/linux/projects/server/safekeeper/internal/auth"
	"github.com/linux/projects/server/safekeeper/internal/safekeeper"
	"github.com/linux/projects/server/safekeeper/internal/server"
)

var (
	port      = flag.Int("port", 8090, "The server port")
	dataDir   = flag.String("data-dir", "./safekeeper-data", "Data directory for WAL storage")
	replicaID = flag.String("replica-id", "safekeeper-1", "Unique identifier for this Safekeeper replica")
	peers     = flag.String("peers", "", "Comma-separated list of peer Safekeeper endpoints (e.g., http://localhost:8091,http://localhost:8092)")

	// Authentication flags
	apiKey     = flag.String("api-key", "", "API key for authentication (optional)")
	authTokens = flag.String("auth-tokens", "", "Comma-separated list of auth tokens")

	// TLS flags
	tlsEnabled  = flag.Bool("tls", false, "Enable TLS/HTTPS")
	tlsCertFile = flag.String("tls-cert", "", "Path to TLS certificate file")
	tlsKeyFile  = flag.String("tls-key", "", "Path to TLS private key file")

	// Compression flag (Zstd - matching Neon)
	enableCompression = flag.Bool("compression", true, "Enable Zstd compression for WAL (matching Neon's 70% reduction)")

	// Protobuf encoding flag (performance optimization)
	enableProtobuf = flag.Bool("protobuf", false, "Enable Protobuf encoding for WAL records (20-30% performance improvement)")

	// S3 Backup flags
	s3Endpoint  = flag.String("s3-endpoint", "", "S3 endpoint for WAL backup (e.g., https://s3.amazonaws.com)")
	s3Bucket    = flag.String("s3-bucket", "", "S3 bucket for WAL backup")
	s3Region    = flag.String("s3-region", "us-east-1", "AWS region for S3 backup")
	s3AccessKey = flag.String("s3-access-key", "", "S3 access key ID")
	s3SecretKey = flag.String("s3-secret-key", "", "S3 secret access key")
	s3Prefix    = flag.String("s3-prefix", "", "Optional prefix for S3 objects")
	s3UseSSL    = flag.Bool("s3-use-ssl", true, "Use SSL/TLS for S3 connections")
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

	// Parse peers
	var peerList []string
	if *peers != "" {
		peerList = strings.Split(*peers, ",")
		for i, peer := range peerList {
			peerList[i] = strings.TrimSpace(peer)
		}
	}

	log.Printf("Starting Safekeeper:")
	log.Printf("  Replica ID: %s", *replicaID)
	log.Printf("  Port: %d", *port)
	log.Printf("  Data Directory: %s", absDataDir)
	log.Printf("  Peers: %v", peerList)
	log.Printf("  Quorum Size: %d", (len(peerList)+1)/2+1)

	// Setup S3 config if provided
	var s3Config *safekeeper.S3Config
	if *s3Bucket != "" {
		s3Config = &safekeeper.S3Config{
			Endpoint:  *s3Endpoint,
			Bucket:    *s3Bucket,
			Region:    *s3Region,
			AccessKey: *s3AccessKey,
			SecretKey: *s3SecretKey,
			Prefix:    *s3Prefix,
			UseSSL:    *s3UseSSL,
		}
		log.Printf("S3 backup configured: bucket=%s", *s3Bucket)
	}

	// Create Safekeeper instance
	sk, err := safekeeper.NewSafekeeper(absDataDir, *replicaID, peerList, *enableCompression, *enableProtobuf, s3Config)
	if err != nil {
		log.Fatalf("Failed to create Safekeeper: %v", err)
	}

	if *enableCompression {
		log.Printf("WAL compression enabled (Zstd - matching Neon)")
	}

	// Create consensus manager
	consensus := safekeeper.NewConsensus(sk)
	consensus.Start()

	// Create API handler
	apiHandler := safekeeper.NewAPIHandler(sk, consensus)

	// Setup authentication middleware
	var authMiddleware *auth.AuthMiddleware
	if *apiKey != "" || *authTokens != "" {
		authMiddleware = auth.NewAuthMiddleware(*apiKey, *authTokens)
		log.Printf("Authentication enabled")
	}

	// Setup HTTP routes
	mux := http.NewServeMux()

	// Public endpoints
	mux.HandleFunc("/api/v1/ping", apiHandler.HandlePing)
	mux.HandleFunc("/api/v1/metrics", apiHandler.HandleMetrics)
	mux.HandleFunc("/api/v1/get_wal", apiHandler.HandleGetWAL)
	mux.HandleFunc("/api/v1/get_latest_lsn", apiHandler.HandleGetLatestLSN)
	mux.HandleFunc("/api/v1/timelines", apiHandler.HandleListTimelines)
	mux.HandleFunc("/api/v1/timelines/create", apiHandler.HandleCreateTimeline)
	mux.HandleFunc("/api/v1/timelines/", apiHandler.HandleGetTimeline) // Must be before /api/v1/timelines
	mux.HandleFunc("/api/v1/membership/add_peer", apiHandler.HandleAddPeer)
	mux.HandleFunc("/api/v1/membership/remove_peer", apiHandler.HandleRemovePeer)
	mux.HandleFunc("/api/v1/recover_from_peer", apiHandler.HandleRecoverFromPeer)
	mux.HandleFunc("/api/v1/recover_timeline", apiHandler.HandleRecoverTimeline)
	mux.HandleFunc("/api/v1/get_wal_range", apiHandler.HandleGetWALRange)

	// Protected endpoints (WAL streaming)
	streamWALHandler := http.HandlerFunc(apiHandler.HandleStreamWAL)
	if authMiddleware != nil {
		streamWALHandler = authMiddleware.Middleware(apiHandler.HandleStreamWAL)
	}
	mux.HandleFunc("/api/v1/stream_wal", streamWALHandler)

	// Internal endpoints (replication, consensus)
	mux.HandleFunc("/api/v1/replicate_wal", apiHandler.HandleReplicateWAL)
	mux.HandleFunc("/api/v1/request_vote", apiHandler.HandleRequestVote)
	mux.HandleFunc("/api/v1/heartbeat", apiHandler.HandleHeartbeat)

	var handler http.Handler = mux

	// Setup HTTP server
	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", *port),
		Handler: handler,
	}

	// Configure TLS if enabled
	if err := server.ConfigureTLS(httpServer, *tlsEnabled, *tlsCertFile, *tlsKeyFile); err != nil {
		log.Fatalf("Failed to configure TLS: %v", err)
	}

	if *tlsEnabled {
		log.Printf("Starting Safekeeper with TLS on port %d", *port)
		if err := httpServer.ListenAndServeTLS("", ""); err != nil {
			log.Fatalf("Failed to start Safekeeper: %v", err)
		}
	} else {
		log.Printf("Starting Safekeeper on port %d", *port)
		if err := httpServer.ListenAndServe(); err != nil {
			log.Fatalf("Failed to start Safekeeper: %v", err)
		}
	}
}
