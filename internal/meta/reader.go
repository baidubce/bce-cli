package meta

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"sync"
)

var (
	productsOnce sync.Once
	cachedProducts *ProductList
	productsErr    error
)

// Products loads and caches the full product list from schema/products.json.
func Products() (*ProductList, error) {
	productsOnce.Do(func() {
		data, err := dataFS.ReadFile("schema/products.json")
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

// ListModules returns all module names under a service (filename without .json suffix).
func ListModules(service string) ([]string, error) {
	dirPath := "schema/" + strings.ToLower(service)
	entries, err := fs.ReadDir(dataFS, dirPath)
	if err != nil {
		return nil, fmt.Errorf("list modules for %q: %w", service, err)
	}
	var modules []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			modules = append(modules, strings.TrimSuffix(e.Name(), ".json"))
		}
	}
	return modules, nil
}

// LoadModule loads a single module JSON file.
func LoadModule(service, module string) (*Module, error) {
	path := fmt.Sprintf("schema/%s/%s.json", strings.ToLower(service), strings.ToLower(module))
	data, err := dataFS.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read module %s/%s: %w", service, module, err)
	}
	var m Module
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse module %s/%s: %w", service, module, err)
	}
	return &m, nil
}

// LoadAllApis loads and merges all APIs across every module of a service.
func LoadAllApis(service string) ([]Api, error) {
	modules, err := ListModules(service)
	if err != nil {
		return nil, err
	}
	var apis []Api
	for _, mod := range modules {
		m, err := LoadModule(service, mod)
		if err != nil {
			return nil, err
		}
		apis = append(apis, m.Apis...)
	}
	return apis, nil
}

// FindApi finds a single API by name within a service (case-insensitive).
func FindApi(service, apiName string) (*Api, error) {
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

// LoadI18n loads the i18n file for a given language and service.
func LoadI18n(lang, service string) (I18nFile, error) {
	path := fmt.Sprintf("i18n/%s/%s.json", lang, strings.ToLower(service))
	data, err := dataFS.ReadFile(path)
	if err != nil {
		return I18nFile{}, err
	}
	var f I18nFile
	if err := json.Unmarshal(data, &f); err != nil {
		return I18nFile{}, fmt.Errorf("parse i18n %s/%s: %w", lang, service, err)
	}
	return f, nil
}

// LoadCommonI18n loads the common terminology file for a given language.
func LoadCommonI18n(lang string) (CommonI18n, error) {
	path := fmt.Sprintf("i18n/%s/common.json", lang)
	data, err := dataFS.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c CommonI18n
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse common i18n %s: %w", lang, err)
	}
	return c, nil
}
