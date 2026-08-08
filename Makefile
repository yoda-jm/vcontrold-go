# Overridable: make deploy TARGET_HOST=user@host
# Real values live in .env, which is not versioned.
-include .env
export

TARGET_HOST ?= user@boiler-host.local
TARGET_DIR  ?= /opt/vcontrold-go
BIN         := vcontrold-go
PKG         := ./cmd/vcontrold-go

# Tools are pinned so a lint result means the same thing on every machine.
STATICCHECK_VERSION := 2025.1.1
TOOLS_BIN           := $(shell go env GOPATH)/bin

# The community XML and the local archive are not ours to lint or format.
GO_PKGS := ./cmd/... ./internal/...

.PHONY: help deps build test test-race cover lint fmt tools deploy run-remote probe verify-write clean

help:
	@echo "deps         fetch the community XML into third_party/"
	@echo "build        build the binary for the Linux amd64 target"
	@echo "test         run the unit tests"
	@echo "test-race    run the tests under the race detector"
	@echo "cover        report test coverage per package"
	@echo "lint         gofmt + go vet + staticcheck"
	@echo "fmt          rewrite the sources with gofmt"
	@echo "deploy       send binary + config + overrides to $(TARGET_HOST)"
	@echo "run-remote   run the binary on the target"
	@echo "probe        show the serial adapter on the target (read-only)"
	@echo "verify-write exercise the write path without changing any value"

deps:
	@./scripts/bootstrap.sh

# Installs the pinned lint tooling. Separate from `lint` so a build without
# network access still runs the checks that need no download.
tools:
	go install honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION)

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o $(BIN) $(PKG)
	@ls -lh $(BIN)

test:
	go test $(GO_PKGS)

# The daemon runs several refresh groups in parallel over one serial port, so
# data races are a realistic failure mode rather than a theoretical one.
test-race:
	go test -race $(GO_PKGS)

cover:
	@go test $(GO_PKGS) -coverprofile=coverage.out >/dev/null
	@go tool cover -func=coverage.out | tail -1
	@go test $(GO_PKGS) -cover 2>&1 | grep -E 'coverage|no test files'
	@echo "detail: go tool cover -html=coverage.out"

fmt:
	gofmt -w cmd internal

lint:
	@echo "== gofmt =="
	@test -z "$$(gofmt -l cmd internal)" || { gofmt -l cmd internal; echo "run: make fmt"; exit 1; }
	@echo "ok"
	@echo "== go vet =="
	go vet $(GO_PKGS)
	@echo "== staticcheck =="
	@command -v $(TOOLS_BIN)/staticcheck >/dev/null || { echo "missing; run: make tools"; exit 1; }
	$(TOOLS_BIN)/staticcheck $(GO_PKGS)

deploy: build
	ssh $(TARGET_HOST) 'mkdir -p $(TARGET_DIR)/third_party'
	rsync -az --delete third_party/vcontrold/xml $(TARGET_HOST):$(TARGET_DIR)/third_party/vcontrold/
	rsync -az overrides $(TARGET_HOST):$(TARGET_DIR)/
	rsync -az $(BIN) config-openv.yaml $(TARGET_HOST):$(TARGET_DIR)/
	@echo "deployed to $(TARGET_HOST):$(TARGET_DIR)"

run-remote:
	ssh -t $(TARGET_HOST) 'cd $(TARGET_DIR) && ./$(BIN) --yaml-config config-openv.yaml'

probe:
	ssh $(TARGET_HOST) 'ls -l /dev/serial/by-id/ 2>/dev/null || ls -l /dev/ttyUSB*'
	@echo "--- adapter shown above; the daemon must be stopped to use the port ---"
	@echo "  systemctl --user stop vcontrold-go"
	@echo "  ./$(BIN) --yaml-config config-openv.yaml -address 0x08A7"

# Reads the setpoint, writes it back unchanged, checks the 00 status, reads
# again. Proves the write path without altering the boiler.
verify-write:
	ssh -t $(TARGET_HOST) 'cd $(TARGET_DIR) && ./$(BIN) --yaml-config config-openv.yaml -verify-write $(ADDR)'

clean:
	rm -f $(BIN) config_parser coverage.out
