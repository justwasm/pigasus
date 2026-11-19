.PHONY: build test quick arch align wirealign verify help \
        fetch-upstream extract-agy extract-ls extract-localharness extract-all check-reproducibility

BIN_DIR ?= ./bin

all: quick

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/pigasus ./cmd/pigasus
	go build -o $(BIN_DIR)/pigga ./cmd/pigga
	go build -o $(BIN_DIR)/harness ./cmd/harness
	go build -o $(BIN_DIR)/mock-remote-agent ./cmd/mock-remote-agent
	go build -o $(BIN_DIR)/storybook ./cmd/storybook
	ln -sf storybook $(BIN_DIR)/storyboard
	go build -o $(BIN_DIR)/tui-inventory ./cmd/tui-inventory
	go build -o $(BIN_DIR)/herdr ./cmd/herdr
	go build -o $(BIN_DIR)/livechat ./cmd/livechat

test:
	./scripts/verify.sh all

quick:
	./scripts/verify.sh quick

arch:
	./scripts/verify.sh arch

align:
	./scripts/verify.sh align

wirealign:
	./scripts/wire-align.sh test

verify:
	./scripts/verify.sh all

# Reverse-engineering extraction SOP targets
fetch-upstream:
	python3 scripts/re/fetch_binaries.py --target all

extract-agy:
	python3 scripts/re/extract_agy.py

extract-ls:
	python3 scripts/re/extract_ls.py

extract-localharness:
	python3 scripts/re/extract_localharness.py

extract-all:
	python3 scripts/re/extract_all.py

check-reproducibility:
	python3 scripts/re/verify_reproducibility.py --target all

help:
	@echo "OpenAGY / Pigasus Build & Test targets:"
	@echo "  make build                - Build all binaries into $(BIN_DIR)/"
	@echo "  make quick                - Run architecture guard, core unit tests, and build test"
	@echo "  make test                 - Run complete standard test suite"
	@echo "  make arch                 - Run architecture layering compliance guard"
	@echo "  make align                - Run tuialign scenarios (skips the live half without agy/tmux)"
	@echo "  make verify               - Full verification suite via scripts/verify.sh"
	@echo ""
	@echo "Reverse-engineering extraction SOP targets:"
	@echo "  make fetch-upstream       - Fetch reference binaries from official distribution endpoints"
	@echo "  make extract-agy          - Extract seeds, embed-fs, web assets & protos from agy CLI"
	@echo "  make extract-ls           - Extract seeds, embed-fs, protoset & JS from Antigravity.app"
	@echo "  make extract-localharness - Extract seeds & protos from google-antigravity wheel"
	@echo "  make extract-all          - Run all extraction pipelines"
	@echo "  make check-reproducibility- Verify 100% byte-for-byte reproducibility of all extractions"
