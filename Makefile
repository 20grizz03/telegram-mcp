BIN := bin/tgmcp
PKG := ./cmd/tgmcp

.PHONY: build test fmt vet dist clean

build: ## build the local binary
	go build -o $(BIN) $(PKG)

test: ## run tests
	go test ./...

fmt: ## format
	gofmt -w .

vet: ## vet
	go vet ./...

## dist: cross-compile release binaries for all platforms (pure Go, no cgo)
dist:
	CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -o dist/tgmcp-macos-arm64   $(PKG)
	CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 go build -o dist/tgmcp-macos-amd64   $(PKG)
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -o dist/tgmcp-linux-amd64   $(PKG)
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -o dist/tgmcp-linux-arm64   $(PKG)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o dist/tgmcp-win-amd64.exe $(PKG)
	@echo "binaries in dist/"

clean:
	rm -rf bin dist
