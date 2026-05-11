package meta

import bcemeta "github.com/baidubce/bce-openapi-meta"

// dataFS is the embedded filesystem from the bce-openapi-meta module.
// Paths inside start with "schema/" and "i18n/".
var dataFS = bcemeta.FS
