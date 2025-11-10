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
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/linux/projects/server/control-plane/internal/api"
	"github.com/linux/projects/server/control-plane/internal/autoscaling"
	"github.com/linux/projects/server/control-plane/internal/billing"
	"github.com/linux/projects/server/control-plane/internal/compute"
	"github.com/linux/projects/server/control-plane/internal/multitenancy"
	"github.com/linux/projects/server/control-plane/internal/project"
	"github.com/linux/projects/server/control-plane/internal/proxy"
	"github.com/linux/projects/server/control-plane/internal/scheduler"
	"github.com/linux/projects/server/control-plane/internal/state"
)

func main() {
	var (
		port          = flag.Int("port", 8080, "API server port")
		proxyPort     = flag.Int("proxy-port", 3306, "Proxy server port for MySQL connections")
		dbDSN         = flag.String("db-dsn", "", "Database DSN (PostgreSQL) or path (SQLite). Default: SQLite at ./control_plane.db")
		dbType        = flag.String("db-type", "sqlite", "Database type: 'postgres' or 'sqlite' (default: sqlite)")
		kubeconfig    = flag.String("kubeconfig", "", "Path to kubeconfig file (empty for in-cluster)")
		namespace     = flag.String("namespace", "default", "Kubernetes namespace")
		idleTimeout      = flag.Duration("idle-timeout", 5*time.Minute, "Idle timeout before suspending compute nodes")
		checkInterval     = flag.Duration("check-interval", 30*time.Second, "Interval for checking idle compute nodes")
		enableProxy       = flag.Bool("enable-proxy", true, "Enable connection proxy (default: true)")
		enableAutoscaling = flag.Bool("enable-autoscaling", true, "Enable auto-scaling (default: true)")
		scaleCheckInterval = flag.Duration("scale-check-interval", 1*time.Minute, "Interval for checking scaling metrics")
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
	
	// Initialize billing/usage tracker (mimics Neon's consumption metrics)
	usageTracker := billing.NewUsageTracker(stateStore)
	
	// Pass usage tracker to compute manager for automatic tracking
	computeManager.SetUsageTracker(usageTracker)
	
	// Initialize network policy manager for multi-tenancy isolation
	k8sClient, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		log.Fatalf("Failed to create Kubernetes client: %v", err)
	}
	networkPolicyManager := multitenancy.NewNetworkPolicyManager(k8sClient, *namespace)
	
	// Pass network policy manager to project manager for automatic policy creation
	projectManager.SetNetworkPolicyManager(networkPolicyManager)

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

	// Initialize and start auto-scaler (mimics Neon's autoscaling)
	if *enableAutoscaling {
		autoScaler := autoscaling.NewScaler(computeManager, stateStore, *scaleCheckInterval)
		go autoScaler.Start()
		defer autoScaler.Stop()
		log.Println("Auto-scaling enabled")
	}

	// Initialize and start connection proxy (mimics Neon's proxy)
	if *enableProxy {
		controlPlaneURL := fmt.Sprintf("http://localhost:%d", *port)
		proxyRouter := proxy.NewRouter(computeManager, controlPlaneURL, *proxyPort)
		go func() {
			log.Printf("Starting connection proxy on port %d", *proxyPort)
			if err := proxyRouter.Start(); err != nil {
				log.Printf("Proxy server error: %v", err)
			}
		}()
	}

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
