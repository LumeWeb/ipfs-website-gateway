package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/urfave/cli/v3"
	"go.uber.org/zap"

	ipfslog "github.com/ipfs/go-log/v2"

	cid "github.com/ipfs/go-cid"
	"github.com/ipfs/boxo/path"

	"go.lumeweb.com/ipfs-website-gateway/internal/api"
	"go.lumeweb.com/ipfs-website-gateway/internal/cache"
	"go.lumeweb.com/ipfs-website-gateway/internal/config"
	"go.lumeweb.com/ipfs-website-gateway/internal/dns"
	gw "go.lumeweb.com/ipfs-website-gateway/internal/gateway"
	"go.lumeweb.com/ipfs-website-gateway/internal/health"
	"go.lumeweb.com/ipfs-website-gateway/internal/ipfs"
	"go.lumeweb.com/ipfs-website-gateway/internal/metrics"
	"go.lumeweb.com/ipfs-website-gateway/internal/otel"
	"go.lumeweb.com/ipfs-website-gateway/internal/prewarm"
	"go.lumeweb.com/ipfs-website-gateway/internal/server"
)

var (
	cfg       *config.Config
	logger    *zap.Logger
	node      *ipfs.Node
	srv       *server.Server
	prewarmer *prewarm.Prewarmer
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

	// Must inject before any Boxo components are created (global singleton)
	if err := metrics.InjectPrometheusAdapter(metrics.Registerer()); err != nil {
		logger.Warn("failed to inject metrics adapter, boxo metrics will be noop", zap.Error(err))
	}

	if err := otel.InitTracing(ctx, cfg.Observability, "1.0.0"); err != nil {
		logger.Warn("failed to initialize OTel tracing", zap.Error(err))
	}

	logger, err = initLogger(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	defer func() { _ = logger.Sync() }()

	logger = otel.InitLogger(cfg.Observability, logger)

	cfgMgr.SetLogger(logger)

	logger.Info("starting IPFS gateway",
		zap.String("version", "1.0.0"),
		zap.Int("port", cfg.Server.Port),
	)

	if cfg.Observability.Enabled {
		logger.Info("observability enabled",
			zap.Bool("tracing", cfg.Observability.IsTracingEnabled()),
			zap.Bool("metrics", cfg.Observability.IsMetricsEnabled()),
			zap.Bool("otel_logging", cfg.Observability.IsLoggingEnabled()),
			zap.String("service_name", cfg.Observability.ServiceName),
		)
		if cfg.Observability.IsTracingEnabled() {
			logger.Info("tracing configured",
				zap.Float64("sample_ratio", cfg.Observability.Tracing.SampleRatio),
			)
		}
		if cfg.Observability.IsMetricsEnabled() {
			logger.Info("metrics endpoint configured",
				zap.String("path", cfg.Observability.Metrics.Path),
				zap.Bool("basic_auth", cfg.Observability.Metrics.IsBasicAuthEnabled()),
			)
		}
		if cfg.Observability.IsLoggingEnabled() {
			logger.Info("otel logging configured",
				zap.String("level", cfg.Observability.Logging.Level),
			)
		}
	}

	apiClient, err := api.NewClient(cfg.API.URL, cfg.API.Secret, cfg.API.Timeout)
	if err != nil {
		logger.Error("failed to initialize API client", zap.Error(err))
		return fmt.Errorf("failed to initialize API client: %w", err)
	}
	logger.Info("API client initialized", zap.String("url", cfg.API.URL))

	// Initialize Redis client
	redisClient, err := cache.NewRedisClient(cfg.Cache.RedisURL, cfg.Cache.RedisPassword, cfg.Cache.RedisDB, cfg.Cache.RedisKeyPrefix)
	if err != nil {
		logger.Error("failed to initialize Redis client", zap.Error(err))
		return fmt.Errorf("failed to initialize Redis client: %w", err)
	}
	defer func() { _ = redisClient.Close() }()
	logger.Info("Redis client initialized", zap.String("url", cfg.Cache.RedisURL))

	statusCache, err := cache.NewStatusCache(cfg.Cache.StatusCacheLRUSize, cfg.Cache.StatusCacheTTL, cfg.Cache.StatusCacheShortTTL, cfg.Cache.StatusCacheStaleTTL, redisClient)
	if err != nil {
		logger.Error("failed to initialize status cache", zap.Error(err))
		return fmt.Errorf("failed to initialize status cache: %w", err)
	}
	logger.Info("Status cache initialized",
		zap.Int("size", cfg.Cache.StatusCacheLRUSize),
		zap.Duration("ttl", cfg.Cache.StatusCacheTTL),
		zap.Duration("short_ttl", cfg.Cache.StatusCacheShortTTL),
		zap.Duration("stale_ttl", cfg.Cache.StatusCacheStaleTTL),
	)

	statusCache.SetAPIClient(apiClient)

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

	gateway, err := gw.NewGateway(node.BlockService, apiClient, statusCache, logger, cfg.IPFS.RetrievalTimeout, node.Routing, cfg.Cache.IPNSCacheLRUSize, cfg.Cache.IPNSCacheFreshTTL, redisClient, cfg.IPFS.PubsubEnabled, cfg.Server.GatewayDomain)
	if err != nil {
		logger.Error("failed to initialize gateway", zap.Error(err))
		return fmt.Errorf("failed to initialize gateway: %w", err)
	}
	defer gateway.Close()
	srv.SetGateway(gateway)

	if cfg.Server.GatewayDomain != "" {
		logger.Info("Gateway domain configured",
			zap.String("domain", cfg.Server.GatewayDomain),
		)
	}

	logger.Info("Gateway handler initialized")

	if cfg.Prewarm.Enabled {
		prewarmer, err = prewarm.NewPrewarmer(ctx, node.BlockService, logger, cfg.IPFS.RetrievalTimeout, cfg.Prewarm.MaxConc, cfg.Prewarm.RetryAttempts, cfg.Prewarm.RetryDelay)
		if err != nil {
			logger.Error("failed to initialize prewarmer", zap.Error(err))
			return fmt.Errorf("failed to initialize prewarmer: %w", err)
		}
		gateway.SetPrewarmCallback(func(key string, resolvedPath path.Path) {
			segments := resolvedPath.Segments()
			if len(segments) < 2 || segments[0] != "ipfs" {
				return
			}
			rootCID, err := cid.Decode(segments[1])
			if err != nil {
				logger.Warn("prewarm: failed to decode CID from path",
					zap.String("path", resolvedPath.String()),
					zap.Error(err),
				)
				return
			}
			prewarmer.Submit(rootCID)
		})
		logger.Info("Cache pre-warming enabled",
			zap.Int("max_concurrency", cfg.Prewarm.MaxConc),
		)
	}

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

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	logger.Info("server starting", zap.String("addr", addr))

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := srv.Start(ctx, addr); err != nil && err != http.ErrServerClosed {
		logger.Error("server failed", zap.Error(err))
		return fmt.Errorf("server failed: %w", err)
	}

	if prewarmer != nil {
		prewarmer.Stop()
	}

	if err := otel.Shutdown(context.Background()); err != nil {
		logger.Error("OTel shutdown failed", zap.Error(err))
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
			"routing/http/client",
			"routing/http/contentrouter",
			"prewarm",
		}
		for _, s := range subsystems {
			_ = ipfslog.SetLogLevel(s, "debug")
		}
	}

	return zapConfig.Build()
}

func initIPFSNode(ctx context.Context, cfg *config.Config, bs *cache.ContentBlockstore, logger *zap.Logger) (*ipfs.Node, error) {
	return ipfs.NewNode(ctx, cfg.IPFS.SeedPeer, cfg.IPFS.ConnectTimeout, cfg.IPFS.RoutingEndpoint(), bs, logger, cfg.IPFS.PubsubEnabled, cfg.IPFS.Seed)
}


