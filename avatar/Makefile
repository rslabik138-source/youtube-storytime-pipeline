.PHONY: build test lint run

build:
	go build -o bin/avatar ./cmd/avatar

test:
	go test ./...

lint:
	gofmt -l .
	go vet ./...

run: build
	./bin/avatar
