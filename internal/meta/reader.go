package meta

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"sync"
)

// ---- products ---------------------------------------------------------------

var (
	productsOnce   sync.Once
	cachedProducts *ProductList
	productsErr    error
)

// Products loads and caches the full product list from schema/products.json.
func Products() (*ProductList, error) {
	productsOnce.Do(func() {
		data, err := fs.ReadFile(dataFS, "schema/products.json")
		if err != nil {
			productsErr = fmt.Errorf("read products.json: %w", err)
			return
		}
		var pl ProductList
		if err := json.Unmarshal(data, &pl); err != nil {
			productsErr = fmt.Errorf("parse products.json: %w", err)
			return
		}
		cachedProducts = &pl
	})
	return cachedProducts, productsErr
}

// GetProduct returns the product definition for the given service code (case-insensitive).
func GetProduct(code string) (*Product, error) {
	pl, err := Products()
	if err != nil {
		return nil, err
	}
	lower := strings.ToLower(code)
	for i := range pl.Products {
		if strings.ToLower(pl.Products[i].Code) == lower {
			return &pl.Products[i], nil
		}
	}
	return nil, fmt.Errorf("product %q not found", code)
}

// ---- schema -----------------------------------------------------------------

var (
	apiSchemaMu    sync.RWMutex
	apiSchemaCache = make(map[string]*Api) // "service:ApiName" -> *Api
)

// LoadApi loads (and caches) a single API from schema/{service}/{ApiName}.json.
func LoadApi(service, apiName string) (*Api, error) {
	key := strings.ToLower(service) + ":" + apiName
	apiSchemaMu.RLock()
	if api, ok := apiSchemaCache[key]; ok {
		apiSchemaMu.RUnlock()
		return api, nil
	}
	apiSchemaMu.RUnlock()

	path := fmt.Sprintf("schema/%s/%s.json", strings.ToLower(service), apiName)
	data, err := fs.ReadFile(dataFS, path)
	if err != nil {
		return nil, fmt.Errorf("read api %s/%s: %w", service, apiName, err)
	}
	var api Api
	if err := json.Unmarshal(data, &api); err != nil {
		return nil, fmt.Errorf("parse api %s/%s: %w", service, apiName, err)
	}
	apiSchemaMu.Lock()
	apiSchemaCache[key] = &api
	apiSchemaMu.Unlock()
	return &api, nil
}

// LoadAllApis loads all APIs for a service by reading every {ApiName}.json
// file under schema/{service}/. Results are served from the per-API cache.
func LoadAllApis(service string) ([]Api, error) {
	dirPath := "schema/" + strings.ToLower(service)
	entries, err := fs.ReadDir(dataFS, dirPath)
	if err != nil {
		return nil, fmt.Errorf("list APIs for %q: %w", service, err)
	}
	var apis []Api
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		apiName := strings.TrimSuffix(e.Name(), ".json")
		api, err := LoadApi(service, apiName)
		if err != nil {
			return nil, err
		}
		apis = append(apis, *api)
	}
	return apis, nil
}

// FindApi finds a single API by name within a service.
// It first attempts a direct file lookup, then falls back to a
// case-insensitive scan of all API files.
func FindApi(service, apiName string) (*Api, error) {
	if api, err := LoadApi(service, apiName); err == nil {
		return api, nil
	}
	apis, err := LoadAllApis(service)
	if err != nil {
		return nil, err
	}
	lower := strings.ToLower(apiName)
	for i := range apis {
		if strings.ToLower(apis[i].Name) == lower {
			return &apis[i], nil
		}
	}
	return nil, fmt.Errorf("api %q not found in service %q", apiName, service)
}

// ---- i18n -------------------------------------------------------------------

var (
	commonMu    sync.RWMutex
	commonCache = make(map[string]CommonI18n) // lang -> CommonI18n
)

// LoadCommonI18n loads (and caches) the common terminology file for a language.
func LoadCommonI18n(lang string) (CommonI18n, error) {
	commonMu.RLock()
	if c, ok := commonCache[lang]; ok {
		commonMu.RUnlock()
		return c, nil
	}
	commonMu.RUnlock()

	path := fmt.Sprintf("i18n/%s/common.json", lang)
	data, err := fs.ReadFile(dataFS, path)
	if err != nil {
		return nil, err
	}
	var c CommonI18n
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse common i18n %s: %w", lang, err)
	}
	commonMu.Lock()
	commonCache[lang] = c
	commonMu.Unlock()
	return c, nil
}

// LoadApiI18n loads i18n translations for a single API from
// i18n/{lang}/{service}/{ApiName}.json.
func LoadApiI18n(lang, service, apiName string) (I18nFile, error) {
	path := fmt.Sprintf("i18n/%s/%s/%s.json", lang, strings.ToLower(service), apiName)
	data, err := fs.ReadFile(dataFS, path)
	if err != nil {
		return I18nFile{Apis: make(map[string]map[string]string)}, fmt.Errorf("read %s: %w", path, err)
	}
	var bundle map[string]map[string]string
	if err := json.Unmarshal(data, &bundle); err != nil {
		return I18nFile{Apis: make(map[string]map[string]string)}, fmt.Errorf("parse i18n %s: %w", path, err)
	}
	return I18nFile{Apis: bundle}, nil
}

// ---- version file -----------------------------------------------------------

var (
	versionMu    sync.RWMutex
	versionCache = make(map[string]*VersionFile) // "lang:service" -> *VersionFile
)

// LoadVersionFile loads (and caches) the version metadata for a service from
// i18n/{lang}/{service}/version.json.
func LoadVersionFile(lang, service string) (*VersionFile, error) {
	key := lang + ":" + strings.ToLower(service)
	versionMu.RLock()
	if vf, ok := versionCache[key]; ok {
		versionMu.RUnlock()
		return vf, nil
	}
	versionMu.RUnlock()

	path := fmt.Sprintf("i18n/%s/%s/version.json", lang, strings.ToLower(service))
	data, err := fs.ReadFile(dataFS, path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var v VersionFile
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	versionMu.Lock()
	versionCache[key] = &v
	versionMu.Unlock()
	return &v, nil
}
