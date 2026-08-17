WEB_DIR := web
BIN_DIR := bin

.PHONY: build test web-build web-test go-test

build: web-build
	mkdir -p $(BIN_DIR)
	go build -tags webdist -o $(BIN_DIR)/kairos-server ./cmd/kairos-server

test: web-test go-test

web-build: $(WEB_DIR)/node_modules/.package-lock.json
	cd $(WEB_DIR) && npm run build

web-test: $(WEB_DIR)/node_modules/.package-lock.json
	cd $(WEB_DIR) && npm test -- --run
	cd $(WEB_DIR) && npm run lint
	cd $(WEB_DIR) && npm run build
	go test -tags webdist ./web

go-test:
	go test ./...

$(WEB_DIR)/node_modules/.package-lock.json: $(WEB_DIR)/package.json $(WEB_DIR)/package-lock.json
	cd $(WEB_DIR) && npm ci
