.PHONY: build-all build-web build-server build-agent-linux build-agent-windows build-agent-android clean sync-install-scripts

BUILD_DIR := build
GOFLAGS := -trimpath -ldflags="-s -w"
AGENT_PKG := ./cmd/agent
SERVER_PKG := ./cmd/server
GOMOBILE_PKG := ./mobile/agentbind
ANDROID_DIR := mobile/android
ANDROID_LIBS := $(ANDROID_DIR)/app/libs
AAR_OUT := $(ANDROID_LIBS)/agentbind.aar

sync-install-scripts:
	cp scripts/install_linux.sh internal/serverinstall/install_linux.sh
	cp scripts/install_windows.ps1 internal/serverinstall/install_windows.ps1

$(BUILD_DIR):
	mkdir -p $(BUILD_DIR)

build-web:
	cd web && npm install && npm run build
	rm -rf internal/api/webui/dist
	mkdir -p internal/api/webui/dist
	cp -r web/dist/. internal/api/webui/dist/

build-server: sync-install-scripts build-web $(BUILD_DIR)
	CGO_ENABLED=0 go build $(GOFLAGS) -tags embedui -o $(BUILD_DIR)/arx-server $(SERVER_PKG)

build-agent-linux: $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -o $(BUILD_DIR)/arx-agent-linux $(AGENT_PKG)

build-agent-windows: $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(GOFLAGS) -o $(BUILD_DIR)/arx-agent-windows.exe $(AGENT_PKG)

$(AAR_OUT): $(GOMOBILE_PKG)/*.go
	@if [ -z "$$ANDROID_HOME" ]; then echo "ANDROID_HOME is not set; run scripts/setup_wsl_android.sh first" >&2; exit 1; fi
	@command -v gomobile >/dev/null 2>&1 || { echo "gomobile not in PATH; run scripts/setup_wsl_android.sh" >&2; exit 1; }
	mkdir -p $(ANDROID_LIBS)
	gomobile bind -target=android -o $(AAR_OUT) $(GOMOBILE_PKG)

build-agent-android: $(AAR_OUT)
	@if [ -z "$$ANDROID_HOME" ]; then echo "ANDROID_HOME is not set" >&2; exit 1; fi
	cd $(ANDROID_DIR) && ./gradlew assembleRelease
	@mkdir -p $(BUILD_DIR)
	@APK=$$(find $(ANDROID_DIR)/app/build/outputs/apk/release -name '*.apk' | head -n1); \
	if [ -z "$$APK" ]; then echo "release APK not found under $(ANDROID_DIR)/app/build/outputs/apk/release" >&2; exit 1; fi; \
	cp "$$APK" $(BUILD_DIR)/arx-agent.apk

build-all: sync-install-scripts build-server build-agent-linux build-agent-windows

clean:
	rm -rf $(BUILD_DIR) $(ANDROID_LIBS)/agentbind.aar $(ANDROID_DIR)/app/build
