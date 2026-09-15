package main

import "embed"

// Wails uses Go's `embed` package to embed the frontend files into the binary.
// Any files in the frontend/dist folder will be embedded into the binary and
// made available to the frontend. The desktop app and the web version serve the
// same files. See https://pkg.go.dev/embed for more information.
//
//go:embed all:frontend/dist
var assets embed.FS
