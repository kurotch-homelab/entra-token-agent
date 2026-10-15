.PHONY: build tray test check secrets

build:
	go build -trimpath -ldflags "-s -w" ./cmd/entra-token-agent

tray:
	GOOS=windows CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H windowsgui" -o dist/entra-token-agent-tray.exe ./cmd/entra-token-agent-tray

test:
	go test ./...

check:
	go vet ./...
	go test -race ./...

secrets:
	gitleaks git --log-opts="--all" --redact=100 --no-banner .
	gitleaks dir --redact=100 --no-banner .
