.PHONY: help build dev test check clean
DEVELOPER_DIR ?= /Applications/Xcode.app/Contents/Developer
export DEVELOPER_DIR
help:
	@echo "build: native Bigboard.app; dev: build and open Bigboard Dev.app; test/check: Swift tests"
build:
	bash scripts/build-macos.sh
dev:
	bash scripts/build-macos.sh Development
	open "build/Bigboard Dev.app"
test:
	swift test --no-parallel
check: test
	bash scripts/build-macos.sh Release
	lipo build/Bigboard.app/Contents/MacOS/Bigboard -verify_arch arm64
	lipo build/Bigboard.app/Contents/MacOS/Bigboard -verify_arch x86_64
clean:
	swift package clean
	rm -rf build
