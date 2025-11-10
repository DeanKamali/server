package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/linux/projects/server/control-plane/internal/api"
	"github.com/linux/projects/server/control-plane/internal/compute"
	"github.com/linux/projects/server/control-plane/internal/project"
	"github.com/linux/projects/server/control-plane/internal/scheduler"
	"github.com/linux/projects/server/control-plane/internal/state"
)

func main() {
	var (
		port          = flag.Int("port", 8080, "API server port")
		dbDSN         = flag.String("db-dsn", "", "Database DSN (PostgreSQL) or path (SQLite). Default: SQLite at ./control_plane.db")
		dbType        = flag.String("db-type", "sqlite", "Database type: 'postgres' or 'sqlite' (default: sqlite)")
		kubeconfig    = flag.String("kubeconfig", "", "Path to kubeconfig file (empty for in-cluster)")
		namespace     = flag.String("namespace", "default", "Kubernetes namespace")
		idleTimeout   = flag.Duration("idle-timeout", 5*time.Minute, "Idle timeout before suspending compute nodes")
		checkInterval = flag.Duration("check-interval", 30*time.Second, "Interval for checking idle compute nodes")
	)
	flag.Parse()

	// Initialize state store
	var stateStore state.StoreInterface
	var err error

	if *dbType == "postgres" {
		if *dbDSN == "" {
			*dbDSN = "postgres://postgres:postgres@localhost:5432/control_plane?sslmode=disable"
		}
		stateStore, err = state.NewStore(*dbDSN)
		if err != nil {
			log.Fatalf("Failed to initialize PostgreSQL state store: %v", err)
		}
		log.Println("Using PostgreSQL for state storage")
	} else {
		// SQLite (default)
		if *dbDSN == "" {
			*dbDSN = "./control_plane.db"
		}
		stateStore, err = state.NewSQLiteStore(*dbDSN)
		if err != nil {
			log.Fatalf("Failed to initialize SQLite state store: %v", err)
		}
		log.Printf("Using SQLite for state storage: %s", *dbDSN)
	}
	defer stateStore.Close()

	// Initialize Kubernetes client
	var k8sConfig *rest.Config
	if *kubeconfig == "" {
		// In-cluster config
		k8sConfig, err = rest.InClusterConfig()
		if err != nil {
			log.Fatalf("Failed to get in-cluster config: %v", err)
		}
	} else {
		// Out-of-cluster config
		k8sConfig, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)
		if err != nil {
			log.Fatalf("Failed to build kubeconfig: %v", err)
		}
	}

	// Initialize managers
	projectManager := project.NewManager(stateStore)
	computeManager, err := compute.NewManager(k8sConfig, stateStore, *namespace)
	if err != nil {
		log.Fatalf("Failed to create compute manager: %v", err)
	}

	// Initialize suspend scheduler
	suspendScheduler := scheduler.NewSuspendScheduler(
		computeManager,
		stateStore,
		*idleTimeout,
		*checkInterval,
	)

	// Start suspend scheduler
	go suspendScheduler.Start()
	defer suspendScheduler.Stop()

	// Initialize API handler
	apiHandler := api.NewHandler(projectManager, computeManager, suspendScheduler)

	// Setup router
	router := gin.Default()
	apiHandler.RegisterRoutes(router)

	// Start server
	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Starting control plane API server on %s", addr)

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := router.Run(addr); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	<-sigChan
	log.Println("Shutting down...")
}
