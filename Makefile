
GO_TAGS ?= sqlite_fts5
VERSION ?= v0.0.0-dev
GO_LDFLAGS ?= -X github.com/arwos/ai-agent/internal/pkg/version.Value=$(VERSION)

# Web commands (web/package.json)
.PHONY: web-install
web-install:
	pnpm --dir web install --frozen-lockfile

.PHONY: web-build
web-build:
	pnpm --dir web build

.PHONY: web-typecheck
web-typecheck:
	pnpm --dir web typecheck

.PHONY: web-lint
web-lint:
	pnpm --dir web lint

.PHONY: web-lint-fix
web-lint-fix:
	pnpm --dir web lint:fix

.PHONY: web-format
web-format:
	pnpm --dir web format

.PHONY: web-format-check
web-format-check:
	pnpm --dir web format:check

.PHONY: web-dev
web-dev:
	pnpm --dir web dev

.PHONY: web-test
web-test: web-typecheck
	pnpm --dir web run --if-present test

# Backend commands
.PHONY: install
install:
	go install go.osspkg.com/goppy/v3/cmd/goppy@latest
	goppy setup-lib

.PHONY: lint
lint:
	goppy lint

.PHONY: license
license:
	goppy license

.PHONY: tests
tests:
	goppy test

.PHONY: run-local
run-local: web-build
	go run -tags "$(GO_TAGS)" -race ./cmd/arwos-agent --config=./config/config.dev.yaml

# Composite commands
.PHONY: build
build: web-build
	mkdir -p ./build
	CGO_ENABLED=1 go build -tags "$(GO_TAGS)" -ldflags "$(GO_LDFLAGS)" -o ./build/arwos-agent ./cmd/arwos-agent

.PHONY: pre-commit
pre-commit: install license lint tests web-test build

.PHONY: ci
ci:
	@echo "Temporarily disabled"
