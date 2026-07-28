.PHONY: build test check

build:
	go build -trimpath -ldflags "-s -w" ./cmd/entra-token-agent

test:
	go test ./...

check:
	go vet ./...
	go test -race ./...
