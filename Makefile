.PHONY: build test check run

build:
	go build ./cmd/hrdx

test:
	go test ./...

check:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')
	go vet ./...
	go test ./...

run:
	go run ./cmd/hrdx
