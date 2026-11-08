CGO_ENABLED ?= 0
BINARY ?= bin/mob3
VERSION ?= dev

.PHONY: all build static test vet vendor clean run docker

all: build

build:
	mkdir -p $(dir $(BINARY))
	CGO_ENABLED=$(CGO_ENABLED) go build -mod=vendor -trimpath \
		-ldflags="-s -w -X browser/internal/browser.Version=$(VERSION)" \
		-o $(BINARY) ./cmd/mob3

static:
	mkdir -p $(dir $(BINARY))
	CGO_ENABLED=0 go build -mod=vendor -trimpath \
		-ldflags="-s -w -X browser/internal/browser.Version=$(VERSION)" \
		-o $(BINARY) ./cmd/mob3

test:
	go test -mod=vendor ./...

vet:
	go vet -mod=vendor ./...

vendor:
	go mod vendor

run: build
	./$(BINARY)

docker:
	docker build -t mob3 .

clean:
	rm -rf bin
