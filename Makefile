.PHONY: help build dev test check clean
DEVELOPER_DIR ?= /Applications/Xcode.app/Contents/Developer
export DEVELOPER_DIR
help:
	@echo "build: native Bigboard.app; dev: build and open Bigboard Dev.app; test/check: Swift tests"
build:
	bash scripts/build-swift.sh
dev:
	bash scripts/build-swift.sh --development
	open "native/build/Bigboard Dev.app"
test:
	swift test --package-path native
check: test
	swift build --package-path native -c release
clean:
	swift package --package-path native clean
