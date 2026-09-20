.PHONY: help build install test fmt fmt-check lint check clean parity

help:
	@echo 'Targets: build install test fmt fmt-check lint check parity clean'
	@echo 'parity requires GO_BIGBOARD=/path/to/reference-Go-binary'

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

parity:
	@test -n "$(GO_BIGBOARD)" || { echo 'Set GO_BIGBOARD to the reference Go binary'; exit 1; }
	cargo build --locked
	python3 scripts/check_parity.py --reference "$(GO_BIGBOARD)" --candidate target/debug/bigboard

clean:
	cargo clean
