# --- 1. METADATA & VERSIONING ---
PROJECT   := $(shell grep module go.mod | rev | cut -d'/' -f1-2 | rev)
NAMESPACE := $(shell echo $(PROJECT) | cut -d'/' -f1)
REPO      := $(shell echo $(PROJECT) | cut -d'/' -f2)
MODULE    := $(shell go list -m)

VERSION ?= dev
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

ifneq ($(GITHUB_REF_NAME),)
VERSION=$(GITHUB_REF_NAME)
endif

GO_BUILD_FLAGS := -ldflags="-s -w -X '$(MODULE)/cmd.version=$(VERSION)' -X '$(MODULE)/cmd.commit=$(COMMIT)' -X '$(MODULE)/cmd.date=$(DATE)'"

# --- 2. PATHS & FILES ---
OPENAPI_SPEC  := api/openapi.yaml
GEN_GO_FILE   := api/gen.go
INTERNAL_YAML := api/openapi.internal.gen.yaml
EXTERNAL_YAML := api/openapi.external.gen.yaml
INTERNAL_HTML := internal/server/docs/api.internal.html
EXTERNAL_HTML := internal/server/docs/api.public.html

WEB_DIR       := internal/server/web
STATIC_DIR    := $(WEB_DIR)/static
CSS_OUT       := $(STATIC_DIR)/css/tailwind.css
JS_OUT        := $(STATIC_DIR)/js/alpine.min.js
INPUT_CSS     := $(WEB_DIR)/input.css
HTML_FILES    := $(shell find $(WEB_DIR) -name "*.html")

.PHONY: all build assets docs sdk clean
all: build

# Build API generated code, web assets(css/js), API docs
build: $(GEN_GO_FILE) assets docs
	@echo "==> Building binary..."
	@go build $(GO_BUILD_FLAGS) -o bin/$(REPO) .

# Build tailwind CSS/fetch alpine JS
assets: $(CSS_OUT) $(JS_OUT)

# Render API docs
docs: $(INTERNAL_HTML) $(EXTERNAL_HTML)

# Build API generated code
sdk: $(GEN_GO_FILE)

# API generated code + docs
$(GEN_GO_FILE) $(INTERNAL_YAML) $(EXTERNAL_YAML): $(OPENAPI_SPEC)
	@echo "==> Spec updated. Running go generate..."
	@go generate ./...
	@touch $(GEN_GO_FILE) $(INTERNAL_YAML) $(EXTERNAL_YAML)

# Tailwind Compilation
$(CSS_OUT): $(INPUT_CSS) $(HTML_FILES)
	@echo "==> Building & Compressing Tailwind CSS..."
	@mkdir -p $(STATIC_DIR)/css
	@docker run --rm -v $$PWD:/src -w /src node:slim \
		sh -c "npx --yes tailwindcss@3 -i $(INPUT_CSS) -o $(CSS_OUT) --config $(WEB_DIR)/tailwind.config.js --minify"

# AlpineJS Bundle
$(JS_OUT):
	@echo "==> Downloading AlpineJS..."
	@mkdir -p $(STATIC_DIR)/js
	@curl -sL https://unpkg.com/alpinejs@3.x.x/dist/cdn.min.js -o $(JS_OUT)

# API Doc (internal, no omitted fields)
$(INTERNAL_HTML): $(INTERNAL_YAML)
	$(call render_docs,dead-mans-switch-internal@v1,$<,$@)

# API Doc (external, omitted fields)
$(EXTERNAL_HTML): $(EXTERNAL_YAML)
	$(call render_docs,dead-mans-switch-external@v1,$<,$@)

.PHONY: run live test lint docker

# Run the server
run: build
	./bin/$(REPO) server

# Tests ofc
test: $(GEN_GO_FILE)
	@go test -v -coverprofile=coverage.out ./...
	@grep -v -E "api/gen.go" coverage.out > coverage.cleaned.out
	@mv coverage.cleaned.out coverage.out
	@go tool cover -func=coverage.out

# Lint ofc
lint:
	@docker run --rm -v $$PWD:/tmp/project -w /tmp/project golangci/golangci-lint golangci-lint run -v

# Build docker image
docker:
	@docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) --build-arg DATE=$(DATE) -t $(PROJECT) .

# Function for rendering docs
define render_docs
    @echo "==> Building $(1)..."
    @docker run --rm \
      -v $$PWD:/tmp/project \
      -w /tmp/project \
      -e NODE_NO_WARNINGS=1 \
      -e npm_config_loglevel=error \
      node \
      sh -c "npx --yes --quiet @redocly/cli bundle $(1) --output $(2) && \
             npx --yes --quiet @redocly/cli build-docs $(2) --output $(3)"
endef

clean:
	@rm -rf bin/ coverage.out