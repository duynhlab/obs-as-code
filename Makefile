# obs-as-code — Grafana dashboards and alerts as Go code.

SHELL := /usr/bin/env bash -o pipefail
.SHELLFLAGS := -ec

.DEFAULT_GOAL := help

# Match CI exactly. `toolchain` in go.mod is only a floor — Go happily uses a
# newer local toolchain — so a developer on Go 1.27 and a runner on 1.26.7 can
# produce different bytes from identical source. That already happened once:
# compress/flate output differed between the two, and every `make diff` failed.
export GOTOOLCHAIN ?= go1.26.7

GO              ?= go
OUT             ?= generated
BIN             := $(CURDIR)/bin
GOLANGCI_LINT   := $(BIN)/golangci-lint
GO_TEST_ARGS    ?= -race
GRAFANA_IMAGE   ?= grafana/grafana:13.2.0

##@ General

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.PHONY: check
check: tidy-check fmt vet build-linux lint test diff validate ## Everything CI runs; green before every commit

##@ Go

.PHONY: tidy
tidy: ## Tidy go.mod for the module and the tools module
	$(GO) mod tidy
	$(GO) -C tools mod tidy

# `go mod tidy` records go.sum entries for every build configuration, not just
# the host's. Skipping it hides a Linux-only dependency until CI finds it: for
# example client_golang pulls in procfs from a file that only builds on Linux,
# so a macOS `go build` passes and the runner fails on a missing go.sum entry.
.PHONY: tidy-check
tidy-check: ## Fail if go.mod or go.sum is not tidy
	@$(GO) mod tidy
	@$(GO) -C tools mod tidy
	@if [[ -n "$$(git status --porcelain -- go.mod go.sum tools/go.mod tools/go.sum)" ]]; then \
		echo "✘ go.mod/go.sum are not tidy. Run 'make tidy' and commit the result."; \
		git --no-pager diff -- go.mod go.sum tools/go.mod tools/go.sum; \
		exit 1; \
	fi
	@echo "✔ modules are tidy"

# The generator only ever runs on Linux in CI and on a developer's machine, but
# the dependency graph differs per platform. Compiling for Linux locally catches
# a build-tagged import before it reaches a runner.
.PHONY: build-linux
build-linux: ## Cross-compile for linux/amd64, the platform CI builds on
	GOOS=linux GOARCH=amd64 $(GO) build ./...

.PHONY: fmt
fmt: ## Format all Go code
	$(GO) fmt ./...

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

# Built from tools/go.mod rather than installed, so the linter a developer runs
# and the one CI runs are the same build of the same pinned version. `go -C tools
# run` cannot be used directly: it would resolve ./... and the config path
# relative to tools/.
$(GOLANGCI_LINT): tools/go.mod tools/go.sum
	$(GO) -C tools build -o $(GOLANGCI_LINT) github.com/golangci/golangci-lint/v2/cmd/golangci-lint

.PHONY: lint
lint: $(GOLANGCI_LINT) ## Run golangci-lint at the version pinned in tools/go.mod
	$(GOLANGCI_LINT) --version
	$(GOLANGCI_LINT) run --config=.golangci.yml

.PHONY: test
test: ## Run tests with the race detector
	$(GO) test $(GO_TEST_ARGS) ./...

.PHONY: golden
golden: ## Rewrite golden files, then review the diff before committing
	$(GO) test ./internal/catalog/ -update
	@echo "✔ golden files rewritten — review 'git diff internal/catalog/testdata' before committing"

##@ Generate

.PHONY: generate
generate: ## Render every dashboard to JSON in $(OUT)
	$(GO) run ./cmd/generate -out=$(OUT)

.PHONY: diff
diff: ## Regenerate in a temporary directory and compare with $(OUT)
	@tmp="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmp"' EXIT; \
	$(GO) run ./cmd/generate -out="$$tmp/generated" >/dev/null; \
	if ! diff -ru "$(OUT)" "$$tmp/generated"; then \
		echo "✘ $(OUT) is out of sync with the code. Run 'make generate'."; \
		exit 1; \
	fi
	@echo "✔ $(OUT) is in sync with the code"

.PHONY: validate
validate: ## Validate JSON identity and build the deployable Kustomization
	@./hack/validate.sh "$(OUT)"

.PHONY: dry-run
dry-run: generate ## Server-side dry-run against the current Kubernetes context
	kubectl apply --dry-run=server -k "$(OUT)/cluster/manifests"

##@ Preview

.PHONY: preview
preview: generate ## Run Grafana locally and import Dashboard V2 resources
	@GRAFANA_IMAGE="$(GRAFANA_IMAGE)" ./hack/preview.sh "$(OUT)/cluster/dashboards"

##@ Utilities

.PHONY: clean
clean: ## Remove build and coverage artifacts (keeps $(OUT), which is committed)
	rm -rf $(BIN) .preview coverage.out coverage-integration.out coverage.html
