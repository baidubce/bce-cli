package openapi

import (
	"sort"
	"strconv"

	"github.com/baidubce/bce-cli/internal/config"
	"github.com/baidubce/bce-cli/internal/meta"
)

// autoPaginate iterates all pages of a paginated API and returns a merged result.
//
// pg is the effective paginator spec — either from the API schema or inferred by
// apiPaginator(). When pg.ResultKey is empty (inferred spec), the result key is
// resolved dynamically from the first response by selecting the first array-valued
// field (sorted alphabetically for determinism).
//
// Behaviour:
//   - Iterates until the output token is empty or the truncation key is false.
//   - The initial page cursor (marker) and page size (maxKeys) are taken directly
//     from apiParams — the user passes them as ordinary API parameters.
//   - ov.TotalCount caps the total items returned. When the limit is hit, the
//     result includes a "nextToken" field pointing to the resume position.
//     Each request asks for exactly min(nativePageSize, remaining) items so the
//     API's own output token is always the correct resume cursor.
func autoPaginate(
	profile *config.Profile,
	api *meta.Api,
	pg *meta.PaginatorSpec,
	apiParams map[string]string,
	ov *config.FlagOverrides,
	product *meta.Product,
) (map[string]interface{}, error) {
	// The initial marker may have been passed as a native API param (e.g. --marker xxx).
	currentMarker := apiParams[pg.InputToken]

	totalCount := ov.TotalCount

	// Read the native page-size param so we can tighten it when total-count requires fewer items.
	nativePageSize := 0
	if pg.LimitKey != "" {
		if v := apiParams[pg.LimitKey]; v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				nativePageSize = n
			}
		}
	}

	// resolvedResultKey is pg.ResultKey when explicitly set; otherwise it is
	// inferred from the first response (first array field, alphabetically).
	resolvedResultKey := pg.ResultKey

	var allItems []interface{}
	var resumeToken string
	var lastResult map[string]interface{}

	for {
		// Effective page size: honour native page-size, but never over-fetch when
		// total-count is set — ask for exactly as many as we still need.
		effectivePageSize := nativePageSize
		if totalCount > 0 {
			remaining := totalCount - len(allItems)
			if remaining <= 0 {
				break
			}
			if effectivePageSize == 0 || remaining < effectivePageSize {
				effectivePageSize = remaining
			}
		}

		// Build per-page params as a shallow copy of apiParams.
		pageParams := make(map[string]string, len(apiParams))
		for k, v := range apiParams {
			pageParams[k] = v
		}
		if currentMarker != "" {
			pageParams[pg.InputToken] = currentMarker
		}
		if effectivePageSize > 0 && pg.LimitKey != "" {
			pageParams[pg.LimitKey] = strconv.Itoa(effectivePageSize)
		}

		inv, err := NewInvoker(profile, api, pageParams, ov, product)
		if err != nil {
			return nil, err
		}
		result, err := inv.Call()
		if err != nil {
			return nil, err
		}
		if result == nil {
			// dry-run: caller prints nothing
			return nil, nil
		}
		lastResult = result

		// Infer ResultKey from the first response when not explicitly specified.
		// Pick the first array-valued field (sorted alphabetically for determinism).
		if resolvedResultKey == "" {
			var arrayKeys []string
			for k, v := range result {
				if _, ok := v.([]interface{}); ok {
					arrayKeys = append(arrayKeys, k)
				}
			}
			sort.Strings(arrayKeys)
			if len(arrayKeys) > 0 {
				resolvedResultKey = arrayKeys[0]
			}
		}

		// Collect items from this page.
		if resolvedResultKey != "" {
			if raw, ok := result[resolvedResultKey]; ok {
				if items, ok := raw.([]interface{}); ok {
					allItems = append(allItems, items...)
				}
			}
		}

		nextMarker, _ := result[pg.OutputToken].(string)

		// Determine end-of-pages: no next marker always means done.
		// When TruncationKey is present, also stop if the API says there are no more pages.
		exhausted := nextMarker == ""
		if pg.TruncationKey != "" {
			truncated, _ := result[pg.TruncationKey].(bool)
			if !truncated {
				exhausted = true
			}
		}
		if exhausted {
			break
		}
		if totalCount > 0 && len(allItems) >= totalCount {
			// Because we requested exactly `remaining` items, the API's nextMarker
			// is the correct cursor for the next call.
			resumeToken = nextMarker
			break
		}

		currentMarker = nextMarker
	}

	// Build merged result: copy the last page response, strip pagination-specific
	// fields, replace the result array with the fully-collected items, and attach
	// nextToken when applicable.
	merged := make(map[string]interface{}, len(lastResult))
	for k, v := range lastResult {
		merged[k] = v
	}
	delete(merged, pg.InputToken)
	if pg.LimitKey != "" {
		delete(merged, pg.LimitKey)
	}
	if pg.TruncationKey != "" {
		delete(merged, pg.TruncationKey)
	} else if resumeToken == "" {
		// All pages consumed; remove the BCE-standard truncation indicator
		delete(merged, "isTruncated")
	}
	// When all pages are consumed, remove the output token (clean result).
	// When truncated by --total-count, keep it so users can pass its value
	// as --marker to resume from where they left off.
	if resumeToken != "" {
		merged[pg.OutputToken] = resumeToken
	} else {
		delete(merged, pg.OutputToken)
	}
	if resolvedResultKey != "" {
		merged[resolvedResultKey] = allItems
	}
	return merged, nil
}
