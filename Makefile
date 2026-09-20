.PHONY: help build install test fmt fmt-check lint check clean

help:
	@echo 'Targets: build install test fmt fmt-check lint check clean'

build:
	BIGBOARD_COMMIT="$$(git rev-parse --short HEAD)" BIGBOARD_BUILD_DATE="$$(date -u +%Y-%m-%dT%H:%M:%SZ)" cargo build --release --locked

install:
	cargo install --path . --locked

test:
	cargo test --locked

fmt:
	cargo fmt --all

fmt-check:
	cargo fmt --all -- --check

lint:
	cargo clippy --all-targets --locked -- -D warnings

check: fmt-check lint test

clean:
	cargo clean
