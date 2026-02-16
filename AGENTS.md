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

# Run a single test file
go test -v ./internal/server/server_test.go
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

### Regenerating API Client
```bash
# Generate OpenAPI client from swagger.yaml
oapi-codegen -config oai-codegen.yaml
```

## High-Level Architecture

This is a stateless edge IPFS gateway that serves DNSLink websites with strict access control via an internal API. The architecture follows a layered request pipeline:

### Request Pipeline
1. **Client Request** → Echo HTTP Server with middleware (RealIP, Logger, Recovery)
2. **Domain Extraction** → Extract domain from Host header
3. **DNSLink Validation** → Verify `_dnslink.{domain}` TXT record
4. **Status Cache Check** → In-memory LRU cache with TTL (prevents DoS)
5. **Internal API Query** → Validate website access via `X-Gateway-Secret` header
6. **Status Check** → Return 404 (not found) or 410 (broken/gone) based on response
7. **IPFS Content Fetch** → Retrieve content from P2P network via Boxo
8. **Response** → Serve with proper headers (Content-Type, Cache-Control, ETag)

### Core Components

#### Configuration Layer (`internal/config/`)
- Uses `go.lumeweb.com/configmanager` for multi-source configuration
- Priority: Environment variables > Config file > Default values
- Config file search order: `/etc/lumeweb/gateway/gateway.yaml`, `$HOME/.lumeweb/gateway/gateway.yaml`, `./gateway.yaml`
- Environment variable format: `GATEWAY__SECTION__KEY` (e.g., `GATEWAY__SERVER__PORT`)

#### Server Layer (`internal/server/`)
- Echo-based HTTP server with dependency injection for components
- Interfaces: `DNSValidator`, `APIClient`, `StatusCache`, `IPFSFetcher`, `health.Checker`
- IP extraction prioritizes `X-Real-IP` (for Caddy proxy) over `X-Forwarded-For`
- Supports graceful shutdown on SIGINT/SIGTERM

#### DNS Layer (`internal/dns/`)
- Uses `github.com/dnslink-std/go` for DNSLink validation
- Queries `_dnslink.{domain}` TXT records
- Supports both `/ipfs/` and `/ipns/` namespaces
- Default timeout: 5 seconds

#### API Layer (`internal/api/`)
- Two implementations: direct HTTP client (`client.go`) and swagger-generated (`client_swagger.go`)
- Endpoint: `{baseURL}/internal/websites/{domain}`
- Authentication: `X-Gateway-Secret` header
- Error handling: 404 (not found), 410 (broken/gone), other HTTP errors

#### Caching Layer (`internal/cache/`)
- **Status Cache**: In-memory LRU cache with TTL for website status queries
  - Caches both positive and negative results (DoS protection)
  - Expiration-based eviction
- **Content Cache**: Disk-based block cache with LRU eviction
  - Nginx-style directory hashing (levels=1:2) for scalability
  - Monitors disk usage and enforces max bytes threshold
  - Backward compatible with legacy flat structure

#### IPFS Layer (`internal/ipfs/`)
- **Node**: Minimal Boxo IPFS node with libp2p host, blockstore, and blockservice
  - Uses in-memory datastore (TODO: integrate with persistent blockstore)
  - Connects to seed peer for bootstrap (default: `ipfs.pinner.xyz`)
  - Supports NAT traversal and hole punching
- **Fetcher**: UnixFS content retrieval with path resolution
  - Supports HTTP range requests
  - Serves `index.html` for directories (SPA support)
  - Handles recursive path traversal

#### Health Layer (`internal/health/`)
- Health checker with 10-second timeout
- Checks: internal API connectivity and IPFS peer connections
- Endpoint: `/healthz`

### Directory Structure

- `cmd/gateway/` - CLI entry point using urfave/cli/v3
- `internal/config/` - Configuration structures and manager wrapper
- `internal/server/` - Echo server setup, middleware, handlers
- `internal/dns/` - DNSLink validation
- `internal/api/` - Internal API client (direct and swagger-generated)
- `internal/cache/` - Status cache (in-memory) and content cache (disk-based)
- `internal/ipfs/` - IPFS node setup and UnixFS content fetching
- `internal/health/` - Health check setup
- `internal/client/` - Auto-generated OpenAPI client from swagger.yaml
- `pkg/types/` - Common types (WebsiteStatus, GatewayWebsiteResponse, CacheEntry, CacheResult)
- `vendor/` - Go dependencies (vendored)

## Important Patterns and Conventions

### Dependency Injection
Components use setter methods for dependency injection rather than constructor injection:
- `SetDNSValidator(dns DNSValidator)`
- `SetAPIClient(api api.APIClient)`
- `SetStatusCache(cache StatusCache)`
- `SetIPFSFetcher(fetcher IPFSFetcher)`
- `SetHealthChecker(checker health.Checker)`

### Interface Design
All major components define interfaces for testability:
- `DNSValidator` - DNSLink validation
- `APIClient` - Internal API communication
- `StatusCache` - Website status caching
- `IPFSFetcher` - IPFS content retrieval
- `IPFSNode` - IPFS node operations (for health checks)

### Configuration Management
- Configuration structs implement `Defaults()` method for default values
- Use `config:"field"` tags for configmanager integration
- Time durations use `time.Duration` type
- Environment variables use double underscore format

### Error Handling
- API client returns errors for different HTTP status codes
- 404: "website not found: {domain}"
- 410: "website is broken or gone: {domain}"
- Context cancellation is respected throughout
- Graceful degradation (e.g., seed peer connection failure doesn't stop startup)

### Testing
- Tests use table-driven patterns
- Mocks generated with mockery (pre-installed)
- Test helpers expose internal methods for testing (e.g., `getBlockPathForTest`)
- Use `zap.NewNop()` for silent logging in tests

### Content Cache Eviction
- LRU eviction using `GetOldest()` for O(1) access
- Fallback to disk-based eviction if LRU cache is empty
- Handles both nested directory structure and legacy flat structure
- Thread-safe with mutex protection

### HTTP Range Requests
- Supported via `ParseRangeHeader()` in `internal/ipfs/range.go`
- Validates ranges against file size
- Wraps readers to limit reads to range length
- Returns proper HTTP range headers

## Configuration Reference

### Required Settings
- `GATEWAY__API__URL` - Internal API base URL
- `GATEWAY__API__SECRET` - Gateway secret for API authentication

### Optional Settings (with defaults)
- `GATEWAY__SERVER__PORT`: 8080
- `GATEWAY__SERVER__TRUSTED_PROXIES`: []
- `GATEWAY__API__TIMEOUT`: 30s
- `GATEWAY__IPFS__SEED_PEER`: "ipfs.pinner.xyz"
- `GATEWAY__IPFS__REPO_PATH`: "./data/ipfs"
- `GATEWAY__CACHE__STATUS_CACHE_TTL`: 5m
- `GATEWAY__CACHE__STATUS_CACHE_LRU_SIZE`: 1000
- `GATEWAY__CACHE__CONTENT_CACHE_PATH`: "/tmp/ipfs-cache"
- `GATEWAY__CACHE__CONTENT_CACHE_MAX_BYTES`: 10737418240 (10GB)
- `GATEWAY__CACHE__CONTENT_CACHE_LRU_SIZE`: 100000
- `GATEWAY__LOGGING__LEVEL`: "info"

## TODOs and Future Work

From the codebase:
- IPFS datastore integration with persistent blockstore from cache package (Phase 7)
- IPFS gateway handler implementation (currently returns "not yet implemented")
