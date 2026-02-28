package webassets

import "embed"

// Dist contains production SPA files.
//
//go:embed dist
var Dist embed.FS
