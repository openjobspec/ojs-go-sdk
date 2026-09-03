.PHONY: build build-all test lint cover vet bench clean docs fmt tidy tidy-check staticcheck golangci lint-all test-all tools

# Pinned tool versions keep local runs and CI byte-for-byte reproducible.
# staticcheck 2025.1.1 is the release used by golangci-lint v1.64.8 in CI.
STATICCHECK_VERSION ?= 2025.1.1
STATICCHECK        ?= $(shell go env GOPATH)/bin/staticcheck

# golangci-lint is pinned to the exact version CI runs. The binary is installed
# under a version-suffixed name so a different golangci-lint on PATH (v2 is
# common on developer machines and rejects this v1 config file) can never be
# used by accident: every target invokes $(GOLANGCI_LINT) by absolute path.
GOLANGCI_LINT_VERSION ?= 1.64.8
GOLANGCI_LINT         ?= $(shell go env GOPATH)/bin/golangci-lint-$(GOLANGCI_LINT_VERSION)

# Nested modules verified independently of the root module.
SUBMODULES := grpc middleware/otel

# GOWORK=off keeps every target standalone: the surrounding go.work must not
# silently substitute sibling checkouts for this module's declared requirements.
GO := GOWORK=off go

# Build the root module.
build:
	$(GO) build ./...

# Build the root module and every nested module.
build-all: build
	@for m in $(SUBMODULES); do \
		echo "==> $$m"; \
		(cd $$m && $(GO) build ./...) || exit 1; \
	done

# Run all tests with race detection.
test:
	$(GO) test ./... -race -count=1

# Run tests for the root module and every nested module.
test-all: test
	@for m in $(SUBMODULES); do \
		echo "==> $$m"; \
		(cd $$m && $(GO) test ./... -race -count=1) || exit 1; \
	done

# Run tests with coverage report.
cover:
	$(GO) test ./... -race -coverprofile=coverage.out
	go tool cover -func=coverage.out
	@echo ""
	@echo "To view HTML report: go tool cover -html=coverage.out"

# Run go vet.
vet:
	$(GO) vet ./...

# Install the pinned staticcheck if it is missing or the wrong version.
$(STATICCHECK):
	GOWORK=off go install honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION)

staticcheck: $(STATICCHECK)
	@have=$$($(STATICCHECK) --version 2>/dev/null | awk '{print $$2}'); \
	if [ "$$have" != "$(STATICCHECK_VERSION)" ]; then \
		echo "staticcheck $$have found, installing pinned $(STATICCHECK_VERSION)..."; \
		GOWORK=off go install honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION); \
	fi
	GOWORK=off $(STATICCHECK) ./...

# Install the pinned golangci-lint under its version-suffixed name. GOBIN is
# redirected to a scratch directory so the unsuffixed binary in GOPATH/bin (and
# therefore anything already on PATH) is never overwritten.
$(GOLANGCI_LINT):
	@echo "installing golangci-lint v$(GOLANGCI_LINT_VERSION)..."
	@tmp=$$(mktemp -d); \
		trap 'rm -rf "$$tmp"' EXIT; \
		GOWORK=off GOBIN=$$tmp go install \
			github.com/golangci/golangci-lint/cmd/golangci-lint@v$(GOLANGCI_LINT_VERSION) && \
		mkdir -p $(dir $(GOLANGCI_LINT)) && \
		mv $$tmp/golangci-lint $(GOLANGCI_LINT)

# Guard: refuse to lint with anything but the pinned version, and reinstall if
# the cached binary drifted. Findings differ between golangci-lint releases, so
# an unpinned run is not a reproducible gate.
golangci: $(GOLANGCI_LINT)
	@have=$$($(GOLANGCI_LINT) --version 2>/dev/null | awk '{print $$4}' | sed 's/^v//'); \
	if [ "$$have" != "$(GOLANGCI_LINT_VERSION)" ]; then \
		echo "golangci-lint $$have found at $(GOLANGCI_LINT), reinstalling pinned $(GOLANGCI_LINT_VERSION)..."; \
		rm -f $(GOLANGCI_LINT); \
		$(MAKE) $(GOLANGCI_LINT); \
	fi
	GOWORK=off $(GOLANGCI_LINT) run ./...

# Install every pinned tool without running it.
tools: $(STATICCHECK) $(GOLANGCI_LINT)

# Run all linters (vet + staticcheck + the configured golangci-lint) for the
# root module.
lint: vet staticcheck golangci

# Lint the root module and every nested module. The nested modules inherit the
# repository .golangci.yml, which golangci-lint resolves by walking up from the
# module directory.
lint-all: lint
	@for m in $(SUBMODULES); do \
		echo "==> $$m"; \
		(cd $$m && $(GO) vet ./... \
			&& GOWORK=off $(STATICCHECK) ./... \
			&& GOWORK=off $(GOLANGCI_LINT) run ./...) || exit 1; \
	done

# Verify formatting.
fmt:
	@out=$$(gofmt -l .); \
	if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

# Verify module requirements are tidy across all modules.
tidy:
	$(GO) mod tidy
	@for m in $(SUBMODULES); do (cd $$m && $(GO) mod tidy) || exit 1; done

# Verify module requirements are tidy without changing files.
tidy-check:
	$(GO) mod tidy -diff
	@for m in $(SUBMODULES); do (cd $$m && $(GO) mod tidy -diff) || exit 1; done

# Run benchmarks.
bench:
	$(GO) test ./... -bench=. -benchmem -run='^$$' -count=1

# Generate documentation using pkgsite.
docs:
	@echo "Go docs are generated via godoc or pkgsite."
	@echo "Run: go install golang.org/x/pkgsite/cmd/pkgsite@latest && pkgsite -open ."

# Remove build artifacts.
clean:
	rm -f coverage.out
