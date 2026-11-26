# Build and verification targets for the retained APK HTML application.
# See docs/scaffold-scope.md for the retained scope.
.DEFAULT_GOAL := all

GO ?= go
BINS ?= $(notdir $(patsubst %/,%,$(dir $(wildcard cmd/*/main.go))))

.PHONY: all build test vet lint verify race cover tidy format clean help FORCE

all: verify build ## Test, vet, and build the retained application.

build: $(addprefix _output/bin/,$(BINS)) ## Build retained commands; optionally set BINS="site worker".

_output/bin/%: FORCE
	@mkdir -p _output/bin
	$(GO) build -mod=readonly -trimpath -o "$@" ./cmd/$*

test: ## Run all tests, with no package filters or skipped suites.
	$(GO) test -mod=readonly ./...

vet: ## Run Go's static checks on every retained package.
	$(GO) vet -mod=readonly ./...

lint: vet ## Alias for vet; no extra tool installation is required.

verify: test vet ## Run the complete test and vet checks.

race: ## Run all tests with the race detector (requires CGO and a C compiler).
	$(GO) test -mod=readonly -race ./...

cover: ## Collect coverage for all retained packages.
	@mkdir -p _output
	$(GO) test -mod=readonly -coverprofile=_output/coverage.out ./...
	$(GO) tool cover -func=_output/coverage.out

tidy: ## Explicitly reconcile go.mod and go.sum after dependency changes.
	$(GO) mod tidy

format: ## Explicitly format retained Go packages.
	$(GO) fmt ./...

clean: ## Remove only this Makefile's build and coverage output.
	rm -rf -- ./_output

help: ## Show available targets.
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ {printf "%-12s %s\n", $$1, $$2}' Makefile

FORCE:
