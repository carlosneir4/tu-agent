.PHONY: build test lint vet fmt clean

build:
	go build -o bin/tu-agent ./cmd/tu-agent

test:
	go test -race -coverprofile=coverage.out ./internal/...
	go tool cover -func=coverage.out

lint:
	golangci-lint run ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

clean:
	rm -rf bin/ coverage.out

.DEFAULT_GOAL := build
