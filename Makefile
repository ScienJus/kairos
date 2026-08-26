WEB_DIR := web
BIN_DIR := bin
GO_PACKAGES := ./cmd/... ./internal/... ./web

.PHONY: build test quickstart web-build web-test go-test go-vet release-notices

build: web-build
	mkdir -p $(BIN_DIR)
	go build -tags webdist -o $(BIN_DIR)/kairos-server ./cmd/kairos-server

test: web-test go-test

quickstart: build
	./examples/quickstart/run.sh

web-build: $(WEB_DIR)/node_modules/.package-lock.json
	cd $(WEB_DIR) && npm run build

web-test: $(WEB_DIR)/node_modules/.package-lock.json
	cd $(WEB_DIR) && npm test -- --run
	cd $(WEB_DIR) && npm run lint
	cd $(WEB_DIR) && npm run build
	go test -tags webdist ./web

go-test:
	go test $(GO_PACKAGES)

go-vet:
	go vet $(GO_PACKAGES)

release-notices: $(WEB_DIR)/node_modules/.package-lock.json
	node scripts/generate-third-party-notices.mjs

$(WEB_DIR)/node_modules/.package-lock.json: $(WEB_DIR)/package.json $(WEB_DIR)/package-lock.json
	cd $(WEB_DIR) && npm ci --ignore-scripts
