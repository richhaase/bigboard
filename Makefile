.PHONY: help build dev preview test test-core test-desktop test-frontend test-coverage fmt fmt-check lint lint-core lint-desktop vet vet-core vet-desktop tidy clean staticcheck staticcheck-core staticcheck-desktop check gui gui-build gui-preview gui-test

WAILS = go run github.com/wailsapp/wails/v2/cmd/wails@v2.17.0
NODE = fnm exec --using $(shell cat .node-version)
LINT = go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.8.0
STATICCHECK = go run honnef.co/go/tools/cmd/staticcheck@latest

help:
	@echo "build: build Bigboard.app; dev: run the desktop app; preview: local browser development"
	@echo "test: core, desktop, and frontend tests; check: formatting, lint, vet, staticcheck, tests"
	@echo "Core-only checks use the -core suffix; desktop checks use -desktop."

build:
	cd desktop && auth_flags="$$(bash ../scripts/auth-ldflags.sh)" && $(WAILS) build -v 0 -skipbindings -s -m -ldflags "$$auth_flags"

dev:
	cd desktop && auth_flags="$$(bash ../scripts/auth-ldflags.sh)" && $(WAILS) dev -v 0 -s -skipbindings -ldflags "$$auth_flags"

preview:
	cd desktop && auth_flags="$$(bash ../scripts/auth-ldflags.sh)" && go run -ldflags "$$auth_flags" . --serve

test: test-core test-desktop test-frontend

test-core:
	go test ./...

test-desktop:
	cd desktop && go test ./...

test-frontend:
	$(NODE) node --test desktop/frontend/*.test.js

test-coverage:
	go test -covermode=atomic -coverprofile=coverage.out ./...
	cd desktop && go test -covermode=atomic -coverprofile=coverage.out ./...

fmt:
	go fmt ./...
	cd desktop && go fmt ./...

fmt-check:
	@files="$$(gofmt -l auth git github gui stats desktop/*.go)"; status=$$?; \
		test $$status -eq 0 || exit $$status; \
		test -z "$$files" || { printf '%s\n' "$$files"; exit 1; }

lint: lint-core lint-desktop

lint-core:
	$(LINT) run --timeout=10m ./...

lint-desktop:
	cd desktop && $(LINT) run --timeout=10m ./...

vet: vet-core vet-desktop

vet-core:
	go vet ./...

vet-desktop:
	cd desktop && go vet ./...

staticcheck: staticcheck-core staticcheck-desktop

staticcheck-core:
	$(STATICCHECK) ./...

staticcheck-desktop:
	cd desktop && $(STATICCHECK) ./...

tidy:
	go mod tidy
	cd desktop && go mod tidy

clean:
	rm -rf desktop/build/bin
	rm -f coverage.out coverage.html coverage.txt desktop/coverage.out

check: fmt-check lint vet staticcheck test

gui: dev

gui-build: build

gui-preview: preview

gui-test: test
