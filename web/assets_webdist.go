//go:build webdist

package webui

import "embed"

//go:embed dist
var assets embed.FS

const assetRoot = "dist"
