package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/urfave/cli/v3"
	"go.uber.org/zap"

	ipfslog "github.com/ipfs/go-log/v2"

	"go.lumeweb.com/ipfs-website-gateway/internal/api"
	"go.lumeweb.com/ipfs-website-gateway/internal/cache"
	"go.lumeweb.com/ipfs-website-gateway/internal/config"
	"go.lumeweb.com/ipfs-website-gateway/internal/dns"
	gw "go.lumeweb.com/ipfs-website-gateway/internal/gateway"
	"go.lumeweb.com/ipfs-website-gateway/internal/health"
	"go.lumeweb.com/ipfs-website-gateway/internal/ipfs"
	"go.lumeweb.com/ipfs-website-gateway/internal/server"
)

var (
	cfg    *config.Config
	logger *zap.Logger
	node   *ipfs.Node
	srv    *server.Server
)

type DNSValidatorAdapter struct{}

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
		logger, _ := zap.NewDevelopment()
		logger.Error("gateway failed to start", zap.Error(err))
		_ = logger.Sync()
		os.Exit(1)
	}
}

func runGateway(ctx context.Context, cmd *cli.Command) error {
	var err error

	var opts []config.ManagerOption
	if configPath := cmd.String("config"); configPath != "" {
		opts = append(opts, config.WithConfigPaths([]string{configPath}))
	}

	cfgMgr, err := config.NewManager(opts...)
	if err != nil {
		return fmt.Errorf("failed to create config manager: %w", err)
	}

	logger = zap.NewNop()
	cfgMgr.SetLogger(logger)

	if err := cfgMgr.Init(); err != nil {
		return fmt.Errorf("failed to initialize config: %w", err)
	}
	cfg = cfgMgr.Config()

	logger, err = initLogger(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	defer func() { _ = logger.Sync() }()

	cfgMgr.SetLogger(logger)

	logger.Info("starting IPFS gateway",
		zap.String("version", "1.0.0"),
		zap.Int("port", cfg.Server.Port),
	)

	statusCache, err := cache.NewStatusCache(cfg.Cache.StatusCacheLRUSize, cfg.Cache.StatusCacheTTL)
	if err != nil {
		logger.Error("failed to initialize status cache", zap.Error(err))
		return fmt.Errorf("failed to initialize status cache: %w", err)
	}
	logger.Info("Status cache initialized",
		zap.Int("size", cfg.Cache.StatusCacheLRUSize),
		zap.Duration("ttl", cfg.Cache.StatusCacheTTL),
	)

	apiClient, err := api.NewClient(cfg.API.URL, cfg.API.Secret, cfg.API.Timeout)
	if err != nil {
		logger.Error("failed to initialize API client", zap.Error(err))
		return fmt.Errorf("failed to initialize API client: %w", err)
	}
	logger.Info("API client initialized", zap.String("url", cfg.API.URL))

	contentCache, err := cache.NewContentCache(
		cfg.Cache.ContentCachePath,
		cfg.Cache.ContentCacheMaxBytes,
		cfg.Cache.ContentCacheLRUSize,
	)
	if err != nil {
		logger.Error("failed to initialize content cache", zap.Error(err))
		return fmt.Errorf("failed to initialize content cache: %w", err)
	}
	logger.Info("Content cache initialized",
		zap.String("path", cfg.Cache.ContentCachePath),
		zap.Int64("max_bytes", cfg.Cache.ContentCacheMaxBytes),
	)

	contentBs := cache.NewContentBlockstore(contentCache, logger)

	node, err = initIPFSNode(ctx, cfg, contentBs, logger)
	if err != nil {
		logger.Error("failed to initialize IPFS node", zap.Error(err))
		return fmt.Errorf("failed to initialize IPFS node: %w", err)
	}
	defer func() { _ = node.Close() }()

	logger.Info("IPFS node initialized",
		zap.String("peer_id", node.Host.ID().String()),
		zap.Int("addrs", len(node.Host.Addrs())),
	)

	srv = server.NewServer(cfg, logger)

	dnsValidator := &DNSValidatorAdapter{}
	srv.SetDNSValidator(dnsValidator)
	srv.SetAPIClient(apiClient)
	srv.SetStatusCache(statusCache)

	gateway, err := gw.NewGateway(node.BlockService, apiClient, statusCache, logger, cfg.IPFS.RetrievalTimeout)
	if err != nil {
		logger.Error("failed to initialize gateway", zap.Error(err))
		return fmt.Errorf("failed to initialize gateway: %w", err)
	}
	srv.SetGateway(gateway)
	logger.Info("Gateway handler initialized")

	healthChecker := health.NewChecker(apiClient, node)
	srv.SetHealthChecker(healthChecker)
	logger.Info("Health checker initialized")

	if cfg.RateLimit.Enabled {
		logger.Info("Rate limiting enabled",
			zap.Float64("rate", cfg.RateLimit.Rate),
			zap.Int("burst", cfg.RateLimit.Burst),
			zap.Duration("expires_in", cfg.RateLimit.ExpiresIn),
		)
	}

	srv.InitializeRoutes()

	setupGracefulShutdown(ctx)

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	logger.Info("server starting", zap.String("addr", addr))
	if err := srv.Start(addr); err != nil && err != http.ErrServerClosed {
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

	if cfg.Logging.Level == "debug" {
		subsystems := []string{
			"boxo/gateway",
			"boxo/gateway/blockstore",
			"blockservice",
			"bitswap",
			"bitswap/client",
			"bitswap/client/getter",
			"bitswap/session",
			"bitswap/bsnet",
			"blockstore",
			"path/resolver",
			"unixfs",
			"namesys",
			"ipns",
		}
		for _, s := range subsystems {
			_ = ipfslog.SetLogLevel(s, "debug")
		}
	}

	return zapConfig.Build()
}

func initIPFSNode(ctx context.Context, cfg *config.Config, bs *cache.ContentBlockstore, logger *zap.Logger) (*ipfs.Node, error) {
	return ipfs.NewNode(ctx, cfg.IPFS.SeedPeer, cfg.IPFS.ConnectTimeout, bs, logger)
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
