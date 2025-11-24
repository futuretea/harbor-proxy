# Harbor Proxy

A stateless reverse proxy for Harbor registry that supports host-based repository prefixing, strict host:port routing, and safe redirect/header rewriting.

## Key Features
- Exact host:port matching for routing (no partial matches)
- Repository prefix rewriting under `/v2/<repo>/...`
- Location and Www-Authenticate header rewriting to client host
- HTTPS usage aligned with backend target configuration
- Health, readiness, and Prometheus metrics endpoints
- Graceful shutdown for long-lived blob transfers

## Quick Start
Run locally (HTTP on :8080, HTTPS backend):

```
harbor-proxy \
  --target https://harbor.example.com \
  --listen :8080 \
  --map "registry-dev.example.com=dev_,registry-prod.example.com=prod_" \
  --log-level debug
```

Enable TLS for the proxy (serves HTTPS on :8080):

```
harbor-proxy \
  --target https://harbor.example.com \
  --listen :8080 \
  --tls-enabled \
  --tls-cert-file /tmp/server.crt \
  --tls-key-file /tmp/server.key \
  --map "registry.example.com:8080=tenant_a_"
```

## Configuration
Flags:
- `--target` Harbor external URL (e.g., `https://harbor.example.com`)
- `--listen` Proxy listen address (default `:8080`)
- `--map` Host→prefix map, comma-separated (exact host:port keys)
- `--tls-insecure` Skip backend TLS verification (default true)
- `--tls-enabled` Serve HTTPS
- `--tls-cert-file`, `--tls-key-file` TLS files for proxy
- `--pprof-listen` Separate HTTP port for pprof (e.g., `:6060`)
- `--log-level` `trace|debug|info|warn|error|fatal|panic` (default `info`)

Environment variables (with `HARBOR_PROXY_` prefix) mirror flags, e.g.:
- `HARBOR_PROXY_TARGET`, `HARBOR_PROXY_LISTEN`, `HARBOR_PROXY_HOST_PREFIX_MAP`, `HARBOR_PROXY_TLS_ENABLED`, `HARBOR_PROXY_TLS_CERT_FILE`, `HARBOR_PROXY_TLS_KEY_FILE`, `HARBOR_PROXY_LOG_LEVEL`

## Endpoints
- `/v2/` Registry API ping
- `/metrics` Prometheus metrics
- `/healthz` Liveness
- `/readyz` Readiness (flips to false during graceful shutdown)

## Docker
Build and run with the provided multi-stage Dockerfile:

```
docker build -t harbor-proxy .
docker run --rm -p 8080:8080 \
  -v /tmp/server.crt:/tmp/server.crt -v /tmp/server.key:/tmp/server.key \
  harbor-proxy \
  --target https://harbor.example.com \
  --tls-enabled --tls-cert-file /tmp/server.crt --tls-key-file /tmp/server.key \
  --map "registry.example.com=tenant_"
```

## Load Balancer Notes
- Preserve the original `Host` header including port; routing keys must match exactly (e.g., `registry.example.com:8080`).
- The proxy always sets backend `Host` to the configured target; response headers are rewritten back to the client host.

## Multi-Replica
- The proxy is stateless and supports horizontal scaling.
- Use `/readyz` for draining: on SIGTERM, readiness becomes false, and in-flight transfers are allowed to complete.
