SHELL := /bin/sh
.DEFAULT_GOAL := help
ROOT := $(CURDIR)
export PATH := $(ROOT)/bin:$(PATH)
export GOBIN := $(ROOT)/bin
COMPOSE := $(shell docker compose version >/dev/null 2>&1 && echo 'docker compose' || echo docker-compose)
DC := $(COMPOSE) --env-file .env -f deploy/docker-compose.yml
SERVICE ?= api-gateway
SERVICES := api-gateway payment-core chain-worker webhook-sender crypto-vault
BUF_BASE ?= .git#branch=main

.PHONY: help bootstrap env tools generate proto sql mocks swagger fmt test lint vet check build run up down logs ps apps stop-apps smoke topics migrate migrate-down compose-check breaking doctor
help:
	@echo 'make bootstrap       Install pinned tools, dependencies, generate code'
	@echo 'make up              Start PostgreSQL and Kafka (Docker required)'
	@echo 'make migrate         Apply payment-core migrations'
	@echo 'make topics          Create local event topics'
	@echo 'make run SERVICE=... Run a service on the host'
	@echo 'make apps            Build and start all bootstrap services in Docker'
	@echo 'make check           Verify formatting, tests, vet, lint, proto and Compose'
	@echo 'make generate        Generate protobuf, sqlc, mocks, Swagger'
	@echo 'make down            Stop containers; keep local data volumes'
	@echo 'make doctor          Inspect local tool and daemon availability'
env:
	@test -f .env || cp .env.example .env
bootstrap: env tools
	go mod download
	$(MAKE) generate
	go mod tidy
tools:
	go install github.com/bufbuild/buf/cmd/buf@v1.50.0
	CGO_ENABLED=0 go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.28.0
	go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.5
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
	GOTOOLCHAIN=go1.23.12 go install go.uber.org/mock/mockgen@v0.5.0
	GOTOOLCHAIN=go1.23.12 go install github.com/swaggo/swag/cmd/swag@v1.16.4
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.5.0
	go install github.com/pressly/goose/v3/cmd/goose@v3.24.1
generate: proto sql mocks swagger
proto:
	$(GOBIN)/buf lint
	$(GOBIN)/buf generate
sql:
	$(GOBIN)/sqlc generate
mocks:
	go generate ./api-gateway/internal/domain/... ./payment-core/internal/domain/... ./chain-worker/internal/domain/... ./webhook-sender/internal/domain/... ./crypto-vault/internal/domain/...
swagger:
	$(GOBIN)/swag init -g main.go -d api-gateway/cmd/server,api-gateway/internal/delivery/http --output api-gateway/internal/delivery/http/docs --parseInternal
fmt:
	gofmt -w api-gateway payment-core chain-worker webhook-sender crypto-vault pkg
test:
	go test -race -count=1 ./...
vet:
	go vet ./...
lint:
	$(GOBIN)/golangci-lint run --timeout=5m
check: test vet lint compose-check
	@test -z "$$(gofmt -l api-gateway payment-core chain-worker webhook-sender crypto-vault pkg)" || (echo 'Run make fmt'; exit 1)
	$(GOBIN)/buf lint
build:
	@mkdir -p bin
	@set -e; for service in $(SERVICES); do go build -o bin/$$service ./$$service/cmd/server; done
run:
	@case ' $(SERVICES) ' in *' $(SERVICE) '*) ;; *) echo 'Unknown SERVICE'; exit 1;; esac
	go run ./$(SERVICE)/cmd/server
up: env
	$(DC) up -d --wait postgres kafka
down: env
	$(DC) --profile apps down
apps: env
	$(DC) --profile apps up -d --build --wait
smoke: env
	./deploy/smoke.sh
stop-apps: env
	$(DC) --profile apps stop $(SERVICES)
logs: env
	$(DC) --profile apps logs -f
ps: env
	$(DC) --profile apps ps
topics: env
	$(DC) run --rm kafka-init
migrate: env
	@set -a; . ./.env; set +a; "$(GOBIN)/goose" -dir deploy/migrations/payment-core postgres "$$DATABASE_URL" up
migrate-down: env
	@set -a; . ./.env; set +a; "$(GOBIN)/goose" -dir deploy/migrations/payment-core postgres "$$DATABASE_URL" down
compose-check:
	$(COMPOSE) --env-file .env.example -f deploy/docker-compose.yml --profile apps config --quiet
breaking:
	$(GOBIN)/buf breaking --against '$(BUF_BASE)'
doctor:
	go version
	@for tool in buf sqlc mockgen swag golangci-lint goose; do command -v $$tool || true; done
	$(COMPOSE) version
	docker info --format '{{.ServerVersion}}'
