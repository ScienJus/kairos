//go:build !webdist

package webui

import "embed"

// The default build keeps Go tooling independent from generated frontend files.
// Use `make build` to embed the production Vite bundle.
//
//go:embed index.html
var assets embed.FS

const assetRoot = "."
