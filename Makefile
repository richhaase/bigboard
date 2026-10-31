.PHONY: help build dev test check clean legacy-test
DEVELOPER_DIR ?= /Applications/Xcode.app/Contents/Developer
export DEVELOPER_DIR
help:
	@echo "build: native Bigboard.app; dev: build and open; test/check: Swift tests; legacy-test: reference tests"
build:
	bash scripts/build-swift.sh
dev: build
	open native/build/Bigboard.app
test:
	swift test --package-path native
check: test
	swift build --package-path native -c release
legacy-test:
	$(MAKE) -f Makefile.legacy test
clean:
	swift package --package-path native clean
