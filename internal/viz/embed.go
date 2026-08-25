// Package viz embeds the mneme-viz single-page dashboard and exposes it as an
// embed.FS for the mneme-viz binary.
package viz

import "embed"

// FS holds the self-contained dashboard (index.html with inline CSS/JS).
//
//go:embed index.html
var FS embed.FS

// Index returns the embedded dashboard HTML.
func Index() ([]byte, error) {
	return FS.ReadFile("index.html")
}
