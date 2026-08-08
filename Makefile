# Surchargeables : make deploy TARGET_HOST=user@host
# Les valeurs reelles sont dans .env, non versionne.
-include .env
export

TARGET_HOST ?= user@boiler-host.local
TARGET_DIR  ?= /opt/vcontrold-go
BIN         := vcontrold-go
PKG         := ./cmd/vcontrold-go

.PHONY: help deps build test lint deploy run-remote probe clean

help:
	@echo "deps        récupère les XML communautaires dans third_party/"
	@echo "build       compile le binaire pour la cible Linux amd64"
	@echo "test        lance les tests unitaires"
	@echo "deploy      envoie binaire + config sur $(TARGET_HOST)"
	@echo "run-remote  exécute le binaire sur la cible"
	@echo "probe       lit les compteurs de brûleur sur la cible (lecture seule)"

deps:
	@./scripts/bootstrap.sh

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o $(BIN) $(PKG)
	@ls -lh $(BIN)

test:
	go test ./... 2>&1 | tail -30

lint:
	gofmt -l . | grep -v '^third_party/' || echo "gofmt : ok"
	go vet ./... || true

deploy: build
	ssh $(TARGET_HOST) 'mkdir -p $(TARGET_DIR)/third_party'
	rsync -az --delete third_party/vcontrold/xml $(TARGET_HOST):$(TARGET_DIR)/third_party/vcontrold/
	rsync -az overrides $(TARGET_HOST):$(TARGET_DIR)/
	rsync -az $(BIN) config-openv.yaml $(TARGET_HOST):$(TARGET_DIR)/
	@echo "déployé sur $(TARGET_HOST):$(TARGET_DIR)"

run-remote:
	ssh -t $(TARGET_HOST) 'cd $(TARGET_DIR) && ./$(BIN) --yaml-config config-openv.yaml'

# Lecture seule : les deux compteurs qui manquent au dossier énergie.
probe:
	ssh $(TARGET_HOST) 'ls -l /dev/serial/by-id/ 2>/dev/null || ls -l /dev/ttyUSB*'
	@echo "--- adaptateur détecté ci-dessus ; lancer ensuite : ---"
	@echo "  make run-remote     puis, dans une autre session :"
	@echo "  vclient -h localhost:3002 -c getBrennerStunden1"
	@echo "  vclient -h localhost:3002 -c getBrennerStarts"

clean:
	rm -f $(BIN) config_parser coverage.out
