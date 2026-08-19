.PHONY: build test lint run

build:
	go build -o bin/voice ./cmd/voice

test:
	go test ./...

lint:
	gofmt -l .
	go vet ./...

run: build
	./bin/voice
