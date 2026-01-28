# 💤 dead-man-switch-api

A project template for Go REST API's.

![Build Status](https://github.com/circa10a/dead-man-switch-api/workflows/deploy/badge.svg)
![GitHub release (latest by date)](https://img.shields.io/github/v/release/circa10a/dead-man-switch-api)

<img width="40%" src="https://raw.githubusercontent.com/ashleymcnamara/gophers/refs/heads/master/CouchPotatoGopher.png" align="right"/>

- [dead-man-switch-api](#dead-man-switch-api)
  - [Features](#features)
  - [Usage](#usage)
    - [Repository setup](#repository-setup)
    - [Initialize a new project](#initialize-a-new-project)
    - [Server](#server)
  - [Development](#development)
    - [Start the server](#start-the-server)
    - [Default Routes](#default-routes)
    - [Adding routes](#adding-routes)
    - [Generate OpenAPI docs](#generate-openapi-documentation)
    - [Generate Client SDK](#generate-client-sdk)
- [Docker compose](#docker-compose)
- [Kubernetes](#kubernetes)

## Features

- :lock: Secure by default with automatic TLS powered by [CertMagic](https://github.com/caddyserver/certmagic)
- :chart_with_upwards_trend: Prometheus metrics
- :scroll: Beautiful logging via [charmbracelet](https://github.com/charmbracelet/log)
- :book: OpenAPI documentation built in with SDK generation
- Easy kubernetes development via [Tilt](https://github.com/tilt-dev/tilt)
- A full local monitoring stack using [Grafana](https://grafana.com/), [Prometheus](https://prometheus.io/), and [Loki](https://grafana.com/oss/loki/) via Docker compose.
- :construction_worker: CI pipelines via GitHub actions
  - Tests
  - Linting via [golangci-lint](https://github.com/golangci/golangci-lint)
  - Security scanning via [gosec](https://github.com/securego/gosec)
  - Secret scanning via [gitleaks](https://github.com/gitleaks/gitleaks)
  - Automatic semantic version tagging
  - [GoReleaser](https://github.com/goreleaser/goreleaser)
  - Docker build and pushes for latest and tagged versions

## Usage

### Repository setup

The default GitHub actions that come with this project has 1 setup requirement.

1. `DOCKERHUB_TOKEN` - Login to [Docker Hub](https://hub.docker.com/) and [create a personal access token](https://docs.docker.com/security/for-developers/access-tokens/) with `Read, Write` scope to push docker images under your account.

> [!WARNING]
> This assumes your Docker Hub namespace matches your git repository namespace.
> Example: github.com/mynamespace/myrepo will push to mynamespace/myrepo on Docker Hub

1. Enable dependabot updates - Navigate to repository > Security > Code security > Grouped security updates > Enabled

### Initialize a new project

Use [gonew](https://pkg.go.dev/golang.org/x/tools/cmd/gonew) to initialize a new project from this template:

```console
# Install gonew (if needed)
$ go install golang.org/x/tools/cmd/gonew@latest

# Init project
$ gonew github.com/circa10a/dead-man-switch-api github.com/namespace/project
```

Finally, replace all of the existing references of the template repository with your newly created one by running:

```console
$ make template
```

### Overview

```console
$ go run .
A template project for Go REST API's

Usage:
  dead-man-switch-api [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  help        Help about any command
  server      Start the dead-man-switch-api server

Flags:
  -h, --help   help for dead-man-switch-api

Use "dead-man-switch-api [command] --help" for more information about a command.
```

## Server

```console
$ go run . server --help

Start the dead-man-switch-api server

Usage:
  dead-man-switch-api server [flags]

Flags:
  -a, --auto-tls                 Enable automatic TLS via Let's Encrypt. Requires port 80/443 open to the internet for domain validation. (env: APP_AUTO_TLS)
  -d, --domains stringArray      Domains to issue certificate for. Must be used with --auto-tls. (env: APP_DOMAINS)
  -h, --help                     help for server
  -f, --log-format string        Server logging format. Supported values are 'text' and 'json'. (env: APP_LOG_FORMAT) (default "text")
  -l, --log-level string         Server logging level. (env: APP_LOG_LEVEL) (default "info")
  -m, --metrics                  Enable Prometheus metrics intrumentation. (env: APP_METRICS)
  -p, --port int                 Port to listen on. Cannot be used in conjunction with --auto-tls since that will require listening on 80 and 443. (env: APP_PORT) (default 8080)
      --tls-certificate string   Path to custom TLS certificate. Cannot be used with --auto-tls. (env: APP_TLS_CERTIFICATE)
      --tls-key string           Path to custom TLS key. Cannot be used with --auto-tls. (env: APP_TLS_KEY)
```

## Development

> [!IMPORTANT]
> Most `make` targets expect [Docker](https://docs.docker.com/engine/install/) to be installed.

### Start the server

```console
$ make run
2024-10-26T19:09:03-07:00 INFO <server/server.go:118> Starting server on :8080 component=server
```

### Default routes

|                            |                                                     |
|----------------------------|-----------------------------------------------------|
| Endpoint                   | Descripton                                          |
| `localhost:8080/v1/docs`   | OpenAPI documentation                               |
| `localhost:8080/v1/health` | Health status                                       |
| `localhost:8080/metrics`   | Prometheus metrics (if server is started with `-m`) |

### Adding routes

Routes are created in `internal/server/server.go` with API handlers in `internal/server/handlers/handlers.go`.

### Generate OpenAPI documentation

Simply add your API spec to `api/openapi.yaml` then run `make docs`. The OpenAPI documentation will then be embed in the application and will be accessible at the `http://localhost:8080/docs` endpoint.

### Generate Client SDK

Once your API documention is added to `api/openapi.yaml`, just run `make sdk` and a generated client sdk will be output to the `api` package.

## Docker compose

The local docker compose stack sets up a full observability stack for testing. Run the following the start the stack:

```console
$ make docker-compose
```

The following services will then be accessible with a pre-configured dashboard:

- Go server: http://localhost:8080
- [Grafana](https://grafana.com/): http://localhost:3000
- [Prometheus](https://prometheus.io/): http://localhost:9090
- [Loki](https://grafana.com/oss/loki/)
- [Promtail](https://grafana.com/docs/loki/latest/send-data/promtail/)

## Kubernetes

> [!NOTE]
> Requires [Tilt](https://tilt.dev/) to be installed and local kubernetes context to be configured.
>
> This has only been tested on MacOS using Docker for Mac's Kubernetes integration.

```console
$ make k8s
```

This will deploy the service to kubernetes and make it available at http://localhost:8080
# dead-mans-switch
