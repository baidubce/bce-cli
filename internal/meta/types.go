package meta

import "encoding/json"

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

// Module is the parsed form of a single schema/{service}/{module}.json file.
type Module struct {
	Service string `json:"service"`
	Module  string `json:"module"`
	Apis    []Api  `json:"apis"`
}

// Api describes a single API action.
type Api struct {
	Name       string      `json:"name"`
	Method     string      `json:"method"`
	URI        string      `json:"uri"`
	DescKey    string      `json:"desc_key"`
	Parameters []Parameter `json:"parameters"`
}

// Parameter describes one request parameter.
type Parameter struct {
	Name          string      `json:"name"`
	Type          string      `json:"type"`              // String / Integer / Boolean / List / Object
	Required      bool        `json:"required"`
	Position      string      `json:"position"`          // URL / Query / Body / Header
	Default       string      `json:"default,omitempty"` // default value applied when param is not provided
	DescKey       string      `json:"desc_key"`
	SubParameters []Parameter `json:"sub_parameters,omitempty"`
}

// I18nFile is the parsed form of i18n/{lang}/{service}.json.
// The JSON mixes a top-level "product_name" string with per-API translation objects,
// so custom unmarshaling separates them.
type I18nFile struct {
	ProductName string
	Apis        map[string]map[string]string // ApiName -> {flatField -> text}
}

func (f *I18nFile) UnmarshalJSON(data []byte) error {
	raw := make(map[string]json.RawMessage)
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	f.Apis = make(map[string]map[string]string)
	for k, v := range raw {
		if k == "product_name" {
			json.Unmarshal(v, &f.ProductName) //nolint:errcheck
			continue
		}
		var bundle map[string]string
		if err := json.Unmarshal(v, &bundle); err == nil {
			f.Apis[k] = bundle
		}
	}
	return nil
}

// CommonI18n is the structure of i18n/{lang}/common.json.
type CommonI18n map[string]string
