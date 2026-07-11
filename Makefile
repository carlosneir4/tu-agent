.PHONY: build test lint vet fmt clean

build:
	go build -tags sqlite_fts5 -o bin/tu-agent ./cmd/tu-agent

test:
	go test -race -tags sqlite_fts5 -coverprofile=coverage.out ./internal/... ./cmd/...
	go tool cover -func=coverage.out

lint:
	golangci-lint run --build-tags sqlite_fts5 ./...

vet:
	go vet -tags sqlite_fts5 ./...

fmt:
	gofmt -w .

clean:
	rm -rf bin/ coverage.out

.DEFAULT_GOAL := build
