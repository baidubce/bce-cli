package openapi

import (
	"strings"
	"testing"

	"github.com/baidubce/bce-cli/internal/meta"
)

var regionClashParams = []meta.Parameter{
	{Name: "zoneId", Type: "String", Required: true},
	{Name: "region", Type: "String", Required: true},
	{Name: "query", Type: "Object"},
}

func TestParseArgsBizPrefix(t *testing.T) {
	args := []string{
		"--zoneId", "z1",
		"--biz-region", "gz",
		"--region", "bj",
		"--biz-query={\"a\":\"b\"}",
		"--query", "result",
	}
	apiParams, globals, err := parseArgs(args, regionClashParams)
	if err != nil {
		t.Fatal(err)
	}
	if apiParams["region"] != "gz" || apiParams["zoneId"] != "z1" || apiParams["query"] != `{"a":"b"}` {
		t.Errorf("apiParams = %v", apiParams)
	}
	if _, ok := apiParams["biz-region"]; ok {
		t.Errorf("biz- prefix leaked into apiParams: %v", apiParams)
	}
	if globals["region"] != "bj" || globals["query"] != "result" {
		t.Errorf("globals = %v", globals)
	}
}

func TestParseArgsBizPrefixUnknown(t *testing.T) {
	params := []meta.Parameter{{Name: "vpcId", Type: "String"}}
	if _, _, err := parseArgs([]string{"--biz-region", "gz"}, params); err == nil {
		t.Fatal("expected error for --biz-region on API without region parameter")
	}
}

func TestParseArgsBizPrefixUnfold(t *testing.T) {
	apiParams, _, err := parseArgs([]string{"--unfold", "--biz-query", "a=1", "b.c=2"}, regionClashParams)
	if err != nil {
		t.Fatal(err)
	}
	if apiParams["query"] != `{"a":"1","b":{"c":"2"}}` {
		t.Errorf("query = %q", apiParams["query"])
	}
}

func TestCheckShadowedParams(t *testing.T) {
	apiParams, globals, _ := parseArgs([]string{"--zoneId", "z1", "--region", "gz"}, regionClashParams)
	err := checkShadowedParams(regionClashParams, apiParams, globals)
	if err == nil || !strings.Contains(err.Error(), "--biz-region") {
		t.Fatalf("expected hint about --biz-region, got %v", err)
	}

	apiParams, globals, _ = parseArgs([]string{"--zoneId", "z1", "--biz-region", "gz", "--region", "bj"}, regionClashParams)
	if err := checkShadowedParams(regionClashParams, apiParams, globals); err != nil {
		t.Fatal(err)
	}
}

func TestResolveFileParams(t *testing.T) {
	got, err := resolveFileParams(map[string]string{"zoneId": "z1", "biz-region": "gz"}, regionClashParams)
	if err != nil {
		t.Fatal(err)
	}
	if got["region"] != "gz" || got["zoneId"] != "z1" || len(got) != 2 {
		t.Errorf("got %v", got)
	}

	if _, err := resolveFileParams(map[string]string{"region": "gz"}, regionClashParams); err == nil {
		t.Fatal("expected error for raw colliding key in file")
	}
}

func TestSkeletonAndHelpNames(t *testing.T) {
	params := []meta.Parameter{
		{Name: "region", Type: "String"},
		{Name: "config", Type: "Object", SubParameters: []meta.Parameter{{Name: "region", Type: "String"}}},
	}
	s := buildSkeletonJSON(params)
	if !strings.Contains(s, `"biz-region": ""`) {
		t.Errorf("top-level key not prefixed:\n%s", s)
	}
	if !strings.Contains(s, `    "region": ""`) {
		t.Errorf("nested key should keep original name:\n%s", s)
	}
	if got := buildKVExample(meta.Parameter{Name: "query", Type: "Object", SubParameters: []meta.Parameter{{Name: "a"}}}); got != "--biz-query a=..." {
		t.Errorf("kv example = %q", got)
	}
}
