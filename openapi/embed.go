// Package openapi embeds the API contract so the running server can publish it without
// reading from disk.
package openapi

import _ "embed"

// Document is the OpenAPI specification that this repository generates both its Go server
// interface and its TypeScript client from.
//
// It is embedded rather than read from "./openapi/openapi.yaml", because that path resolves
// against the process working directory. Serving it from disk meant the endpoint that
// publishes the contract worked under `go run` from this directory and returned a
// plain-text 404 from anywhere else — a container, systemd, or a compiled binary started in
// another directory — while every other route kept answering normally. The failure
// therefore looked like a missing endpoint rather than a missing file.
//
// This is the same defect class as the migration source (see migrations/embed.go), and it
// is fixed the same way: the asset travels inside the binary.
//
//go:embed openapi.yaml
var Document []byte
