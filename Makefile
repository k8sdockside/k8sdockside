# Makefile for k8sdockside
# inspired by kubebuilder.io

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

# Basic colors
BLACK=\033[0;30m
RED=\033[0;31m
GREEN=\033[0;32m
YELLOW=\033[0;33m
BLUE=\033[0;34m
PURPLE=\033[0;35m
CYAN=\033[0;36m
WHITE=\033[0;37m

# Text formatting
BOLD=\033[1m
UNDERLINE=\033[4m
RESET=\033[0m

APP_NAME ?= k8sdockside
FRONTEND_DIR ?= frontend

## Location to install tool dependencies to. Kept under bin/ (already gitignored),
## but in its own subdir so `make clean` can drop build output without nuking tools.
LOCALBIN ?= $(shell pwd)/bin/tools
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

GOLANGCI_LINT = $(LOCALBIN)/golangci-lint
GOSEC ?= $(LOCALBIN)/gosec
GOVULNCHECK ?= $(LOCALBIN)/govulncheck

# Use the Go toolchain version declared in go.mod when building tools
GO_VERSION := $(shell awk '/^go /{print $$2}' go.mod)
GO_TOOLCHAIN := go$(GO_VERSION)
GOSEC_VERSION ?= latest
GOLANGCI_LINT_VERSION ?= latest
GOVULNCHECK_VERSION ?= latest

# Keep the wails3 CLI on the exact version this module depends on. Lazily
# evaluated so it only runs when a wails target is actually invoked.
WAILS_VERSION = $(shell go list -m -f '{{.Version}}' github.com/wailsapp/wails/v3)

# build/ios is `package main` whose main() sits behind `//go:build ios`, so a
# plain `go build ./...` fails to link it on Linux. Filter it out.
GO_PKGS = $(shell go list ./... | grep -v '/build/ios')

# The same list as the server build sees it. Listed with the tag, because a
# package whose files are all `//go:build server` does not exist as far as a
# plain `go list` is concerned -- GO_PKGS would quietly leave it out.
GO_PKGS_SERVER = $(shell go list -tags server ./... | grep -v '/build/ios')

##@ Help
.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-24s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Build
.PHONY: dev
dev: ## Run the app in development mode with hot reload (wails3 dev).
	@printf "$(CYAN)Starting wails3 dev...$(RESET)\n"
	@wails3 dev

.PHONY: build
build: ## Build the production desktop app into bin/ (wails3 build).
	@printf "$(CYAN)Building $(APP_NAME)...$(RESET)\n"
	@wails3 build
	@printf "$(GREEN)✓ Build complete: $(BOLD)bin/$(APP_NAME)$(RESET)\n"

.PHONY: build-go
build-go: frontend-dist-stub ## Compile the Go packages only (skips build/ios, no frontend build).
	@printf "$(CYAN)Building Go packages...$(RESET)\n"
	@go build $(GO_PKGS)
	@printf "$(GREEN)✓ Go build complete$(RESET)\n"

.PHONY: build-frontend
build-frontend: ## Build the frontend into frontend/dist.
	@printf "$(CYAN)Building frontend...$(RESET)\n"
	@cd $(FRONTEND_DIR) && npm run build
	@printf "$(GREEN)✓ Frontend built: $(BOLD)$(FRONTEND_DIR)/dist$(RESET)\n"

# main.go embeds frontend/dist, so the root package does not even type-check
# until that directory holds at least one file -- which breaks `go build`,
# `go test`, golangci-lint, gosec and govulncheck alike. dist is generated and
# gitignored, and the CI Go jobs deliberately skip Node to avoid paying for a
# frontend bundle they never look at, so stand in a placeholder instead. The
# `all:` prefix on the embed pattern is what makes a dotfile enough to satisfy
# it. A real `make build-frontend` overwrites this with the actual bundle.
.PHONY: frontend-dist-stub
frontend-dist-stub: ## Ensure frontend/dist exists so the Go embed resolves.
	@mkdir -p $(FRONTEND_DIR)/dist
	@[ -n "$$(ls -A $(FRONTEND_DIR)/dist 2>/dev/null)" ] || \
		printf 'placeholder so //go:embed all:frontend/dist resolves; see make build-frontend\n' \
			> $(FRONTEND_DIR)/dist/.embed-placeholder

.PHONY: generate
generate: ## Regenerate the TypeScript bindings from the Go services.
	@printf "$(CYAN)Generating bindings...$(RESET)\n"
	# -i must match build/Taskfile.yml's generate:bindings, which reruns on every
	# build. Without it this target emits model classes and non-null slices, and
	# the next build silently replaces them with interfaces and nullable ones.
	@wails3 generate bindings -clean=true -ts -i
	@printf "$(GREEN)✓ Bindings generated$(RESET)\n"

.PHONY: clean
clean: clean-frontend ## Clean build artifacts, caches and frontend output.
	@printf "$(YELLOW)Cleaning build artifacts...$(RESET)\n"
	@rm -rf bin/$(APP_NAME) bin/$(APP_NAME).exe bin/$(APP_NAME)-server bin/helm-template .task
	@rm -f coverage.out coverage.html bench.cpu bench.mem
	@go clean -testcache
	@printf "$(GREEN)✓ Clean complete$(RESET)\n"

.PHONY: clean-frontend
clean-frontend: ## Remove frontend/dist, node_modules and the vite cache.
	@printf "$(YELLOW)Cleaning frontend...$(RESET)\n"
	@rm -rf $(FRONTEND_DIR)/dist $(FRONTEND_DIR)/node_modules $(FRONTEND_DIR)/.vite $(FRONTEND_DIR)/node_modules/.vite
	@printf "$(GREEN)✓ Frontend cleaned$(RESET)\n"

.PHONY: clean-tools
clean-tools: ## Remove the locally installed tools in bin/tools.
	@printf "$(YELLOW)Removing $(LOCALBIN)...$(RESET)\n"
	@rm -rf $(LOCALBIN)
	@printf "$(GREEN)✓ Tools removed$(RESET)\n"

##@ Server
# Server mode: the same app built with `-tags server`, serving its UI over HTTP
# behind its own sign-in, for running in Kubernetes. See docs/server-mode.md.
# The image cannot build the frontend itself -- the bindings come from the
# wails3 CLI, which needs the GTK headers -- so the image targets build bindings
# and the bundle here first, and the Dockerfile copies frontend/dist in.

IMG ?= ghcr.io/k8sdockside/k8sdockside:dev
PLATFORMS ?= linux/amd64,linux/arm64
KIND_CLUSTER ?= kind
CHART_DIR ?= charts/k8sdockside
HELM_RELEASE ?= k8sdockside
HELM_NAMESPACE ?= k8sdockside
# Extra flags for helm-install, e.g. HELM_ARGS='-f my-values.yaml --set rbac.mode=admin'.
HELM_ARGS ?=

# IMG split into repository and tag for the chart. The tag is what follows the
# last colon with no slash after it, so a registry port
# (localhost:5000/k8sdockside:dev) is not mistaken for one.
IMG_REPOSITORY = $(shell echo '$(IMG)' | sed -E 's|:[^:/]+$$||')
IMG_TAG = $(shell echo '$(IMG)' | sed -nE 's|.*:([^:/]+)$$|\1|p')

.PHONY: build-server
build-server: generate build-frontend ## Build the production server-mode binary into bin/ (bindings and frontend first).
	@printf "$(CYAN)Building $(APP_NAME)-server...$(RESET)\n"
	@go build -tags server,production -trimpath -buildvcs=false -ldflags="-s -w" -o bin/$(APP_NAME)-server .
	@printf "$(GREEN)✓ Build complete: $(BOLD)bin/$(APP_NAME)-server$(RESET)\n"

.PHONY: build-go-server
build-go-server: frontend-dist-stub ## Compile the Go packages with -tags server (no frontend build).
	@printf "$(CYAN)Building Go packages (server mode)...$(RESET)\n"
	@go build -tags server $(GO_PKGS_SERVER)
	@printf "$(GREEN)✓ Go build complete (server mode)$(RESET)\n"

# Local clusters come from ~/.kube, read-only; everything the server stores
# (users, sessions, uploads, settings) goes to bin/server-data, so `make clean`
# leaves it alone and deleting that directory is a factory reset.
.PHONY: run-server
run-server: build-server ## Build and run the server on http://127.0.0.1:8080 (state in bin/server-data).
	@mkdir -p bin/server-data
	@printf "$(CYAN)Serving on $(BOLD)http://127.0.0.1:8080$(RESET)$(CYAN), state in bin/server-data...$(RESET)\n"
	@K8SDOCKSIDE_DATA_DIR=$(CURDIR)/bin/server-data \
		K8SDOCKSIDE_LISTEN_ADDR=127.0.0.1:8080 \
		K8SDOCKSIDE_IN_CLUSTER=false \
		K8SDOCKSIDE_KUBECONFIG_DIRS=$(HOME)/.kube \
		./bin/$(APP_NAME)-server

.PHONY: docker-build
docker-build: generate build-frontend ## Build the server image IMG for this machine's platform.
	@printf "$(CYAN)Building image $(BOLD)$(IMG)$(RESET)$(CYAN)...$(RESET)\n"
	@docker build -f build/docker/Dockerfile.server --build-arg GO_VERSION=$(GO_VERSION) -t $(IMG) .
	@printf "$(GREEN)✓ Image built: $(BOLD)$(IMG)$(RESET)\n"

# Pushes. The classic docker image store cannot hold a multi-platform image, so
# buildx sends it straight to the registry: needs `docker login` for IMG's
# registry, and a builder that can target both platforms (Docker Desktop's
# default can; elsewhere `docker buildx create --use` first).
.PHONY: docker-buildx
docker-buildx: generate build-frontend ## Build AND PUSH a multi-arch (linux/amd64, linux/arm64) image to IMG.
	@printf "$(YELLOW)Building and PUSHING $(BOLD)$(IMG)$(RESET)$(YELLOW) for $(PLATFORMS)...$(RESET)\n"
	@docker buildx build --platform $(PLATFORMS) -f build/docker/Dockerfile.server --build-arg GO_VERSION=$(GO_VERSION) -t $(IMG) --push .
	@printf "$(GREEN)✓ Pushed: $(BOLD)$(IMG)$(RESET)\n"

.PHONY: kind-load
kind-load: ## Load IMG into the kind cluster KIND_CLUSTER (default: kind).
	@printf "$(CYAN)Loading $(BOLD)$(IMG)$(RESET)$(CYAN) into kind cluster $(KIND_CLUSTER)...$(RESET)\n"
	@kind load docker-image $(IMG) --name $(KIND_CLUSTER)
	@printf "$(GREEN)✓ Image loaded$(RESET)\n"

.PHONY: helm-lint
helm-lint: ## Lint the Helm chart with default values and with every ci/*-values.yaml.
	@printf "$(CYAN)Linting $(CHART_DIR)...$(RESET)\n"
	@helm lint --strict $(CHART_DIR)
	@for f in $(CHART_DIR)/ci/*-values.yaml; do \
		printf "$(CYAN)Linting $(CHART_DIR) with $$f...$(RESET)\n"; \
		helm lint --strict $(CHART_DIR) -f $$f; \
	done
	@printf "$(GREEN)✓ Chart lint complete$(RESET)\n"

.PHONY: helm-template
helm-template: ## Render the chart (defaults and every ci/*-values.yaml) into bin/helm-template/.
	@printf "$(CYAN)Rendering $(CHART_DIR)...$(RESET)\n"
	@mkdir -p bin/helm-template
	@helm template $(HELM_RELEASE) $(CHART_DIR) --namespace $(HELM_NAMESPACE) > bin/helm-template/default.yaml
	@for f in $(CHART_DIR)/ci/*-values.yaml; do \
		helm template $(HELM_RELEASE) $(CHART_DIR) --namespace $(HELM_NAMESPACE) -f $$f > bin/helm-template/$$(basename $$f); \
	done
	@printf "$(GREEN)✓ Rendered into $(BOLD)bin/helm-template/$(RESET)\n"

# pullPolicy IfNotPresent so a kind-loaded image, which is in no registry, is
# used as is. Re-running this after a rebuild with the same tag changes nothing
# Helm can see: `kubectl -n $(HELM_NAMESPACE) rollout restart deployment/$(HELM_RELEASE)`.
.PHONY: helm-install
helm-install: ## Install or upgrade the chart into HELM_NAMESPACE (default: k8sdockside), running IMG.
	@printf "$(CYAN)Installing $(HELM_RELEASE) into namespace $(BOLD)$(HELM_NAMESPACE)$(RESET)$(CYAN) with $(IMG)...$(RESET)\n"
	@helm upgrade --install $(HELM_RELEASE) $(CHART_DIR) \
		--namespace $(HELM_NAMESPACE) --create-namespace \
		--set image.repository=$(IMG_REPOSITORY) \
		--set-string image.tag=$(IMG_TAG) \
		--set image.pullPolicy=IfNotPresent \
		$(HELM_ARGS)
	@printf "$(GREEN)✓ Installed. Reach it with: $(BOLD)kubectl -n $(HELM_NAMESPACE) port-forward svc/$(HELM_RELEASE) 8080:80$(RESET)\n"

.PHONY: helm-uninstall
helm-uninstall: ## Uninstall the chart from HELM_NAMESPACE.
	@printf "$(YELLOW)Uninstalling $(HELM_RELEASE) from namespace $(HELM_NAMESPACE)...$(RESET)\n"
	@helm uninstall $(HELM_RELEASE) --namespace $(HELM_NAMESPACE)
	@printf "$(GREEN)✓ Uninstalled$(RESET)\n"

##@ Code sanity
.PHONY: fmt
fmt: ## Run go fmt against code.
	@printf "$(CYAN)Running go fmt...$(RESET)\n"
	@go fmt ./...
	@printf "$(GREEN)✓ Code formatted$(RESET)\n"

# What CI's "Check formatting" step catches, without rewriting anything: a check
# before a commit should say what is wrong, not quietly change the tree. Both
# package lists, because a directory whose files are all `//go:build server`
# is missing from the plain one; gofmt then reads every file in each directory,
# whatever its build tags.
.PHONY: fmt-check
fmt-check: frontend-dist-stub ## Fail if any Go file is not gofmt-formatted (`make fmt` fixes it).
	@printf "$(CYAN)Checking Go formatting...$(RESET)\n"
	@dirs=$$( { go list -e -f '{{.Dir}}' ./...; go list -e -tags server -f '{{.Dir}}' ./...; } | sort -u ); \
	unformatted=$$(gofmt -l $$dirs); \
	if [ -n "$$unformatted" ]; then \
		printf "$(RED)✗ Not gofmt-formatted -- run $(BOLD)make fmt$(RESET)$(RED):$(RESET)\n$$unformatted\n"; \
		exit 1; \
	fi
	@printf "$(GREEN)✓ Formatting OK$(RESET)\n"

.PHONY: tidy-check
tidy-check: ## Fail if go.mod/go.sum are not tidy (`go mod tidy` fixes it).
	@printf "$(CYAN)Checking go.mod and go.sum are tidy...$(RESET)\n"
	@go mod tidy -diff || { printf "$(RED)✗ go.mod/go.sum are not tidy -- run $(BOLD)go mod tidy$(RESET)\n"; exit 1; }
	@printf "$(GREEN)✓ Modules tidy$(RESET)\n"

.PHONY: vet
vet: frontend-dist-stub ## Run go vet against code (default and server build tags).
	@printf "$(CYAN)Running go vet...$(RESET)\n"
	@go vet ./...
	@printf "$(CYAN)Running go vet (server mode)...$(RESET)\n"
	@go vet -tags server $(GO_PKGS_SERVER)
	@printf "$(GREEN)✓ Vet complete$(RESET)\n"

.PHONY: fix
fix: ## Run go fix against code.
	@printf "$(CYAN)Running go fix...$(RESET)\n"
	@go fix ./...
	@printf "$(GREEN)✓ Fix complete$(RESET)\n"

.PHONY: lint
lint: golangci-lint frontend-dist-stub ## Run golangci-lint against the Go code (default and server build tags).
	@printf "$(CYAN)Running golangci-lint...$(RESET)\n"
	@$(GOLANGCI_LINT) run --timeout 5m ./...
	@# Files behind `//go:build server` are invisible to the run above, and the
	@# desktop-only ones to this one, so it takes both to cover the code.
	@printf "$(CYAN)Running golangci-lint (server mode)...$(RESET)\n"
	@$(GOLANGCI_LINT) run --timeout 5m --build-tags server ./...
	@printf "$(GREEN)✓ Lint complete$(RESET)\n"

.PHONY: lint-frontend
lint-frontend: ## Type-check the Svelte frontend (svelte-check).
	@printf "$(CYAN)Running svelte-check...$(RESET)\n"
	@cd $(FRONTEND_DIR) && npm run check
	@printf "$(GREEN)✓ Frontend check complete$(RESET)\n"

##@ Tests
.PHONY: test
test: frontend-dist-stub ## Run unit tests.
	@printf "$(CYAN)Running unit tests...$(RESET)\n"
	@go test -v $(GO_PKGS) -coverprofile coverage.out
	@go tool cover -html=coverage.out -o coverage.html
	@printf "$(GREEN)✓ Tests complete - coverage report: $(BOLD)coverage.html$(RESET)\n"

.PHONY: test-frontend
test-frontend: ## Run the Svelte tests (vitest: state in jsdom, components in a browser).
	@printf "$(CYAN)Running vitest...$(RESET)\n"
	@cd $(FRONTEND_DIR) && npm test
	@printf "$(GREEN)✓ Frontend tests complete$(RESET)\n"

.PHONY: bench
bench: ## Run benchmarks (override with BENCH=<regex>, PKG=<package pattern>, COUNT=<n>)
	@bench_regex=$${BENCH:-.}; \
	pkg_pattern=$${PKG:-.}; \
	count=$${COUNT:-1}; \
	printf "$(CYAN)Running benchmarks: $(RESET)regex=$${bench_regex} packages=$${pkg_pattern} count=$${count}\n"; \
	go test -run=^$$ -bench=$${bench_regex} -benchmem -count=$${count} $${pkg_pattern}; \
	printf "$(GREEN)✓ Benchmarks complete$(RESET)\n"

##@ Dependencies
.PHONY: deps
deps: ## Download and verify Go dependencies, and install frontend deps.
	@printf "$(CYAN)Downloading Go dependencies...$(RESET)\n"
	@go mod download
	@go mod verify
	@go mod tidy
	@printf "$(CYAN)Installing frontend dependencies...$(RESET)\n"
	@cd $(FRONTEND_DIR) && npm install
	@printf "$(GREEN)✓ Dependencies ready!$(RESET)\n"

.PHONY: update-deps
update-deps: update-deps-go update-deps-frontend ## Update Go and frontend dependencies.
	@printf "$(GREEN)$(BOLD)✓ All dependencies updated!$(RESET)\n"

# `go get -u` walks the whole graph, which the Kubernetes libraries do not
# survive: k8s.io/apimachinery pins k8s.io/kube-openapi and
# sigs.k8s.io/structured-merge-diff per release, and upgrading either on its own
# leaves apimachinery type-checking against the wrong major. Compile afterwards
# so a graph like that fails here, loudly, instead of in the next build.
.PHONY: update-deps-go
update-deps-go: ## Update Go dependencies.
	@printf "$(CYAN)Updating Go dependencies...$(RESET)\n"
	@go get -u ./...
	@go mod tidy
	@printf "$(CYAN)Verifying the upgraded module graph still compiles...$(RESET)\n"
	@$(MAKE) --no-print-directory build-go
	@printf "$(GREEN)✓ Go dependencies updated!$(RESET)\n"

.PHONY: update-deps-frontend
update-deps-frontend: ## Update frontend dependencies (minor/patch only; TARGET=latest for majors).
	@target=$${TARGET:-minor}; \
	printf "$(CYAN)Updating frontend dependencies (target=$${target})...$(RESET)\n"; \
	cd $(FRONTEND_DIR) && \
	if command -v ncu >/dev/null 2>&1; then \
		ncu --target $${target} -u; \
	else \
		printf "$(YELLOW)ncu not found, using npx npm-check-updates...$(RESET)\n"; \
		npx --yes npm-check-updates --target $${target} -u; \
	fi; \
	npm install
	@printf "$(GREEN)✓ Frontend dependencies updated!$(RESET)\n"

##@ Tools
.PHONY: golangci-lint
golangci-lint: | $(LOCALBIN) ## Download golangci-lint locally if necessary.
	@$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

.PHONY: install-wails
install-wails: ## Install the wails3 CLI at the version required by go.mod.
	@printf "$(CYAN)Installing wails3 $(WAILS_VERSION)...$(RESET)\n"
	@go install github.com/wailsapp/wails/v3/cmd/wails3@$(WAILS_VERSION)
	@printf "$(GREEN)✓ wails3 $(WAILS_VERSION) installed$(RESET)\n"

.PHONY: install-security-scanner
install-security-scanner: $(GOSEC) ## Install gosec security scanner locally (static analysis for security issues)
$(GOSEC): | $(LOCALBIN)
	@set -e; printf "$(CYAN)Installing gosec $(GOSEC_VERSION)...$(RESET)\n"; \
	if ! GOBIN=$(LOCALBIN) go install github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION) 2>/dev/null; then \
		printf "$(YELLOW)Primary install failed, attempting fallback to @main...$(RESET)\n"; \
		if ! GOBIN=$(LOCALBIN) go install github.com/securego/gosec/v2/cmd/gosec@main; then \
			printf "$(RED)✗ gosec installation failed$(RESET)\n"; \
			exit 1; \
		fi; \
	fi; \
	printf "$(GREEN)✓ gosec installed at $(BOLD)$(GOSEC)$(RESET)\n"; \
	chmod +x $(GOSEC)

.PHONY: install-govulncheck
install-govulncheck: $(GOVULNCHECK) ## Install govulncheck locally (vulnerability scanner for Go)
$(GOVULNCHECK): | $(LOCALBIN)
	@set -e; echo "Attempting to install govulncheck $(GOVULNCHECK_VERSION)"; \
	if ! GOBIN=$(LOCALBIN) go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) 2>/dev/null; then \
		echo "Primary install failed, attempting install from @latest (compatibility fallback)"; \
		if ! GOBIN=$(LOCALBIN) go install golang.org/x/vuln/cmd/govulncheck@latest; then \
			echo "govulncheck installation failed for versions $(GOVULNCHECK_VERSION) and @latest"; \
			exit 1; \
		fi; \
	fi; \
	echo "govulncheck installed at $(GOVULNCHECK)"; \
	chmod +x $(GOVULNCHECK)

##@ Security
# gosec skips build/: the Wails scaffold's own iOS/Android dependency installers
# live there and trip G204/G702, which would fail every scan on code we don't own.
.PHONY: audit
audit: gosec govulncheck npm-audit ## Run all security scans (gosec + govulncheck + npm audit).
	@printf "$(GREEN)$(BOLD)✓ Audit complete$(RESET)\n"

.PHONY: gosec
gosec: install-security-scanner frontend-dist-stub ## Run gosec security scan (fails on findings)
	@printf "$(CYAN)Running gosec...$(RESET)\n"
	@$(GOSEC) -exclude-dir=build ./...
	@printf "$(GREEN)✓ gosec complete$(RESET)\n"

.PHONY: govulncheck
govulncheck: install-govulncheck frontend-dist-stub ## Run govulncheck vulnerability scan (fails on findings)
	@printf "$(CYAN)Running govulncheck...$(RESET)\n"
	@$(GOVULNCHECK) ./...
	@printf "$(GREEN)✓ govulncheck complete$(RESET)\n"

.PHONY: npm-audit
npm-audit: ## Run npm audit against the frontend dependencies.
	@printf "$(CYAN)Running npm audit...$(RESET)\n"
	@cd $(FRONTEND_DIR) && npm audit
	@printf "$(GREEN)✓ npm audit complete$(RESET)\n"

##@ Before you push
# Everything CI checks, on this machine, before a commit goes anywhere: the Go
# build, tests, vet, formatting and lint (desktop and server mode), the
# frontend's bindings, type check, tests and bundle, the Helm chart, and the
# security scans. Ordered so the cheapest checks fail first and the scans, which
# need the network, come last; it stops at the first failure and says which.
#
# It needs what CI installs: the wails3 CLI (`make install-wails`) for the
# bindings, the frontend's node_modules (`make deps`) and Playwright's Chromium
# (`cd frontend && npx playwright install chromium`) for its browser tests, and
# helm. npm audit here is `make npm-audit`, which fails on any advisory -- CI's
# frontend audit only fails on high, so this is the stricter of the two.
PRECHECK_STEPS = fmt-check tidy-check vet lint build-go build-go-server test \
	generate lint-frontend test-frontend build-frontend helm-lint helm-template audit

.PHONY: precheck
precheck: ## Run every check CI runs (Go, frontend, chart, security) -- use before commit and push.
	@start=$$SECONDS; \
	for step in $(PRECHECK_STEPS); do \
		printf "\n$(BOLD)$(BLUE)━━ precheck: $$step$(RESET)\n"; \
		$(MAKE) --no-print-directory $$step || { \
			printf "\n$(RED)$(BOLD)✗ precheck failed at: $$step$(RESET)\n"; \
			exit 1; \
		}; \
	done; \
	printf "\n$(GREEN)$(BOLD)✓ precheck passed in $$((SECONDS - start))s -- all %d checks green$(RESET)\n" $(words $(PRECHECK_STEPS))

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
printf "$(CYAN)Downloading $${package}...$(RESET)\n" ;\
rm -f $(1) || true ;\
GOTOOLCHAIN=$(GO_TOOLCHAIN) GOBIN=$(LOCALBIN) go install $${package} ;\
mv $(1) $(1)-$(3) ;\
printf "$(GREEN)✓ Installed $(BOLD)$(1)-$(3)$(RESET)\n" ;\
} ;\
ln -sf $(1)-$(3) $(1)
endef
