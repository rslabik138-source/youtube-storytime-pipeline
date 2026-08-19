.PHONY: build test vet fmt lint run tidy clean

build:
	go build -o bin/gen ./cmd/gen

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

lint: vet
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || echo "golangci-lint not installed; ran go vet only"

run: build
	./bin/gen generate

clean:
	rm -rf bin

tidy:
	go mod tidy
