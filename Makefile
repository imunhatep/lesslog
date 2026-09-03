BINARY  ?= lesslog
PKG     ?= ./cmd/lesslog
PREFIX  ?= $(HOME)/.local
GO      ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
PLATFORMS ?= darwin/arm64 darwin/amd64 linux/arm64 linux/amd64

.DEFAULT_GOAL := build

.PHONY: help
help: ## list targets
	@awk 'BEGIN{FS=":.*## "} /^[a-z][a-z0-9_-]*:.*## /{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: build
build: ## build ./bin/$(BINARY)
	$(GO) build -ldflags '$(LDFLAGS)' -o bin/$(BINARY) $(PKG)

.PHONY: install
install: ## install into $(PREFIX)/bin
	@mkdir -p $(PREFIX)/bin
	$(GO) build -ldflags '$(LDFLAGS)' -o $(PREFIX)/bin/$(BINARY) $(PKG)
	@echo "installed $(PREFIX)/bin/$(BINARY) ($(VERSION))"

.PHONY: uninstall
uninstall: ## remove $(PREFIX)/bin/$(BINARY)
	rm -f $(PREFIX)/bin/$(BINARY)

.PHONY: test
test: ## run tests
	$(GO) test ./...

.PHONY: race
race: ## run tests with the race detector
	$(GO) test -race -count=1 ./...

.PHONY: cover
cover: ## report test coverage
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -1

.PHONY: vet
vet: ## run go vet
	$(GO) vet ./...

.PHONY: fmt
fmt: ## format sources
	$(GO) fmt ./...

.PHONY: fmtcheck
fmtcheck: ## fail if sources are not gofmt-clean
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "not gofmt-clean:"; echo "$$out"; exit 1; fi

.PHONY: tidy
tidy: ## tidy go.mod / go.sum
	$(GO) mod tidy

.PHONY: check
check: fmtcheck vet test ## fmtcheck + vet + test

.PHONY: demo
demo: build ## page the sample log
	./bin/$(BINARY) testdata/sample.log

.PHONY: dist
dist: ## cross-build into build/ for $(PLATFORMS)
	@mkdir -p build
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		echo "build/$(BINARY)-$$os-$$arch"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 \
			$(GO) build -ldflags '$(LDFLAGS)' -o build/$(BINARY)-$$os-$$arch $(PKG) || exit 1; \
	done

.PHONY: clean
clean: ## remove build output
	rm -rf bin build coverage.out
