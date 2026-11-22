CGO_ENABLED ?= 0
BINARY ?= bin/mob3
VERSION ?= dev
FUZZTIME ?= 60s

.PHONY: all build static test fuzz vet vendor hooks clean run docker

all: build

build:
	mkdir -p $(dir $(BINARY))
	CGO_ENABLED=$(CGO_ENABLED) go build -mod=vendor -trimpath \
		-ldflags="-s -w -X browser/internal/app.Version=$(VERSION)" \
		-o $(BINARY) ./cmd/mob3

static:
	mkdir -p $(dir $(BINARY))
	CGO_ENABLED=0 go build -mod=vendor -trimpath \
		-ldflags="-s -w -X browser/internal/app.Version=$(VERSION)" \
		-o $(BINARY) ./cmd/mob3

test:
	go test -mod=vendor ./...

fuzz:
	go test -mod=vendor -fuzz=FuzzDecodeCommand -fuzztime=$(FUZZTIME) ./internal/protocol
	go test -mod=vendor -fuzz=FuzzDecodeEvent -fuzztime=$(FUZZTIME) ./internal/protocol

vet:
	go vet -mod=vendor ./...

vendor:
	go mod vendor

hooks:
	git config core.hooksPath .githooks

run: build
	./$(BINARY)

docker:
	docker build -t mob3 .

clean:
	rm -rf bin
