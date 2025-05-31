# ──────────────────────────────────────────────────────────────────────────────
# Makefile for ZEAM (Cosmos SDK v0.53.0 + Cobra v1.9.1 + Go 1.24)
# ──────────────────────────────────────────────────────────────────────────────

MODULE     := $(shell go list -m)
BINARY     := zeamd
CMD_PATH   := ./app/cmd/zeamd

GO         := go
GOBIN      := $(shell go env GOPATH)/bin

# default target
all: build

# tidy go.mod & go.sum
.PHONY: tidy
tidy:
	$(GO) mod tidy

# vendor dependencies
.PHONY: vendor
vendor: tidy
	$(GO) mod vendor

# build the binary
.PHONY: build
build:
	$(GO) build -o $(BINARY) $(CMD_PATH)

# install to GOBIN
.PHONY: install
install:
	$(GO) install $(CMD_PATH)@latest

# run tests
.PHONY: test
test:
	$(GO) test ./...

# remove generated binary
.PHONY: clean
clean:
	rm -f $(BINARY)

# show this help
.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' Makefile \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'
