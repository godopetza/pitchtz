package docs

import _ "embed"

// ClientOpenAPI contains only the public and player-facing mobile contract.
// Admin and owner endpoints are intentionally excluded.
//
//go:embed openapi.yaml
var ClientOpenAPI []byte
