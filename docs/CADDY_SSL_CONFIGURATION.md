# Caddy SSL Configuration Guide

This guide explains how to configure Caddy for On-Demand TLS with the IPFS Website Gateway. The gateway uses Caddy's On-Demand TLS feature to automatically obtain SSL certificates for DNSLink websites.

## Overview

The gateway integrates with Caddy to provide automatic HTTPS for all DNSLink websites. When a client makes an HTTPS request to a DNSLink website:

1. Caddy receives the request and checks for an existing certificate
2. If no certificate exists, Caddy queries the gateway's `/allowed` endpoint to validate the domain
3. The gateway validates the DNSLink record and website status via the internal API
4. If valid, Caddy obtains a certificate from Let's Encrypt via HTTP-01 challenge
5. The certificate is cached and used for subsequent requests

## Prerequisites

- **Port 80** must be accessible from the internet (required for HTTP-01 ACME challenge)
- **Port 443** must be accessible for HTTPS traffic
- The gateway must be running and accessible from Caddy
- DNSLink records must be properly configured for your domains

## Caddyfile Configuration

### Basic Caddyfile Example

Create a `Caddyfile` in your Caddy configuration directory:

```caddyfile
{
    # Use Let's Encrypt for certificate management
    acme_ca https://acme-v02.api.letsencrypt.org/directory
    
    # On-Demand TLS configuration
    on_demand_tls {
        # Gateway endpoint for domain validation
        ask http://localhost:8080/allowed
        
        # Optional: Rate limit certificate issuance to prevent abuse
        interval 2m
        burst 5
    }
}

# Reverse proxy all HTTPS traffic to the gateway
:443 {
    reverse_proxy localhost:8080
    
    # Enable On-Demand TLS for all domains
    tls {
        on_demand
    }
}

# Redirect HTTP to HTTPS
:80 {
    redir https://{host}{uri} permanent
}
```

### Staging Environment Configuration

For testing purposes, use Let's Encrypt staging environment to avoid rate limits:

```caddyfile
{
    # Use Let's Encrypt staging CA (no rate limits, untrusted certificates)
    acme_ca https://acme-staging-v02.api.letsencrypt.org/directory
    
    on_demand_tls {
        ask http://localhost:8080/allowed
        interval 2m
        burst 5
    }
}

:443 {
    reverse_proxy localhost:8080
    tls {
        on_demand
    }
}

:80 {
    redir https://{host}{uri} permanent
}
```

### Production Configuration with Custom ACME Endpoint

If you use a different ACME provider (e.g., ZeroSSL):

```caddyfile
{
    # Custom ACME endpoint
    acme_ca https://acme.zerossl.com/v2/DV90
    
    # Email for certificate expiration notices
    email admin@example.com
    
    on_demand_tls {
        ask http://localhost:8080/allowed
        interval 2m
        burst 5
    }
}

:443 {
    reverse_proxy localhost:8080
    tls {
        on_demand
        dns provider  # Optional: Use DNS challenge instead of HTTP-01
    }
}

:80 {
    redir https://{host}{uri} permanent
}
```

## JSON Configuration

Caddy also supports JSON configuration, which is useful for programmatic generation or complex setups.

### Basic JSON Configuration

```json
{
  "apps": {
    "http": {
      "servers": {
        "srv0": {
          "listen": [":443"],
          "tls_connection_policies": [
            {
              "match": {"sni": ["*"]},
              "on_demand": true
            }
          ],
          "routes": [
            {
              "handle": [
                {
                  "handler": "reverse_proxy",
                  "upstreams": [
                    {"dial": "localhost:8080"}
                  ]
                }
              ]
            }
          ]
        },
        "srv1": {
          "listen": [":80"],
          "routes": [
            {
              "handle": [
                {
                  "handler": "static_response",
                  "headers": {
                    "Location": ["https://{http.request.host}{http.request.uri}"]
                  },
                  "status_code": 301
                }
              ]
            }
          ]
        }
      }
    },
    "tls": {
      "automation": {
        "policies": [
          {
            "subjects": ["*"],
            "on_demand": true,
            "issuers": [
              {
                "module": "acme",
                "ca": "https://acme-v02.api.letsencrypt.org/directory"
              }
            ]
          }
        ],
        "on_demand": {
          "ask": "http://localhost:8080/allowed",
          "interval": "2m",
          "burst": 5
        }
      }
    }
  }
}
```

### JSON Configuration with Staging CA

```json
{
  "apps": {
    "http": {
      "servers": {
        "srv0": {
          "listen": [":443"],
          "tls_connection_policies": [
            {
              "match": {"sni": ["*"]},
              "on_demand": true
            }
          ],
          "routes": [
            {
              "handle": [
                {
                  "handler": "reverse_proxy",
                  "upstreams": [
                    {"dial": "localhost:8080"}
                  ]
                }
              ]
            }
          ]
        },
        "srv1": {
          "listen": [":80"],
          "routes": [
            {
              "handle": [
                {
                  "handler": "static_response",
                  "headers": {
                    "Location": ["https://{http.request.host}{http.request.uri}"]
                  },
                  "status_code": 301
                }
              ]
            }
          ]
        }
      }
    },
    "tls": {
      "automation": {
        "policies": [
          {
            "subjects": ["*"],
            "on_demand": true,
            "issuers": [
              {
                "module": "acme",
                "ca": "https://acme-staging-v02.api.letsencrypt.org/directory"
              }
            ]
          }
        ],
        "on_demand": {
          "ask": "http://localhost:8080/allowed",
          "interval": "2m",
          "burst": 5
        }
      }
    }
  }
}
```

## HTTP-01 Challenge Requirements

The HTTP-01 ACME challenge requires Caddy to serve a validation token on port 80:

### Requirements

1. **Port 80 must be accessible** from the internet on the public IP address of your domain
2. **Firewall rules** must allow inbound TCP traffic on port 80
3. **DNS records** must point your domain to the correct IP address
4. **No reverse proxy** should block the `/.well-known/acme-challenge/` path

### How It Works

When Caddy requests a certificate from Let's Encrypt:

1. Let's Encrypt returns a challenge token
2. Caddy serves the token at `http://{domain}/.well-known/acme-challenge/{token}`
3. Let's Encrypt's validation server requests the token
4. If the token matches, Let's Encrypt issues the certificate

### Port 80 Redirect Configuration

The Caddyfile example includes a redirect from HTTP to HTTPS:

```caddyfile
:80 {
    redir https://{host}{uri} permanent
}
```

This redirect does **not** interfere with the ACME challenge because Caddy serves the challenge token before applying the redirect.

## Gateway `/allowed` Endpoint

The `/allowed` endpoint is used by Caddy to validate domains before issuing certificates.

### Endpoint Details

- **URL**: `GET /allowed?domain={domain}`
- **Response Codes**:
  - `200 OK` - Domain is allowed (valid DNSLink and active website)
  - `400 Bad Request` - Missing or invalid domain parameter
  - `403 Forbidden` - Domain is not allowed (invalid DNSLink or inactive website)

### Validation Logic

The gateway validates domains by:

1. Parsing and validating the domain parameter
2. Querying the DNSLink TXT record (`_dnslink.{domain}`)
3. Querying the internal API for website status
4. Returning 200 only if both validations pass

### Security Considerations

- Prevents unauthorized certificate issuance
- Enforces DNSLink requirement before certificate issuance
- Uses the internal API to validate website status
- Implements status caching to prevent DoS attacks

## Certificate Caching and Renewal

### Certificate Caching

Caddy caches certificates in memory and on disk:

- **In-memory cache**: Certificates are kept in memory for fast access
- **Disk cache**: Certificates are persisted to Caddy's data directory
- **Default location**: `/var/lib/caddy` (Linux) or `~/.local/share/caddy` (macOS)

### Certificate Renewal

Caddy automatically renews certificates before they expire:

- **Renewal window**: Certificates are renewed 30 days before expiration
- **Renewal process**: Caddy attempts to renew certificates in the background
- **Graceful renewal**: Existing certificates continue to work until renewal succeeds
- **Renewal failure**: Caddy retries renewal according to exponential backoff

### Certificate Storage

Certificates are stored in Caddy's data directory:

```
/var/lib/caddy/
├── certificates/
│   └── acme-v02.api.letsencrypt.org/
│       └── example.com/
│           ├── example.com.crt
│           ├── example.com.key
│           └── example.com.json
```

### Certificate Metrics

You can monitor certificate issuance and renewal using Caddy's admin API:

```bash
# List all certificates
curl http://localhost:2019/certificates/

# Get certificate details
curl http://localhost:2019/certificates/example.com
```

## Troubleshooting

### Certificate Issuance Fails

**Symptom**: Caddy fails to obtain certificates, browser shows "Connection Not Secure"

**Possible Causes**:

1. **Port 80 not accessible**
   - Check firewall rules: `sudo iptables -L -n | grep 80`
   - Verify port is open: `curl -I http://your-domain.com`
   - Ensure no other service is blocking port 80

2. **DNS records incorrect**
   - Verify DNS A/AAAA record points to correct IP: `dig your-domain.com`
   - Check DNS propagation: `dig your-domain.com +trace`

3. **Gateway `/allowed` endpoint returning 403**
   - Check gateway logs for validation errors
   - Verify DNSLink record exists: `dig _dnslink.your-domain.com TXT`
   - Confirm website is active in internal API

4. **Rate limiting by Let's Encrypt**
   - Use staging environment for testing
   - Wait for rate limit to reset (usually 1 hour for failed validations)
   - Check rate limit status: https://letsencrypt.org/docs/rate-limits/

### ACME Challenge Fails

**Symptom**: Caddy logs show "challenge failed" or "validation failed"

**Solutions**:

1. **Check Caddy logs**:
   ```bash
   journalctl -u caddy -f
   # or
   caddy run --config /path/to/Caddyfile --adapter caddyfile
   ```

2. **Verify ACME challenge token is accessible**:
   ```bash
   # Caddy should serve the token at this path
   curl -I http://your-domain.com/.well-known/acme-challenge/
   ```

3. **Test with staging CA**:
   ```caddyfile
   {
       acme_ca https://acme-staging-v02.api.letsencrypt.org/directory
   }
   ```

4. **Check for reverse proxy interference**:
   - Ensure no reverse proxy blocks `/.well-known/acme-challenge/`
   - Verify Cloudflare (if used) is set to "DNS Only" mode during setup

### Gateway `/allowed` Endpoint Issues

**Symptom**: Caddy logs show "asking for certificate approval: 403"

**Solutions**:

1. **Verify gateway is running**:
   ```bash
   curl http://localhost:8080/healthz
   ```

2. **Test `/allowed` endpoint directly**:
   ```bash
   curl "http://localhost:8080/allowed?domain=example.com"
   ```

3. **Check gateway configuration**:
   - Verify `GATEWAY__API__URL` is set correctly
   - Verify `GATEWAY__API__SECRET` is valid
   - Check internal API connectivity

4. **Monitor status cache**:
   - The gateway caches validation results to prevent DoS
   - Cache TTL: 5 minutes (default)
   - Cache size: 1000 entries (default)

### Certificate Not Renewing

**Symptom**: Certificate expired and not renewed automatically

**Solutions**:

1. **Check Caddy logs for renewal errors**:
   ```bash
   journalctl -u caddy -f | grep -i renew
   ```

2. **Manually trigger renewal**:
   ```bash
   curl -X POST http://localhost:2019/certificates/reload
   ```

3. **Verify disk space**:
   ```bash
   df -h /var/lib/caddy
   ```

4. **Check certificate expiration**:
   ```bash
   openssl x509 -in /var/lib/caddy/certificates/acme-v02.api.letsencrypt.org/example.com/example.com.crt -noout -dates
   ```

### Performance Issues

**Symptom**: Slow certificate issuance or high latency

**Solutions**:

1. **Optimize DNS resolution**:
   - Use fast DNS resolvers (e.g., Cloudflare 1.1.1.1, Google 8.8.8.8)
   - Consider using DNS caching

2. **Adjust on_demand rate limits**:
   ```caddyfile
   on_demand_tls {
       ask http://localhost:8080/allowed
       interval 1m  # Reduce interval
       burst 10     # Increase burst
   }
   ```

3. **Enable certificate caching**:
   - Caddy caches certificates by default
   - Ensure Caddy has write access to data directory

4. **Monitor gateway performance**:
   - Check `/healthz` endpoint response time
   - Monitor status cache hit rate
   - Profile DNS validation performance

## Best Practices

### Security

1. **Use production CA only when ready**: Start with staging CA to avoid rate limits
2. **Implement rate limiting**: Prevent certificate abuse with `interval` and `burst` settings
3. **Monitor certificate issuance**: Set up alerts for failed validations
4. **Secure gateway endpoint**: Use internal network or authentication if accessible externally
5. **Keep Caddy updated**: Security patches and bug fixes

### Reliability

1. **Set up monitoring**: Monitor certificate expiration and renewal status
2. **Configure alerts**: Get notified for certificate issuance failures
3. **Test renewal process**: Manually renew certificates before expiration
4. **Backup certificates**: Export certificates for disaster recovery
5. **Document configuration**: Keep Caddyfile and gateway configuration in version control

### Performance

1. **Use DNS caching**: Reduce DNS lookup latency
2. **Adjust cache settings**: Tune gateway status cache for your workload
3. **Monitor resource usage**: Track CPU, memory, and disk I/O
4. **Load testing**: Test certificate issuance under load
5. **Optimize DNS resolution**: Use fast DNS resolvers

## Configuration Reference

### Caddyfile Directives

| Directive | Description | Default |
|-----------|-------------|---------|
| `acme_ca` | ACME CA endpoint URL | Let's Encrypt production |
| `email` | Email for certificate notices | None |
| `on_demand` | Enable On-Demand TLS | false |
| `ask` | Endpoint for domain validation | None |
| `interval` | Rate limit interval | 2m |
| `burst` | Rate limit burst size | 5 |

### Gateway Environment Variables

| Variable | Description | Required |
|----------|-------------|----------|
| `GATEWAY__API__URL` | Internal API base URL | Yes |
| `GATEWAY__API__SECRET` | Gateway secret for API authentication | Yes |
| `GATEWAY__SERVER__PORT` | Gateway HTTP server port | 8080 (default) |

### Gateway Cache Settings

| Variable | Description | Default |
|----------|-------------|---------|
| `GATEWAY__CACHE__STATUS_CACHE_TTL` | Status cache TTL | 5m |
| `GATEWAY__CACHE__STATUS_CACHE_LRU_SIZE` | Status cache size | 1000 |

## Additional Resources

- [Caddy Documentation](https://caddyserver.com/docs/)
- [Caddy TLS Management](https://caddyserver.com/docs/automatic-https)
- [Caddy On-Demand TLS](https://caddyserver.com/docs/caddyfile/options#on-demand-tls)
- [Let's Encrypt Documentation](https://letsencrypt.org/docs/)
- [ACME Protocol](https://datatracker.ietf.org/doc/html/rfc8555)
- [Gateway Architecture](../AGENTS.md#ssl-certificate-generation)

## Support

For issues with:
- **Caddy configuration**: Check Caddy logs and documentation
- **Gateway `/allowed` endpoint**: Check gateway logs and AGENTS.md
- **DNSLink validation**: Verify DNS records with `dig _dnslink.{domain} TXT`
- **ACME challenges**: Ensure port 80 is accessible from the internet
