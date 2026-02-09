PROJECT=$(shell grep module go.mod | rev | cut -d'/' -f1-2 | rev)
NAMESPACE=$(shell echo $(PROJECT) | cut -d'/' -f1)
REPO=$(shell echo $(PROJECT) | cut -d'/' -f2)

# Module path from go.mod used for ldflags -X assignments
MODULE=$(shell go list -m)

# If running in CI (GitHub Actions), use GITHUB_REF_NAME which contains the tag or branch name
TAG=$(GITHUB_REF_NAME)

# Default version, commit and date (can be overridden via env or in build targets)
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE   ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

ifneq ($(TAG),)
VERSION=$(TAG)
endif

# GO_BUILD_FLAGS injects build-time version info into the binary.
GO_BUILD_FLAGS=-ldflags="-s -w -X '$(MODULE)/cmd.version=$(VERSION)' -X '$(MODULE)/cmd.commit=$(COMMIT)' -X '$(MODULE)/cmd.date=$(DATE)'"

build:
	@go build $(GO_BUILD_FLAGS) .

docker:
	@docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg DATE=$(DATE) \
		-t $(PROJECT) .

docker-compose:
	@docker compose -f ./deploy/docker-compose/docker-compose.yaml up


# Define the Docker runner macro
# $(1): Redocly Alias (e.g., dead-mans-switch@v1)
# $(2): Intermediate YAML path
# $(3): Final HTML output path
define render_docs
	@docker run --rm \
	  -v $$PWD:/tmp/project \
	  -w /tmp/project \
	  -e NODE_NO_WARNINGS=1 \
	  -e npm_config_loglevel=error \
	  node \
	  sh -c "npx --yes --quiet @redocly/cli bundle $(1) --output $(2) && \
	         npx --yes --quiet @redocly/cli build-docs $(2) --output $(3)"
endef

INTERNAL_SPEC = api/openapi.internal.gen.yaml
EXTERNAL_SPEC = api/openapi.external.gen.yaml

docs:
	# Internal Group: /api/v1/docs/internal
	$(call render_docs,dead-mans-switch-internal@v1,$(INTERNAL_SPEC),internal/server/docs/api.internal.html)

	# External Group: /api/v1/docs/public
	$(call render_docs,dead-mans-switch-external@v1,$(EXTERNAL_SPEC),internal/server/docs/api.public.html)

k8s:
	@tilt up --stream=true

lint:
	@docker run --rm \
	  -v $$PWD:/tmp/project \
	  -w /tmp/project \
	  golangci/golangci-lint \
	  golangci-lint run -v

run:
	@go run . server

live:
	@air server

sdk:
	@go generate ./...

sure-docs-are-updated:
	@bash -c 'if [[ $$(git status --porcelain) ]]; then \
		echo "API docs not updated. Run make docs"; \
		exit 1; \
	fi'

test:
	@go test -v -coverprofile=coverage.out ./...
	@# Filter out generated files from the profile
	@grep -v -E "api/gen.go" coverage.out > coverage.cleaned.out
	@mv coverage.cleaned.out coverage.out
	@go tool cover -func=coverage.out

coverage: test
	@go tool cover -html=coverage.out
