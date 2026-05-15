.PHONY: build-all docker-up clean sync-install-scripts

sync-install-scripts:
	cp scripts/install_linux.sh internal/serverinstall/install_linux.sh
	cp scripts/install_windows.ps1 internal/serverinstall/install_windows.ps1

# Production-like local tree: dashboard dist + cross-built agents + server with embedbins.
build-all: sync-install-scripts
	cd web && npm ci && npm run build
	rm -rf internal/serverinstall/dashboard
	cp -r web/dist internal/serverinstall/dashboard
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o internal/serverinstall/arx-agent-linux ./cmd/agent
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o internal/serverinstall/arx-agent-windows.exe ./cmd/agent
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -tags embedbins -o bin/arx-server ./cmd/server

docker-up:
	docker compose up -d --build

clean:
	rm -rf bin web/dist internal/serverinstall/dashboard internal/serverinstall/arx-agent-linux internal/serverinstall/arx-agent-windows.exe
