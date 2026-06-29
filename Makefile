SHELL := /bin/sh
.DEFAULT_GOAL := check

TS_DIR := ts
GO_DIR := go
RUST_DIR := rust
EXAMPLE_TS_DIR := examples/ts
EXAMPLE_GO_DIR := examples/go
EXAMPLE_RUST_DIR := examples/rust
GO_FILES := $(shell find $(GO_DIR) $(EXAMPLE_GO_DIR) -name '*.go' -type f)

.PHONY: check hooks generate generate-check build build-ts test test-ts test-go test-rust examples example-ts example-go example-rust fmt fmt-ts fmt-go fmt-rust clean clean-ts clean-go clean-rust clean-examples

check:
	node scripts/check.mjs

hooks:
	git config core.hooksPath .githooks

generate:
	node scripts/sync-hosts.mjs

generate-check:
	node scripts/sync-hosts.mjs --check

build: generate-check build-ts

build-ts: generate-check
	pnpm --dir $(TS_DIR) build

test: test-ts test-go test-rust

test-ts:
	pnpm --dir $(TS_DIR) test

test-go:
	cd $(GO_DIR) && go test ./...

test-rust:
	cargo test --manifest-path $(RUST_DIR)/Cargo.toml

examples: example-ts example-go example-rust

example-ts:
	pnpm --dir $(EXAMPLE_TS_DIR) install-skill

example-go:
	cd $(EXAMPLE_GO_DIR) && go run .

example-rust:
	cargo run --quiet --manifest-path $(EXAMPLE_RUST_DIR)/Cargo.toml

fmt: fmt-ts fmt-go fmt-rust

fmt-ts:
	cd $(TS_DIR) && pnpm exec prettier --write src test ../examples/ts/cli.ts

fmt-go:
	gofmt -w $(GO_FILES)

fmt-rust:
	cargo fmt --manifest-path $(RUST_DIR)/Cargo.toml
	cargo fmt --manifest-path $(EXAMPLE_RUST_DIR)/Cargo.toml

clean: clean-ts clean-go clean-rust clean-examples

clean-ts:
	rm -rf \
		$(TS_DIR)/node_modules \
		$(TS_DIR)/dist \
		$(TS_DIR)/coverage \
		$(TS_DIR)/*.tsbuildinfo

clean-go:
	rm -f \
		$(GO_DIR)/coverage.out

clean-rust:
	rm -rf \
		target \
		$(RUST_DIR)/target

clean-examples:
	rm -rf \
		$(EXAMPLE_TS_DIR)/node_modules \
		$(EXAMPLE_TS_DIR)/dist \
		$(EXAMPLE_TS_DIR)/coverage \
		$(EXAMPLE_TS_DIR)/*.tsbuildinfo \
		$(EXAMPLE_RUST_DIR)/target \
		$(EXAMPLE_GO_DIR)/coverage.out
