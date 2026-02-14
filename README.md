# dead-mans-switch

![Build Status](https://github.com/circa10a/dead-mans-switch/workflows/deploy/badge.svg)
![GitHub release (latest by date)](https://img.shields.io/github/v/release/circa10a/dead-mans-switch)

<img width="45%" src="" align="right" style="margin-left: 20px"/>

**Dead Man's Switch** is an all one self-hosted app for creating a "Dead Man's Switch". Create switch via the UI, API, or CLI that sends a message via many providers if you don't "check-in" by resetting your timer(s).

## Table of Contents

- [Quick Start](#quick-start)
- [Features](#features)
- [Guides](#guides)
- [CLI Reference](#cli-reference)
- [Deployment](#deployment)
- [Development](#development)
- [License](#license)

## Quick Start

### Demo

Try the live demo at [insert demo URL here]

### Deploy

```bash
# Start the server with default settings (HTTP)
# http://localhost:8080
docker run -v -p 8080:8080 $PWD/dead-mans-switch-data:/data circa10a/dead-mans-switch

# Start with HTTPS (Let's Encrypt for automatic TLS)
# Requires domain pointing to host address
# http://myexamplesite.com
docker run -v -p 443:443 80:80 $PWD/dead-mans-switch-data:/data circa10a/dead-mans-switch \
  --auto-tls \
  --domains myexamplesite.com \
  --storage-dir /data

# Start with HTTPS (Custom Certs)
docker run -v -p 8443:8443 $PWD/certs:/certs $PWD/dead-mans-switch-data:/data circa10a/dead-mans-switch server \
  --auto-tls \
  --domains myexamplesite.com \
  --storage-dir /data \
  --tls-certificate /certs/cert.pem \
  --tls-key /certs/key.pem
```

> [!NOTE]
> HTTPS is required for push notifications.

> [!CAUTION]
> The `--storage-dir` directory contains your database and VAPID keys. Deleting or losing this directory will permanently destroy all switches and break existing push notification subscriptions. **Back it up.**

## Features

- **Multi-channel alerting** — Notify via push, email, webhook, and more. Never miss an expired switch. Powered by [Shoutrrr](https://shoutrrr.nickfedor.com/latest)
- **Push notifications** — Get real-time browser alerts on mobile or desktop when a switch expires, even if the tab is closed.
- **Zero-dependency deployment** — UI, CLI, and API ship as a single binary. No runtime dependencies, no sidecar services. Just run it.
- **Secure by default** — Automatic TLS via [CertMagic](https://github.com/caddyserver/certmagic), with optional [Authentik](https://goauthentik.io/) OIDC integration for multi-user setups.
- **Full observability** — Prometheus metrics and structured JSON logging

## Guides

- [Authentik Integration](guides/AUTHENTIK_INTEGRATION.md) — Set up OIDC authentication with Authentik

### API Documentation

Visit the interactive API documentation:

- **API Docs**: http://localhost:8080/v1/docs

## CLI Reference

### Install

```console
# curl (Linux/macOS)
curl -sSfL https://github.com/circa10a/dead-mans-switch/releases/latest/download/dead-mans-switch_$(uname -s)_$(uname -m).tar.gz | tar xz

# Go
go install github.com/circa10a/dead-mans-switch@latest
```

### Usage

```console
$ dead-mans-switch
Manage Dead Man's Switches

Usage:
  dead-mans-switch [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  help        Help about any command
  server      Start the dead-mans-switch server
  switch      Manage dead man switches
  version     Print the version information

Flags:
  -h, --help      help for dead-mans-switch
  -v, --version   version for dead-mans-switch

Use "dead-mans-switch [command] --help" for more information about a command.
```

### Server Command

```
$ dead-mans-switch server -h
Start the dead-mans-switch server

Usage:
  dead-mans-switch server [flags]

Flags:
      --auth-audience string           Expected JWT audience claim. (env: DEAD_MANS_SWITCH_AUTH_AUDIENCE)
      --auth-enabled                   Enable JWT authentication via Authentik. (env: DEAD_MANS_SWITCH_AUTH_ENABLED)
      --auth-issuer-url string         Identity provider OAuth2 issuer URL. (env: DEAD_MANS_SWITCH_AUTH_ISSUER_URL)
  -a, --auto-tls                       Enable automatic TLS via Let's Encrypt. Requires port 80/443 open to the internet for domain validation. (env: DEAD_MANS_SWITCH_AUTO_TLS)
      --contact-email string           Email used for TLS cert registration + push notification point of contact (not required). (env: DEAD_MANS_SWITCH_CONTACT_EMAIL) (default "user@dead-mans-switch.com")
      --demo-mode                      Enable demo mode which creates sample switches on startup and resets the database periodically. (env: DEAD_MANS_SWITCH_DEMO_MODE)
      --demo-reset-interval duration   How often to reset the database with fresh sample switches when in demo mode. (env: DEAD_MANS_SWITCH_DEMO_RESET_INTERVAL) (default 6h0m0s)
  -d, --domains stringArray            Domains to issue certificate for. Must be used with --auto-tls. (env: DEAD_MANS_SWITCH_DOMAINS)
  -h, --help                           help for server
  -f, --log-format string              Server logging format. Supported values are 'text' and 'json'. (env: DEAD_MANS_SWITCH_LOG_FORMAT) (default "text")
  -l, --log-level string               Server logging level. (env: DEAD_MANS_SWITCH_LOG_LEVEL) (default "info")
  -m, --metrics                        Enable Prometheus metrics instrumentation. (env: DEAD_MANS_SWITCH_METRICS)
  -p, --port int                       Port to listen on. Cannot be used in conjunction with --auto-tls since that will require listening on 80 and 443. (env: DEAD_MANS_SWITCH_PORT) (default 8080)
  -s, --storage-dir string             Storage directory for database (env: DEAD_MANS_SWITCH_STORAGE_DIR) (default "./data")
      --tls-certificate string         Path to custom TLS certificate. Cannot be used with --auto-tls. (env: DEAD_MANS_SWITCH_TLS_CERTIFICATE)
      --tls-key string                 Path to custom TLS key. Cannot be used with --auto-tls. (env: DEAD_MANS_SWITCH_TLS_KEY)
      --worker-batch-size int          How many notification records to process at a time. (env: DEAD_MANS_SWITCH_WORKER_BATCH_SIZE) (default 1000)
      --worker-interval duration       How often to check for expired switches. (env: DEAD_MANS_SWITCH_WORKER_INTERVAL) (default 5m0s)
```

## Deployment

### Docker Compose

Run the service with integrated monitoring and observability stack:

```bash
make docker-compose
```

This starts:
- **API Server**: http://localhost:8080
- **Grafana** (dashboards): http://localhost:3000
- **Prometheus** (metrics): http://localhost:9090
- **Loki** (logs)
- **Promtail** (log shipper)

### Kubernetes

Deploy to Kubernetes using the provided manifests:

```bash
# Apply the manifests from the repository
kubectl apply -f https://raw.githubusercontent.com/circa10a/dead-mans-switch/main/deploy/k8s/

# Verify deployment
kubectl get pods -l app=dead-mans-switch
```

The service will be available based on your Kubernetes configuration.

For local development with [Tilt](https://tilt.dev/):

```bash
make k8s
```

See [deploy/k8s](deploy/k8s) for the manifest files.

## Development

> [!IMPORTANT]
> Most `make` targets require [Docker](https://docs.docker.com/engine/install/) to be installed.

### Start Local Server

```bash
make run
```

```
2024-10-26T19:09:03-07:00 INFO <server/server.go:118> Starting server on :8080 component=server
```

### Run Tests

```bash
make test
```

### Run local authentik stack

```console
# Start
make auth

# Stop
make auth-down
```

### Run Prometheus/Grafana/Loki stack

```console
$ make monitoring
````

## License

[See LICENSE file](LICENSE)
