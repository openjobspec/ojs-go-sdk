.PHONY: test lint cover vet bench clean docs

# Run all tests with race detection.
test:
	go test ./... -race -count=1

# Run tests with coverage report.
cover:
	go test ./... -race -coverprofile=coverage.out
	go tool cover -func=coverage.out
	@echo ""
	@echo "To view HTML report: go tool cover -html=coverage.out"

# Run go vet.
vet:
	go vet ./...

# Run all linters (vet + staticcheck).
lint: vet
	@which staticcheck > /dev/null 2>&1 || (echo "Installing staticcheck..." && go install honnef.co/go/tools/cmd/staticcheck@latest)
	staticcheck ./...

# Run benchmarks.
bench:
	go test ./... -bench=. -benchmem -run='^$$' -count=1

# Generate documentation using pkgsite.
docs:
	@echo "Go docs are generated via godoc or pkgsite."
	@echo "Run: go install golang.org/x/pkgsite/cmd/pkgsite@latest && pkgsite -open ."

# Remove build artifacts.
clean:
	rm -f coverage.out
