.PHONY: build test quick arch align verify help

all: quick

build:
	go build -o /tmp/pigasus ./cmd/pigasus
	go build -o /tmp/harness ./cmd/harness
	go build -o /tmp/mock-remote-agent ./cmd/mock-remote-agent
	go build -o /tmp/storybook ./cmd/storybook
	ln -sf /tmp/storybook /tmp/storyboard
	go build -o /tmp/tui-inventory ./cmd/tui-inventory
	go build -o /tmp/herdr ./cmd/herdr

test:
	./scripts/verify.sh all

quick:
	./scripts/verify.sh quick

arch:
	./scripts/verify.sh arch

align:
	./scripts/verify.sh align

verify:
	./scripts/verify.sh all

help:
	@echo "OpenAGY / Pigasus Build & Test targets:"
	@echo "  make build   - Build pigasus and harness binaries"
	@echo "  make quick   - Run architecture guard, core unit tests, and build test"
	@echo "  make test    - Run complete standard test suite"
	@echo "  make arch    - Run architecture layering compliance guard"
	@echo "  make align   - Run tuialign scenarios (skips the live half without agy/tmux)"
	@echo "  make verify  - Full verification suite via scripts/verify.sh"
