.PHONY: build test lint run

build:
	go build -o bin/compose ./cmd/compose

test:
	go test ./...

lint:
	gofmt -l .
	go vet ./...

run: build
	./bin/compose
