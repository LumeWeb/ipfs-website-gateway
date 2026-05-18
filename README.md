# IPFS Website Gateway

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go)](https://golang.org/)

A stateless edge IPFS gateway that serves DNSLink websites with strict access control via an internal API. Built with Go, Echo, and Boxo IPFS.

## Features

- **DNSLink vhost support** - Serves websites via virtual hosting using DNSLink records (`_dnslink.{domain}`)
- **Access control** - Validates website access against a central API via ipfs-sdk before serving
- **Status caching** - In-memory LRU cache for website status queries (TTL-based, DoS protection)
- **Boxo IPFS integration** - Lightweight Bitswap client-only node for content fetching from P2P network
- **Caddy On-Demand TLS** - `/allowed` endpoint for certificate issuance validation with optional rate limiting and auth
- **Health monitoring** - `/healthz` endpoint checking internal API and IPFS peer connectivity
- **Graceful shutdown** - Proper cleanup on SIGINT/SIGTERM

## Architecture

```
┌─────────────┐
│   Client    │
└──────┬──────┘
       │ HTTP Request
       ▼
┌─────────────────────────────────┐
│   Echo HTTP Server              │
│   - Recovery middleware         │
│   - Logger middleware           │
└──────┬──────────────────────────┘
       │ AccessControlMiddleware
       ▼
┌─────────────────────────────────┐
│   Request Pipeline              │
│   1. Extract domain from Host   │
│      or X-Forwarded-Host        │
│   2. Strip port (IPv6-safe)    │
│   3. Passthrough for IPs and   │
│      /ipfs/, /ipns/ paths      │
│   4. Check status cache (LRU)   │
│   5. Query internal API         │
│   6. Check result               │
│      (404/410/active)           │
│   7. Rewrite to /ipns/{domain} │
└──────┬──────────────────────────┘
       │
       ▼
┌─────────────────────────────────┐
│   Boxo Gateway Handler          │
│   - DNSLink resolution          │
│   - UnixFS content serving      │
│   - Headers (ETag, Cache-Control)│
│   - Range requests              │
└─────────────────────────────────┘
```

## Installation

### Prerequisites

- **Go 1.26** or later
- Network access to internal API
- DNS server with DNSLink records (for serving websites)

### Build

```bash
go build -o gateway cmd/gateway/main.go
```

For other platforms:
```bash
GOOS=linux GOARCH=amd64 go build -o gateway-linux-amd64 cmd/gateway/main.go
GOOS=darwin GOARCH=amd64 go build -o gateway-darwin-amd64 cmd/gateway/main.go
GOOS=windows GOARCH=amd64 go build -o gateway-windows-amd64.exe cmd/gateway/main.go
```

## Usage

### Running the Gateway

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

### CLI Flags

| Flag | Description |
|------|-------------|
| `--config` | Path to config file directory |

### Configuration

Configuration is loaded from multiple sources in priority order:

1. **Environment variables** (highest priority)
2. **Configuration file** (YAML)
3. **Default values** (lowest priority)

#### Config File Search

The gateway searches for `gateway.yaml` in these directories (in order):
- `/etc/lumeweb/gateway/`
- `$HOME/.lumeweb/gateway/`
- `./` (current directory)

If no config file is found, `gateway.yaml` is created in the first writable directory.

#### Example Config File

```yaml
server:
  port: 8080
  trusted_proxies: []
  allowed_secret: ""

api:
  url: https://api.example.com
  secret: my-secret-key
  timeout: 30s

ipfs:
  seed_peer: ipfs.pinner.xyz
  repo_path: ./data/ipfs

cache:
  status_cache_ttl: 5m
  status_cache_lru_size: 1000
  content_cache_path: /tmp/ipfs-cache
  content_cache_max_bytes: 10737418240  # 10GB
  content_cache_lru_size: 100000

logging:
  level: info

rate_limit:
  enabled: false
  rate: 0.167
  burst: 10
  expires_in: 5m
```

#### Environment Variables

All configuration can be set via environment variables using the format `GATEWAY__SECTION__KEY`:

**Required:**
- `GATEWAY__API__URL` - Internal API base URL
- `GATEWAY__API__SECRET` - Gateway secret for API authentication

**Optional:**
| Variable | Description | Default |
|----------|-------------|---------|
| `GATEWAY__SERVER__PORT` | Server port | `8080` |
| `GATEWAY__SERVER__TRUSTED_PROXIES` | Trusted proxy IPs | `[]` |
| `GATEWAY__SERVER__ALLOWED_SECRET` | Auth secret for /allowed endpoint | `""` |
| `GATEWAY__API__TIMEOUT` | API request timeout | `30s` |
| `GATEWAY__IPFS__SEED_PEER` | IPFS seed peer | `ipfs.pinner.xyz` |
| `GATEWAY__IPFS__REPO_PATH` | IPFS repository path | `./data/ipfs` |
| `GATEWAY__CACHE__STATUS_CACHE_TTL` | Status cache TTL | `5m` |
| `GATEWAY__CACHE__STATUS_CACHE_LRU_SIZE` | Status cache size | `1000` |
| `GATEWAY__CACHE__CONTENT_CACHE_PATH` | Content cache directory | `/tmp/ipfs-cache` |
| `GATEWAY__CACHE__CONTENT_CACHE_MAX_BYTES` | Content cache max size | `10737418240` (10GB) |
| `GATEWAY__CACHE__CONTENT_CACHE_LRU_SIZE` | Content cache size | `100000` |
| `GATEWAY__LOGGING__LEVEL` | Log level | `info` |
| `GATEWAY__RATE_LIMIT__ENABLED` | Enable rate limiting on /allowed | `false` |
| `GATEWAY__RATE_LIMIT__RATE` | Rate limit (requests/second) | `0.167` |
| `GATEWAY__RATE_LIMIT__BURST` | Rate limit burst | `10` |
| `GATEWAY__RATE_LIMIT__EXPIRES_IN` | Rate limiter cleanup interval | `5m` |

## API Reference

### Health Check

**Endpoint:** `GET /healthz`

Returns health status of gateway dependencies.

**Response:**
```json
{
  "status": "pass",
  "checks": {
    "internal_api": {
      "status": "pass"
    },
    "ipfs_peer": {
      "status": "pass",
      "data": {
        "peer_count": 5
      }
    }
  }
}
```

### Gateway Request

**Endpoint:** `ANY /*path`

Serves content from IPFS based on DNSLink resolution of the `Host` (or `X-Forwarded-Host`) header.

**Headers:**
- `Host` - Domain name with DNSLink record
- `X-Forwarded-Host` - Alternative domain header (takes precedence over Host)

**Responses:**
- `200 OK` - Content served successfully
- `404 Not Found` - Website not found or not active
- `410 Gone` - Website is broken or removed
- `500 Internal Server Error` - Server error

**Example:**
```bash
curl -H "Host: example.com" http://localhost:8080/
```

### Caddy On-Demand TLS

**Endpoint:** `GET /allowed?domain=...&secret=...`

Validates whether a domain is allowed for TLS certificate issuance. Used by Caddy's On-Demand TLS feature.

**Parameters:**
- `domain` - Domain name to validate
- `secret` - Authentication secret (required if `AllowedSecret` is configured)

**Responses:**
- `200 OK` - Domain is allowed
- `400 Bad Request` - Domain not allowed or auth failed

## Development

### Project Structure

```
.
├── cmd/gateway/          # CLI entry point
├── docs/                 # Documentation
├── internal/
│   ├── api/             # Internal API client (ipfs-sdk)
│   ├── cache/           # Status and content cache
│   ├── config/          # Configuration management
│   ├── dns/             # DNSLink validation
│   ├── gateway/         # Boxo gateway handler + access control
│   ├── health/          # Health checks
│   ├── ipfs/            # IPFS node and fetcher
│   └── server/          # Echo server setup
├── pkg/types/           # Common types
├── vendor/              # Vendored dependencies
├── AGENTS.md            # Agent development guide
├── go.mod               # Go module definition
└── README.md            # This file
```

### Running Tests

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run tests with verbose output
go test -v ./...

# Run tests for specific package
go test ./internal/server/...
```

### Generating Mocks

```bash
# Mockery is pre-installed at $HOME/go/bin/mockery
$HOME/go/bin/mockery
```

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
