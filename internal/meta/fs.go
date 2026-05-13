package meta

import (
	"embed"
	"io/fs"
)

// Metadata files are embedded from the bce-openapi-meta git submodule,
// which lives at internal/meta/bce-openapi-meta/ relative to the repo root.
//
//go:embed bce-openapi-meta/schema bce-openapi-meta/i18n
var rawFS embed.FS

// dataFS strips the "bce-openapi-meta" prefix so all paths start with
// "schema/" or "i18n/", matching every ReadFile call in reader.go.
var dataFS = func() fs.FS {
	sub, err := fs.Sub(rawFS, "bce-openapi-meta")
	if err != nil {
		panic(err)
	}
	return sub
}()
