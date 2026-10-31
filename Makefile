BINARY := canirun
PKG    := ./cmd/canirun

.PHONY: build test lint clean

build:
	go build -o bin/$(BINARY) $(PKG)

test:
	go test -race ./...

lint:
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed on:"; gofmt -l .; exit 1)

clean:
	rm -rf bin dist
