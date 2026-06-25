.PHONY: build build-agent build-gateway build-backend \
       test test-integration lint vet \
       docker-up docker-down docker-build \
       gen-certs migrate clean \
       build-desktop-darwin \
       package-macos package-macos-desktop package-windows package-device package-device-config \
       package-browser-extensions check-packext-drift render-browser-policies package-customer-delivery

# ──────────────────────────────────────────────────────────────────────────────
# Build
# ──────────────────────────────────────────────────────────────────────────────

build: build-agent build-gateway build-backend

build-agent:
	go build -o bin/agent ./cmd/agent

build-agent-darwin:
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 go build -buildvcs=false -o bin/agent-darwin-arm64 ./cmd/agent

build-agent-windows:
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -o bin/agent-windows-amd64.exe ./cmd/agent

build-gateway:
	cd gateway && go build -o ../bin/gateway ./cmd/gateway

build-backend:
	cd backend && go build -o ../bin/backend ./cmd/backend


# ──────────────────────────────────────────────────────────────────────────────
# Test
# ──────────────────────────────────────────────────────────────────────────────

test:
	go test ./... -count=1
	cd gateway && go test ./... -count=1
	cd backend && go test ./... -count=1

test-integration:
	go test ./test/integration/... -v -count=1

# ──────────────────────────────────────────────────────────────────────────────
# Lint / Vet
# ──────────────────────────────────────────────────────────────────────────────

vet:
	go vet ./...
	cd gateway && go vet ./...
	cd backend && go vet ./...

lint: vet
	@command -v staticcheck >/dev/null 2>&1 || { echo "install staticcheck: go install honnef.co/go/tools/cmd/staticcheck@latest"; exit 1; }
	staticcheck ./...
	cd gateway && staticcheck ./...
	cd backend && staticcheck ./...

# ──────────────────────────────────────────────────────────────────────────────
# Docker
# ──────────────────────────────────────────────────────────────────────────────

docker-build:
	docker compose build

docker-up:
	docker compose up -d

docker-down:
	docker compose down

# ──────────────────────────────────────────────────────────────────────────────
# Desktop App & Installer
# ──────────────────────────────────────────────────────────────────────────────

build-desktop:
	cd desktop && wails build -platform windows/amd64

build-desktop-darwin:
	cd desktop && GOARCH=arm64 CGO_ENABLED=1 wails build -platform darwin/arm64

build-desktop-dev:
	cd desktop && wails dev

build-installer:
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -o cmd/installer/assets/themisto-agent.exe ./cmd/agent
	cd desktop && wails build -platform windows/amd64 -o ../cmd/installer/assets/themisto-desktop.exe
	cp desktop/build/cmd/installer/assets/themisto-desktop.exe cmd/installer/assets/themisto-desktop.exe
	go run ./cmd/packext
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -ldflags="-H=windowsgui" -o dist/ThemistoSetup.exe ./cmd/installer
	cp dist/ThemistoSetup.exe dist/ThemistoUninstall.exe

install-wails:
	go install github.com/wailsapp/wails/v2/cmd/wails@latest

check-packext-drift:
	go run ./cmd/packext
	git diff --exit-code -- cmd/installer/assets/extensions
	test -z "$$(git status --porcelain --untracked-files=all -- cmd/installer/assets/extensions)"

# ──────────────────────────────────────────────────────────────────────────────
# Installer Packaging
# ──────────────────────────────────────────────────────────────────────────────

package-macos:
	bash scripts/build-macos-installer.sh

package-macos-desktop:
	bash scripts/build-macos-desktop-installer.sh \
		$(if $(VERSION),$(VERSION),0.1.0) \
		$(if $(OUT_DIR),$(OUT_DIR),dist/installers) \
		$(if $(CONFIG_FILE),--config-file "$(CONFIG_FILE)",) \
		$(if $(SIGN_IDENTITY),--sign "$(SIGN_IDENTITY)",) \
		$(if $(NOTARIZE),--notarize --apple-id "$(APPLE_ID)" --team-id "$(TEAM_ID)" --app-password "$(APP_PASSWORD)",)

package-windows:
	bash scripts/build-windows-bundle.sh

package-device:
	bash scripts/build-device-installers.sh \
		--device-id "$(DEVICE_ID)" \
		--org-name "$(ORG_NAME)" \
		--token "$(ENROLLMENT_TOKEN)" \
		--backend-url "$(BACKEND_URL)" \
		--gateway-url "$(GATEWAY_URL)" \
		$(if $(VERSION),--version "$(VERSION)",)

package-device-config:
	bash scripts/build-device-installers.sh \
		--config-file "$(CONFIG_FILE)" \
		$(if $(DEVICE_ID),--device-id "$(DEVICE_ID)",) \
		$(if $(VERSION),--version "$(VERSION)",)

package-browser-extensions:
	bash scripts/build-browser-extensions.sh \
		$(if $(VERSION),--version "$(VERSION)",)

render-browser-policies:
	bash scripts/render-browser-policies.sh \
		--chrome-extension-id "$(CHROME_EXTENSION_ID)" \
		--firefox-install-url "$(FIREFOX_INSTALL_URL)" \
		$(if $(CHROME_UPDATE_URL),--chrome-update-url "$(CHROME_UPDATE_URL)",) \
		$(if $(FIREFOX_EXTENSION_ID),--firefox-extension-id "$(FIREFOX_EXTENSION_ID)",)

package-customer-delivery:
	bash scripts/build-customer-delivery-bundle.sh \
		$(if $(VERSION),--version "$(VERSION)",) \
		$(if $(OUT_DIR),--out-dir "$(OUT_DIR)",) \
		$(if $(CONFIG_FILE),--config-file "$(CONFIG_FILE)",) \
		$(if $(DEVICE_ID),--device-id "$(DEVICE_ID)",) \
		$(if $(ORG_NAME),--org-name "$(ORG_NAME)",) \
		$(if $(ENROLLMENT_TOKEN),--token "$(ENROLLMENT_TOKEN)",) \
		$(if $(BACKEND_URL),--backend-url "$(BACKEND_URL)",) \
		$(if $(GATEWAY_URL),--gateway-url "$(GATEWAY_URL)",) \
		--chrome-extension-id "$(CHROME_EXTENSION_ID)" \
		--firefox-install-url "$(FIREFOX_INSTALL_URL)" \
		$(if $(CHROME_UPDATE_URL),--chrome-update-url "$(CHROME_UPDATE_URL)",) \
		$(if $(FIREFOX_EXTENSION_ID),--firefox-extension-id "$(FIREFOX_EXTENSION_ID)",)

# ──────────────────────────────────────────────────────────────────────────────
# Certificates
# ──────────────────────────────────────────────────────────────────────────────

gen-certs:
	bash scripts/gen-ca.sh
	bash scripts/gen-server-cert.sh

# ──────────────────────────────────────────────────────────────────────────────
# Database Migrations
# ──────────────────────────────────────────────────────────────────────────────

MIGRATE_DSN ?= postgres://themisto:$(POSTGRES_PASSWORD)@localhost:5432/themisto?sslmode=disable

migrate:
	@command -v migrate >/dev/null 2>&1 || { echo "install golang-migrate: go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest"; exit 1; }
	migrate -path db/migrations -database "$(MIGRATE_DSN)" up

migrate-down:
	migrate -path db/migrations -database "$(MIGRATE_DSN)" down 1

migrate-status:
	migrate -path db/migrations -database "$(MIGRATE_DSN)" version

# ──────────────────────────────────────────────────────────────────────────────
# Clean
# ──────────────────────────────────────────────────────────────────────────────

clean:
	rm -rf bin/

