// Package web embeds the server-rendered console: html/template pages and the static assets (synced design tokens,
// hash-pinned htmx, the passkey and keyboard scripts). Nothing is fetched from a third party at run time (FR-039).
package web

import "embed"

//go:embed templates static
var FS embed.FS
