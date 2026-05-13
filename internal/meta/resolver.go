package meta

import "strings"

// Resolver resolves desc_key values to translated text for a specific API,
// backed by that API's own i18n file and the shared common terminology.
type Resolver struct {
	i18n   I18nFile
	common CommonI18n
}

// NewApiResolver builds a Resolver for a single API by loading only
// i18n/{lang}/{service}/{ApiName}.json and i18n/{lang}/common.json.
// This is the only resolver constructor; service-wide loading is not needed
// because each API's descriptions are self-contained in its own file.
func NewApiResolver(lang, service, apiName string) *Resolver {
	i18n, _ := LoadApiI18n(lang, service, apiName)
	common, _ := LoadCommonI18n(lang)
	if common == nil {
		common = make(CommonI18n)
	}
	return &Resolver{i18n: i18n, common: common}
}

// Resolve translates a desc_key to human-readable text.
//
// Supported desc_key formats:
//   - "{service}.{ApiName}.{field}"  — look up in the API's i18n bundle,
//     fall back to common[lastSegment]
//   - "common.{field}"               — look up directly in common
func (r *Resolver) Resolve(descKey string) string {
	parts := strings.SplitN(descKey, ".", 3)
	switch len(parts) {
	case 3:
		apiName, field := parts[1], parts[2]
		if bundle, ok := r.i18n.Apis[apiName]; ok {
			if text, ok := bundle[field]; ok {
				return text
			}
		}
		// fallback: last dot-segment in common
		paramName := field
		if idx := strings.LastIndex(field, "."); idx >= 0 {
			paramName = field[idx+1:]
		}
		if text, ok := r.common[paramName]; ok {
			return text
		}
	case 2:
		if text, ok := r.common[parts[1]]; ok {
			return text
		}
	}
	return ""
}

// ApiSummary returns the "desc" translation for apiName, or "" if not found.
func (r *Resolver) ApiSummary(apiName string) string {
	if bundle, ok := r.i18n.Apis[apiName]; ok {
		return bundle["desc"]
	}
	return ""
}
