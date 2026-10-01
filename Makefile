.PHONY: build test check

build:
	go build -o bin/sbx-herdr ./cmd/sbx-herdr

test:
	go test -race ./...

check:
	go vet ./...
	test -z "$$(gofmt -l cmd internal)"
