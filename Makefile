PROJECT_DIR = $(shell pwd)
PROJECT_BIN = $(PROJECT_DIR)/bin
$(shell [ -f bin ] || mkdir -p $(PROJECT_BIN))
PATH := $(PROJECT_BIN):$(PATH)

GOLANGCI_LINT = $(PROJECT_BIN)/golangci-lint

.PHONY: .install-linter
.install-linter:
	[ -f $(PROJECT_BIN)/golangci-lint ] || curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(PROJECT_BIN) v1.46.2

.PHONY: lint
lint: .install-linter
	golangci-lint run ./...

.PHONY: tests
tests:
	go test ./...

.PHONY: loadgen
loadgen:
	go run ./build-tools/loadgen.go -brokers localhost:9092 -topic raw-swaps -rps 500 -duration 30s

topic ?= raw-swaps
brokers ?= localhost:9092

create-topic:
	./infra/kafka/create_topic.sh $(topic) $(brokers)

loadgen:
	go run ./build-tools/loadgen.go -brokers $(brokers) -topic $(topic) -rps 500 -duration 30s
