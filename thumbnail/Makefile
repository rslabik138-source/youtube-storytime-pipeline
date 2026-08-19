.PHONY: build test lint run

build:
	go build -o bin/thumb ./cmd/thumb

test:
	go test ./...

lint:
	gofmt -l .
	go vet ./...

run: build
	./bin/thumb
