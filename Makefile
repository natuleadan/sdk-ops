.PHONY: all build lint test clean install cross third-party

BINARY = sdk-ops
VERSION = $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

all: build lint

build: third-party
	go build -ldflags="-s -w -X main.version=$(VERSION)" -o $(BINARY) ./cmd/sdk-ops/

lint:
	golangci-lint run --timeout=5m ./...
	go vet ./...

test:
	go test -race -count=1 ./...

test-integration: build
	npx bats tests/ 2>/dev/null || (npm install -g bats-core 2>/dev/null; bats tests/)

clean:
	rm -f $(BINARY)
	go clean -cache -testcache

install: build
	install -m 755 $(BINARY) /usr/local/bin/

cross:
	GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o $(BINARY)-linux-amd64 ./cmd/sdk-ops/
	GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o $(BINARY)-darwin-amd64 ./cmd/sdk-ops/
	GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o $(BINARY)-darwin-arm64 ./cmd/sdk-ops/
	GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o $(BINARY)-windows-amd64.exe ./cmd/sdk-ops/

third-party:
	@bash .github/scripts/generate-third-party.sh
