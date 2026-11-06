package llmlauncher

import _ "embed"

//go:embed catalog.json
var embeddedCatalog string

func EmbeddedCatalog() string { return embeddedCatalog }

//go:embed catalog.pub
var catalogPublicKey string

func CatalogPublicKey() string { return catalogPublicKey }
