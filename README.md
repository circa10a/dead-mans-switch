# ☠️ dead-mans-switch

**Dead Man's Switch** is an all one self-hosted app for creating a "Dead Man's Switch". Create switch via the UI, API, or CLI that sends a message via many providers if you don't "check-in" by resetting your timer(s).

![Build Status](https://github.com/circa10a/dead-mans-switch/workflows/deploy/badge.svg)
![GitHub release (latest by date)](https://img.shields.io/github/v/release/circa10a/dead-mans-switch)

<img width="35%" height="35%" src="docs/images/grim_gopher.png" align="right"/>

## Table of Contents

### Try out a [demo version here](https://deadmanswitch.shop/) 👈

- [Features](#features)
- [Quick Start](#quick-start)
- [Install as a PWA](#install-as-a-pwa)
- [Configuration](#configuration)
- [Guides](#guides)
- [CLI Reference](#cli-reference)
- [Deployment](#deployment)
- [Development](#development)
- [License](#license)

## Background

I have several personal use cases for this so I made this to run for myself. Maybe you'll like it too.

## Features

- **Multi-channel alerting** — Notify via push, email, webhook, and more. Never miss an expired switch. Powered by [Shoutrrr](https://caleblemoine.dev/shoutrrr/latest)
- **Push notifications** — Check in via real-time push notifications on mobile or desktop before a switch expires as your chosen threshold.
- **Zero-dependency deployment** — UI, CLI, and API ship as a single binary. No runtime dependencies, no sidecar services. Just run it.
- **Secure** — Automatic TLS via [CertMagic](https://github.com/caddyserver/certmagic), with optional [Authentik](https://goauthentik.io/) OIDC integration for multi-user setups, optional encryption per switch.
- **Full observability** — Prometheus metrics and structured JSON logging

## Quick Start

### Demo

<div align="center">
  <img src="docs/images/demo.gif" alt="Dead Man's Switch Mobile Demo" width="300"/>
  <p><em>Mobile App view — creating a switch, checking in, and receiving push notifications</em></p>
</div>

### Deploy

```bash
# Start the server with default settings (HTTP)
# http://localhost:8080
docker run -v $PWD/dead-mans-switch-data:/data -p 8080:8080 circa10a/dead-mans-switch

# Start with HTTPS (Let's Encrypt for automatic TLS)
# Requires domain pointing to host address
# http://myexamplesite.com
docker run -v $PWD/dead-mans-switch-data:/data -p 443:443 -p 80:80 circa10a/dead-mans-switch \
  server \
  --auto-tls \
  --domains myexamplesite.com \
  --data-dir /data

# Start with HTTPS (Custom Certs)
docker run -v $PWD/certs:/certs $PWD/dead-mans-switch-data:/data -p 8443:8443 circa10a/dead-mans-switch \
  server
  --auto-tls \
  --domains myexamplesite.com \
  --data-dir /data \
  --tls-certificate /certs/cert.pem \
  --tls-key /certs/key.pem
```

> [!NOTE]
> HTTPS is required for push notifications.

> [!CAUTION]
> The `--data-dir` directory contains your database and VAPID keys. Deleting or losing this directory will permanently destroy all switches and break existing push notification subscriptions. **Back it up.**

## Install as a PWA

Dead Man's Switch can be installed as a Progressive Web App (PWA) for a native app-like experience on your phone or desktop.

### iOS (Safari)

1. Open your Dead Man's Switch instance in **Safari**
2. Tap the **Share** button (square with an arrow)
3. Scroll down and tap **Add to Home Screen**
4. Tap **Add**

### Android (Chrome)

1. Open your Dead Man's Switch instance in **Chrome**
2. Tap the **three-dot menu** (⋮)
3. Tap **Add to Home Screen** (or **Install app**)
4. Tap **Install**

### Desktop (Chrome / Edge)

1. Open your Dead Man's Switch instance in **Chrome** or **Edge**
2. Click the **install icon** (⊕) in the address bar, or go to **Menu -> Install Dead Man's Switch**
3. Click **Install**

> [!NOTE]
> HTTPS is required for PWA installation. Use `--auto-tls` or provide custom certificates.

## Configuration

The server can be configured via **CLI flags**, **environment variables**, or a **YAML config file**. When multiple sources set the same value, the precedence order is:

**CLI flags > Environment variables > Config file > Defaults**

### Config File

Place a `dead-mans-switch.yaml` file in the current working directory or your home directory and it will be loaded automatically. You can also specify a path explicitly:

```bash
dead-mans-switch server --config /path/to/config.yaml
```

Example config (see [`dead-mans-switch.example.yaml`](dead-mans-switch.example.yaml) for all options):

```yaml
port: 8080
log-level: info
log-format: json
data-dir: ./data
metrics: true

auth-enabled: true
auth-issuer-url: https://auth.example.com/application/o/dead-mans-switch/
auth-audience: my-client-id

worker-interval: 1m
worker-batch-size: 1000
```

Config keys match CLI flag names (hyphenated). Every flag also has a corresponding environment variable with the `DEAD_MANS_SWITCH_` prefix (e.g. `DEAD_MANS_SWITCH_PORT`).

## Guides

- [Authentik Integration](docs.guides/AUTHENTIK_INTEGRATION.md) — Set up OIDC authentication with Authentik
- [Tailscale + MagicDNS HTTPS](docs.guides/TAILSCALE_INTEGRATION.md) — Private HTTPS via Tailscale without exposing to the internet

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
      --auth-enabled                   Enable JWT authentication via OIDC. (env: DEAD_MANS_SWITCH_AUTH_ENABLED)
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
  -s, --data-dir string               Data directory for database and keys (env: DEAD_MANS_SWITCH_DATA_DIR) (default "./data")
      --tls-certificate string         Path to custom TLS certificate. Cannot be used with --auto-tls. (env: DEAD_MANS_SWITCH_TLS_CERTIFICATE)
      --tls-key string                 Path to custom TLS key. Cannot be used with --auto-tls. (env: DEAD_MANS_SWITCH_TLS_KEY)
      --worker-batch-size int          How many notification records to process at a time. (env: DEAD_MANS_SWITCH_WORKER_BATCH_SIZE) (default 1000)
      --worker-interval duration       How often to check for expired switches. (env: DEAD_MANS_SWITCH_WORKER_INTERVAL) (default 5m0s)
```

### Switch Command

```
$ dead-mans-switch switch -h
Manage dead man switches

Usage:
  dead-mans-switch switch [command]

Available Commands:
  create      Create a new dead man switch
  delete      Delete a dead man switch
  disable     Disable a dead man switch
  get         Get all switches or a specific one by ID
  reset       Reset a dead man switch timer
  update      Update an existing dead man switch

Flags:
      --color           Enable colorized output (default true)
  -h, --help            help for switch
  -o, --output string   Output format (json, yaml) (default "json")
  -u, --url string      API base URL (default "http://localhost:8080/api/v1")

Global Flags:
      --config string   Config file (default: ./dead-mans-switch.yaml or ~/dead-mans-switch.yaml)

Use "dead-mans-switch switch [command] --help" for more information about a command.
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

Deploy to Kubernetes using the provided manifest:

```bash
# Apply the manifest
kubectl apply -f https://raw.githubusercontent.com/circa10a/dead-mans-switch/main/deploy/k8s/manifest.yaml

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

### Run local Authentik stack

```console
# Start
make auth

# Stop
make auth-down
```

### Run local Prometheus/Grafana/Loki stack

```console
make monitoring
````

## License

[See LICENSE file](LICENSE)
