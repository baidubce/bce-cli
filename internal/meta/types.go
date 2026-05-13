package meta

// ProductList is the parsed form of schema/products.json.
type ProductList struct {
	Products []Product `json:"products"`
}

// Product holds a service code, supported protocols, and its region-to-endpoint mapping.
type Product struct {
	Code     string            `json:"code"`
	Protocol string            `json:"protocol"` // e.g. "HTTP", "HTTPS", "HTTP|HTTPS"
	Endpoint map[string]string `json:"endpoint"`
}

// PaginatorSpec describes how to iterate a paginated API.
type PaginatorSpec struct {
	InputToken    string `json:"input_token"`    // request param for page cursor, e.g. "marker"
	OutputToken   string `json:"output_token"`   // response field for next cursor, e.g. "nextMarker"
	TruncationKey string `json:"truncation_key"` // response boolean field for more pages, e.g. "isTruncated"
	ResultKey     string `json:"result_key"`     // response array field to merge, e.g. "vpcs"
	LimitKey      string `json:"limit_key"`      // request param for page size, e.g. "maxKeys"
}

// Api describes a single API action.
// In v2 metadata each schema/{service}/{ApiName}.json file is parsed directly as one Api.
type Api struct {
	Name       string         `json:"name"`
	Method     string         `json:"method"`
	URI        string         `json:"uri"`
	DescKey    string         `json:"desc_key"`
	Parameters []Parameter    `json:"parameters"`
	Paginator  *PaginatorSpec `json:"paginator,omitempty"`
}

// Parameter describes one request parameter.
type Parameter struct {
	Name          string      `json:"name"`
	Type          string      `json:"type"`              // String / Integer / Boolean / List / Object
	Required      bool        `json:"required"`
	Position      string      `json:"position"`          // URL / Query / Body / Header
	Default       any         `json:"default,omitempty"` // default value applied when param is not provided
	DescKey       string      `json:"desc_key"`
	SubParameters []Parameter `json:"sub_parameters,omitempty"`
}

// I18nFile holds merged translations for all APIs in a service.
// Built by merging every i18n/{lang}/{service}/{ApiName}.json file (excluding version.json).
type I18nFile struct {
	Apis map[string]map[string]string // ApiName -> {flatField -> text}
}

// CommonI18n is the structure of i18n/{lang}/common.json.
type CommonI18n map[string]string

// VersionFile is the parsed form of i18n/{lang}/{service}/version.json.
// It provides product-level metadata and a summary index of all APIs.
type VersionFile struct {
	Version     string                    `json:"version"`
	Style       string                    `json:"style"` // "rpc" or "rest"
	Description string                    `json:"description"`
	Apis        map[string]VersionApiMeta `json:"apis"`
}

// VersionApiMeta holds the title and summary for a single API entry in version.json.
type VersionApiMeta struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
}
