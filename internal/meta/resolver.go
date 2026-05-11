package meta

import "strings"

// Resolver resolves desc_key values to translated text for a specific
// language and service, with automatic fallback to common terminology.
type Resolver struct {
	i18n   I18nFile
	common CommonI18n
}

// NewResolver builds a Resolver for the given language and service.
// Errors loading i18n files are silently ignored; empty maps are used as fallback.
func NewResolver(lang, service string) *Resolver {
	i18n, _ := LoadI18n(lang, service)
	common, _ := LoadCommonI18n(lang)
	if common == nil {
		common = make(CommonI18n)
	}
	return &Resolver{i18n: i18n, common: common}
}

// Resolve translates a desc_key to human-readable text.
//
// desc_key format: "{service}.{ApiName}.{flatField}"
//
// Resolution order:
//  1. i18n.Apis[ApiName][flatField]
//  2. common[lastSegment(flatField)]  (fallback)
func (r *Resolver) Resolve(descKey string) string {
	parts := strings.SplitN(descKey, ".", 3)
	if len(parts) == 3 {
		apiName, field := parts[1], parts[2]
		if bundle, ok := r.i18n.Apis[apiName]; ok {
			if text, ok := bundle[field]; ok {
				return text
			}
		}
		// fallback: look up the last dot-segment in common
		paramName := field
		if idx := strings.LastIndex(field, "."); idx >= 0 {
			paramName = field[idx+1:]
		}
		if text, ok := r.common[paramName]; ok {
			return text
		}
	}
	return ""
}

// ApiSummary returns the summary line for an API, or empty string if not found.
func (r *Resolver) ApiSummary(apiName string) string {
	if bundle, ok := r.i18n.Apis[apiName]; ok {
		return bundle["desc"]
	}
	return ""
}

// ProductName returns the localised product display name.
func (r *Resolver) ProductName() string {
	return r.i18n.ProductName
}
