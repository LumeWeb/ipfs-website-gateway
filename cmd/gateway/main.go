package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/urfave/cli/v3"
	"go.uber.org/zap"

	"go.lumeweb.com/ipfs-website-gateway/internal/api"
	"go.lumeweb.com/ipfs-website-gateway/internal/cache"
	"go.lumeweb.com/ipfs-website-gateway/internal/config"
	"go.lumeweb.com/ipfs-website-gateway/internal/dns"
	"go.lumeweb.com/ipfs-website-gateway/internal/health"
	"go.lumeweb.com/ipfs-website-gateway/internal/ipfs"
	"go.lumeweb.com/ipfs-website-gateway/internal/server"
)

var (
	cfg     *config.Config
	logger  *zap.Logger
	node    *ipfs.Node
	srv     *server.Server
)

// DNSValidatorAdapter adapts the dns.ValidateDNSLink function to the server.DNSValidator interface.
type DNSValidatorAdapter struct{}

// ValidateDNSLink implements the server.DNSValidator interface.
func (d *DNSValidatorAdapter) ValidateDNSLink(ctx context.Context, domain string) (string, error) {
	return dns.ValidateDNSLink(ctx, domain)
}

func main() {
	cmd := &cli.Command{
		Name:  "gateway",
		Usage: "Edge IPFS gateway for DNSLink websites",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "config",
				Usage: "Path to config file directory",
			},
		},
		Action: runGateway,
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		os.Exit(1)
	}
}

func runGateway(ctx context.Context, cmd *cli.Command) error {
	// 1. Load configuration using Manager
	var err error
	
	// Prepare manager options
	var opts []config.ManagerOption
	if configPath := cmd.String("config"); configPath != "" {
		opts = append(opts, config.WithConfigPaths([]string{configPath}))
	}
	
	// Create config manager
	cfgMgr, err := config.NewManager(opts...)
	if err != nil {
		return fmt.Errorf("failed to create config manager: %w", err)
	}
	
	// Initialize temporary logger for config loading
	logger = zap.NewNop()
	cfgMgr.SetLogger(logger)
	
	// Load configuration
	if err := cfgMgr.Init(); err != nil {
		return fmt.Errorf("failed to initialize config: %w", err)
	}
	cfg = cfgMgr.Config()

	// 2. Initialize logger (Zap) with loaded config
	logger, err = initLogger(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	defer logger.Sync()
	
	// Set the real logger on config manager
	cfgMgr.SetLogger(logger)

	logger.Info("starting IPFS gateway",
		zap.String("version", "1.0.0"),
		zap.Int("port", cfg.Server.Port),
	)

	// 3. Initialize IPFS node
	node, err = initIPFSNode(ctx, cfg, logger)
	if err != nil {
		logger.Error("failed to initialize IPFS node", zap.Error(err))
		return fmt.Errorf("failed to initialize IPFS node: %w", err)
	}
	defer node.Close()

	logger.Info("IPFS node initialized",
		zap.String("peer_id", node.Host.ID().String()),
		zap.Int("addrs", len(node.Host.Addrs())),
	)

	// 4. Initialize API client (using swagger-based client)
	apiClient := api.NewSwaggerClient(cfg.API.URL, cfg.API.Secret, cfg.API.Timeout)
	logger.Info("API client initialized", zap.String("url", cfg.API.URL))

	// 5. Initialize caches
	statusCache, err := cache.NewStatusCache(cfg.Cache.StatusCacheLRUSize, cfg.Cache.StatusCacheTTL)
	if err != nil {
		logger.Error("failed to initialize status cache", zap.Error(err))
		return fmt.Errorf("failed to initialize status cache: %w", err)
	}
	logger.Info("Status cache initialized",
		zap.Int("size", cfg.Cache.StatusCacheLRUSize),
		zap.Duration("ttl", cfg.Cache.StatusCacheTTL),
	)

	// 6. Create server
	srv = server.NewServer(cfg, logger)
	
	// Create DNS validator wrapper
	dnsValidator := &DNSValidatorAdapter{}
	srv.SetDNSValidator(dnsValidator)
	srv.SetAPIClient(apiClient)
	srv.SetStatusCache(statusCache)
	srv.SetIPFSFetcher(ipfs.NewFetcher(node, logger))

	// 7. Setup health checker
	healthChecker := health.NewChecker(apiClient, node)
	srv.SetHealthChecker(healthChecker)
	logger.Info("Health checker initialized")

	// 8. Setup graceful shutdown
	setupGracefulShutdown(ctx)

	// 9. Start server
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	logger.Info("server starting", zap.String("addr", addr))
	if err := srv.Start(addr); err != nil {
		logger.Error("server failed", zap.Error(err))
		return fmt.Errorf("server failed: %w", err)
	}

	return nil
}

func initLogger(cfg *config.Config) (*zap.Logger, error) {
	var zapConfig zap.Config

	switch cfg.Logging.Level {
	case "debug":
		zapConfig = zap.NewDevelopmentConfig()
	case "info":
		zapConfig = zap.NewProductionConfig()
		zapConfig.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	case "warn":
		zapConfig = zap.NewProductionConfig()
		zapConfig.Level = zap.NewAtomicLevelAt(zap.WarnLevel)
	case "error":
		zapConfig = zap.NewProductionConfig()
		zapConfig.Level = zap.NewAtomicLevelAt(zap.ErrorLevel)
	default:
		zapConfig = zap.NewProductionConfig()
		zapConfig.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	}

	return zapConfig.Build()
}

func initIPFSNode(ctx context.Context, cfg *config.Config, logger *zap.Logger) (*ipfs.Node, error) {
	return ipfs.NewNode(ctx, cfg.IPFS.SeedPeer, cfg.IPFS.RepoPath, logger)
}

func setupGracefulShutdown(ctx context.Context) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Info("received shutdown signal", zap.String("signal", sig.String()))

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("server shutdown failed", zap.Error(err))
		} else {
			logger.Info("server shutdown complete")
		}
	}()
}
