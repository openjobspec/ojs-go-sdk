.PHONY: test lint cover vet clean

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

# Run all linters (vet + staticcheck if available).
lint: vet
	@which staticcheck > /dev/null 2>&1 && staticcheck ./... || echo "staticcheck not installed, skipping (go install honnef.co/go/tools/cmd/staticcheck@latest)"

# Remove build artifacts.
clean:
	rm -f coverage.out
