# Big Board development tasks

.PHONY: help build install test test-coverage fmt lint vet tidy clean staticcheck check

help:
	@echo "Available targets:"
	@echo "  build        - Build the bigboard binary with version information"
	@echo "  install      - Build and install bigboard to ~/.bin"
	@echo "  test         - Run all unit tests"
	@echo "  test-coverage - Run tests with coverage"
	@echo "  fmt          - Format Go source code"
	@echo "  lint         - Run golangci-lint v2"
	@echo "  vet          - Run go vet"
	@echo "  tidy         - Tidy go modules"
	@echo "  clean        - Clean build artifacts and test cache"
	@echo "  staticcheck  - Run staticcheck"
	@echo "  check        - Run all quality checks (fmt, lint, vet, staticcheck, tests)"

build:
	@echo "Building bigboard with version information..."
	@mkdir -p bin
	@VERSION=$$(git describe --tags --always --dirty 2>/dev/null || echo "dev"); \
	COMMIT=$$(git rev-parse --short HEAD 2>/dev/null || echo "none"); \
	DATE=$$(date -u +"%Y-%m-%dT%H:%M:%SZ"); \
	if ! go build -ldflags "-X main.version=$$VERSION -X main.commit=$$COMMIT -X main.date=$$DATE" -o bin/bigboard ./cmd/bigboard; then \
		echo "Build failed"; \
		exit 1; \
	fi; \
	echo "Built bigboard binary to bin/ (version: $$VERSION)"

install: build
	@echo "Installing bigboard to ~/.bin..."
	@cp bin/bigboard ~/.bin/bigboard
	@echo "Installed bigboard to ~/.bin/bigboard"

test:
	@echo "Running unit tests..."
	@go test ./...
	@echo "Unit tests passed!"

test-coverage:
	@echo "Running unit tests with coverage..."
	@go clean -testcache
	@go test -covermode=atomic -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out > coverage.txt
	@awk 'END{printf "Total coverage: %s\n", $$3}' coverage.txt
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Unit tests passed! Coverage report: coverage.html (see also coverage.txt)"

fmt:
	@echo "Formatting Go source code..."
	@go fmt ./...
	@echo "Formatting complete!"

lint:
	@echo "Running golangci-lint v2..."
	@go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.8.0 run --timeout=10m ./...
	@echo "Linting passed!"

vet:
	@echo "Running go vet..."
	@go vet ./...
	@echo "Vet passed!"

tidy:
	@echo "Tidying go modules..."
	@go mod tidy
	@echo "Modules tidied!"

clean:
	@echo "Cleaning build artifacts and caches..."
	@rm -rf bin
	@rm -f coverage.out coverage.html coverage.txt
	@go clean
	@go clean -testcache
	@echo "Build artifacts and test cache cleaned"

staticcheck:
	@echo "Running staticcheck..."
	@go run honnef.co/go/tools/cmd/staticcheck@latest ./...
	@echo "Staticcheck passed!"

check: fmt lint vet staticcheck test
