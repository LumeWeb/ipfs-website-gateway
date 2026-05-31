# AGENTS.md
This file provides guidance to various AI agents when working with code in this repository.

## Common Commands

### Building
```bash
# Build for current platform
go build -o gateway cmd/gateway/main.go

# Build for specific platforms
GOOS=linux GOARCH=amd64 go build -o gateway-linux-amd64 cmd/gateway/main.go
GOOS=darwin GOARCH=amd64 go build -o gateway-darwin-amd64 cmd/gateway/main.go
GOOS=windows GOARCH=amd64 go build -o gateway-windows-amd64.exe cmd/gateway/main.go
```

### Testing
```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run tests for specific package
go test ./internal/server/...

# Run tests with verbose output
go test -v ./...
```

### Linting
```bash
go vet ./...
golangci-lint run ./...
```

### Running
```bash
# Using environment variables
export GATEWAY__API__URL=https://api.example.com
export GATEWAY__API__SECRET=my-secret-key
export GATEWAY__SERVER__PORT=8080
./gateway

# Using config file
./gateway --config /path/to/config/directory

# Using default config (creates ./gateway.yaml if not exists)
./gateway
```

### Generating Mocks
```bash
# Mockery is pre-installed at $HOME/go/bin/mockery
# Run without arguments to generate mocks based on existing configuration
$HOME/go/bin/mockery
```

## High-Level Architecture

This is a stateless edge IPFS gateway that serves DNSLink websites with strict access control via an internal API. The architecture follows a layered request pipeline:

### Active Request Pipeline (Boxo Gateway)
1. **Client Request** → Echo HTTP Server with middleware (Recovery, Logger)
2. **AccessControlMiddleware** → Extract domain from `Host` or `X-Forwarded-Host` header, strip port
3. **Passthrough** → IP addresses and `/ipfs/`/`/ipns/` paths pass through without access control
4. **Status Cache Check** → In-memory LRU cache with TTL (prevents DoS)
5. **Internal API Query** → Validate website access via ipfs-sdk (cache miss)
6. **Status Check** → 404 (not found/denied), 410 (broken/gone), or active
7. **Path Rewrite** → Active domains rewritten to `/ipns/{domain}{path}`
8. **Boxo Gateway Handler** → Serve IPFS content via BlocksBackend (DNSLink resolution, UnixFS, headers handled by boxo)
9. **Response** → Served with proper headers (Content-Type, Cache-Control, ETag)

### Additional Endpoints
- **`GET /healthz`** → Health check (internal API reachability + IPFS peer connections)
- **`GET /allowed?domain=...&secret=...`** → Caddy On-Demand TLS validation (DNSLink + API check, optional rate limiting, auth via `AllowedSecret`)

### Core Components

#### Configuration Layer (`internal/config/`)
- Uses `go.lumeweb.com/configmanager` for multi-source configuration
- Priority: Environment variables > Config file > Default values
- Config file search order (directories, `gateway.yaml` appended): `/etc/lumeweb/gateway`, `$HOME/.lumeweb/gateway`, `./`
- `$HOME` expanded via `os.ExpandEnv()` at load time
- Environment variable format: `GATEWAY__SECTION__KEY` (e.g., `GATEWAY__SERVER__PORT`)
- Validation via Zog schemas on each config struct
- Creates `gateway.yaml` if no config file found

#### Server Layer (`internal/server/`)
- Echo-based HTTP server with dependency injection via setter methods
- Interfaces defined in server package: `DNSValidator`, `StatusCache`
- `api.APIClient` and `health.Checker` imported from external packages
- IP extraction uses custom `e.IPExtractor` (not `middleware.RealIP`): prioritizes `X-Real-IP` (for Caddy proxy) over `X-Forwarded-For`
- Supports graceful shutdown on SIGINT/SIGTERM
- Auth middleware for `/allowed` endpoint validates `secret` query param against `ServerConfig.AllowedSecret`

#### DNS Layer (`internal/dns/`)
- Exposes bare function `ValidateDNSLink(ctx, domain) (string, error)` (not a struct)
- Uses `github.com/dnslink-std/go` for DNSLink validation
- Queries `_dnslink.{domain}` TXT records via custom `LookupTXT` wrapping `net.DefaultResolver`
- Supports both `/ipfs/` and `/ipns/` namespaces
- Returns IPFS path string (e.g., `/ipfs/Qm...`), not `path.Path`
- Default timeout: 5 seconds
- `main.go` provides `DNSValidatorAdapter` to satisfy `server.DNSValidator` interface

#### API Layer (`internal/api/`)
- Single implementation: `sdkClient` using `go.lumeweb.com/ipfs-sdk`
- `NewClient(baseURL, secret string, timeout time.Duration)` creates SDK client with gateway secret
- SDK handles authentication and endpoint routing internally
- Timeout is set on the SDK's underlying `http.Client`
- `NewClientFromWebsitesService(websites)` for test injection
- Error handling delegates to SDK; callers check error strings for status

#### Caching Layer (`internal/cache/`)
- **Status Cache**: In-memory LRU cache with TTL for website status queries
  - `Get(domain) *CacheResult` returns Hit/Expired/Entry fields
  - Caches both positive and negative results (DoS protection)
  - Expiration-based eviction
  - Used by Gateway's `CheckAccess()`
- **Content Cache**: Disk-based block cache with LRU eviction
  - Nginx-style directory hashing (levels=1:2) for scalability
  - Monitors disk usage and enforces max bytes threshold
  - Backward compatible with legacy flat structure
  - Wrapped by `ContentBlockstore` adapter implementing `blockstore.Blockstore`
- **ContentBlockstore**: Adapter that wraps ContentCache to satisfy Boxo's `blockstore.Blockstore` interface
  - Bridges between Boxo's `cid.Cid`/`blocks.Block` types and ContentCache's string/[]byte storage
  - Supports all Blockstore methods: Get, Put, Has, DeleteBlock, GetSize, PutMany, AllKeysChan, View
  - Returns `ipld.ErrNotFound` on cache miss (critical — allows blockservice to fall through to Bitswap)
  - Used as the base blockstore for the IPFS node

#### IPFS Layer (`internal/ipfs/`)
- **Node**: Minimal Boxo IPFS node with libp2p host and blockservice
  - Accepts `blockstore.Blockstore` via dependency injection (typically `ContentBlockstore`)
  - Bitswap client-only: `bitswap.WithServerEnabled(false)` — cannot serve blocks to peers
  - No DHT — Bitswap has `nil` providerFinder, can only fetch from connected peers
  - Only connects to the configured seed peer (default: `ipfs.pinner.xyz`)
  - This ensures the gateway only serves content the seed peer has — prevents becoming a public gateway
  - Relay disabled: `libp2p.DisableRelay()`
  - Supports NAT traversal and hole punching: `libp2p.EnableHolePunching()`
  - Seed peer `dnsaddr` resolution: plain DNS names auto-prefixed with `/dnsaddr/`
  - Seed peer connect timeout: configurable via `GATEWAY__IPFS__CONNECT_TIMEOUT`
  - `UserAgent: "ipfs-website-gateway/1.0.0"`
- **CreateInMemoryBlockstore()**: Helper for creating in-memory blockstore with Boxo's bloom filter + twoqueue cache acceleration

#### Gateway Layer (`internal/gateway/`)
- **Gateway**: Wraps boxo's `BlocksBackend` and `NewHandler` to serve IPFS content
  - `NewGateway(bs, apiClient, statusCache, logger)` — takes BlockService, API client, status cache, logger
  - Creates DNS resolver → name system (with `routinghelpers.Null{}` — no DHT) → BlocksBackend → handler
  - Gateway config: `NoDNSLink: true` (DNSLink handled by AccessControlMiddleware, not boxo), `DeserializedResponses: true`, `RetrievalTimeout` configurable via `GATEWAY__IPFS__RETRIEVAL_TIMEOUT`, empty `PublicGateways` map
  - `CheckAccess(ctx, domain)`: status cache lookup then API query for domain access control
  - `GetDNSLinkRecord(ctx, hostname)`: delegates to backend for DNSLink path resolution
  - Implements `http.Handler` via `ServeHTTP()`
- **AccessControlMiddleware**: Separate struct with `Wrap(next http.Handler) http.Handler`
  - Extracts domain from `Host` header, falls back to `X-Forwarded-Host`
  - Strips port via `net.SplitHostPort()` (IPv6-safe)
  - IP addresses pass through without access control
  - `/ipfs/` and `/ipns/` path prefixes pass through
  - Active websites: rewrites path to `/ipns/{domain}{originalPath}`
  - 404 for denied/not found, 410 for broken
  - Does NOT perform DNSLink validation (that's only in `/allowed` endpoint)

#### Health Layer (`internal/health/`)
- Health checker with 10-second timeout (hard-coded)
- Defines its own `APIClient` and `IPFSNode` interfaces (not reusing `api.APIClient` directly)
- `IPFSNode` requires `Close()` even though health check never calls it
- Checks: `internal_api` (queries `GetWebsite("health-check.example.com")`, 404/410 = healthy), `ipfs_peer` (has addrs + has connected peers)
- Endpoint: `/healthz`

### Directory Structure

- `cmd/gateway/` - CLI entry point using urfave/cli/v3
- `docs/` - Documentation (Caddy SSL configuration)
- `internal/config/` - Configuration structures, manager wrapper, Zog validation schemas
- `internal/server/` - Echo server setup, middleware, handlers
- `internal/dns/` - DNSLink validation (bare function)
- `internal/api/` - Internal API client using ipfs-sdk
- `internal/cache/` - Status cache (in-memory), content cache (disk-based), ContentBlockstore adapter
- `internal/ipfs/` - IPFS node setup with dependency-injected blockstore
- `internal/gateway/` - Boxo-based gateway handler with access control middleware
- `internal/health/` - Health check setup
- `pkg/types/` - Status constants, CacheEntry, CacheResult, GatewayWebsiteResponse alias to SDK type
- `vendor/` - Go dependencies (vendored)

## Important Patterns and Conventions

### Dependency Injection
Components use setter methods for dependency injection rather than constructor injection:
- `SetDNSValidator(dns DNSValidator)`
- `SetAPIClient(api api.APIClient)`
- `SetStatusCache(cache StatusCache)`
- `SetHealthChecker(checker health.Checker)`
- `SetGateway(g *gw.Gateway)`

The IPFS Node uses constructor injection:
- `NewNode(ctx, seedPeer, blockstore.Blockstore, logger)`

### Interface Design
Interfaces defined by consumers (Go convention):
- `server.DNSValidator` - DNSLink validation (returns path string)
- `server.StatusCache` - Website status caching
- `api.APIClient` - Internal API communication
- `health.APIClient` - API health check (structurally identical to api.APIClient)
- `health.IPFNode` - IPFS node operations (includes Close())

### Configuration Management
- Configuration structs implement `Defaults()` method for default values
- Configuration structs implement `Schema()` method returning Zog validation schemas
- Use `config:"field"` tags for configmanager integration
- Time durations use `time.Duration` type
- Environment variables use double underscore format

### Error Handling
- API client returns SDK errors; callers check error strings for status
- Health check treats 404/410 responses as healthy (API is reachable)
- Context cancellation is respected throughout
- Graceful degradation (e.g., seed peer connection failure doesn't stop startup)
- `http.ErrServerClosed` silently ignored on graceful shutdown

### Testing
- Tests use table-driven patterns
- Mocks generated with mockery (pre-installed)
- Use `zap.NewNop()` for silent logging in tests
- IPFS node tests use `cache.ContentBlockstore` with temp directory

### Content Cache Eviction
- LRU eviction using `GetOldest()` for O(1) access
- Fallback to disk-based eviction if LRU cache is empty
- Handles both nested directory structure and legacy flat structure
- Thread-safe with mutex protection

## Configuration Reference

### Go Version
- Go 1.26+ required (go.mod specifies `go 1.26.0`)

### Required Settings
- `GATEWAY__API__URL` - Internal API base URL
- `GATEWAY__API__SECRET` - Gateway secret for API authentication

### Optional Settings (with defaults)
- `GATEWAY__SERVER__PORT`: 8080
- `GATEWAY__SERVER__TRUSTED_PROXIES`: []
- `GATEWAY__SERVER__ALLOWED_SECRET`: "" (auth for /allowed endpoint; empty = no auth)
- `GATEWAY__API__TIMEOUT`: 30s
- `GATEWAY__IPFS__SEED_PEER`: "ipfs.pinner.xyz"
- `GATEWAY__IPFS__CONNECT_TIMEOUT`: 30s
- `GATEWAY__IPFS__RETRIEVAL_TIMEOUT`: 30s
- `GATEWAY__CACHE__STATUS_CACHE_TTL`: 5m
- `GATEWAY__CACHE__STATUS_CACHE_LRU_SIZE`: 1000
- `GATEWAY__CACHE__CONTENT_CACHE_PATH`: "/data/cache"
- `GATEWAY__CACHE__CONTENT_CACHE_MAX_BYTES`: 10737418240 (10GB)
- `GATEWAY__CACHE__CONTENT_CACHE_LRU_SIZE`: 100000
- `GATEWAY__CACHE__IPNS_CACHE_PATH`: "/data/ipns"
- `GATEWAY__CACHE__IPNS_CACHE_LRU_SIZE`: 140000
- `GATEWAY__LOGGING__LEVEL`: "info"
- `GATEWAY__RATE_LIMIT__ENABLED`: false
- `GATEWAY__RATE_LIMIT__RATE`: 0.167 (requests per second)
- `GATEWAY__RATE_LIMIT__BURST`: 10
- `GATEWAY__RATE_LIMIT__EXPIRES_IN`: 5m
