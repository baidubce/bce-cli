package openapi

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/baidubce/bce-cli/internal/config"
	"github.com/baidubce/bce-cli/internal/i18n"
	"github.com/baidubce/bce-cli/internal/meta"
	"github.com/spf13/cobra"
)

// RegisterCommands dynamically adds one cobra.Command per service (from products.json),
// each with sub-commands for every API action found across all module JSON files.
func RegisterCommands(root *cobra.Command) {
	pl, err := meta.Products()
	if err != nil {
		// Non-fatal: service commands simply won't be available
		fmt.Fprintf(root.ErrOrStderr(), "warning: failed to load API metadata: %v\n", err)
		return
	}
	root.AddGroup(&cobra.Group{ID: "products", Title: "Cloud Services:"})
	for i := range pl.Products {
		root.AddCommand(buildServiceCmd(&pl.Products[i]))
	}
}

func buildServiceCmd(p *meta.Product) *cobra.Command {
	svc := strings.ToLower(p.Code)

	cmd := &cobra.Command{
		GroupID:            "products",
		Use:                svc,
		DisableSuggestions: true,
		Annotations:        map[string]string{"service": svc},
		// RunE is needed so that cobra routes back to this command when the
		// provided subcommand name doesn't match any registered action.
		// Without it, cobra silently prints help and returns nil — preventing
		// suggestionsForError in Execute() from ever running.
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return fmt.Errorf("unknown command %q for %q", args[0], cmd.CommandPath())
		},
	}

	// Phase-1 registration: read only version.json to get API names and summaries.
	// Individual schema files are not touched here.
	vf, err := meta.LoadVersionFile(i18n.GetLanguage(), svc)
	if err != nil {
		return cmd
	}
	cmd.Short = vf.Description
	for apiName, apiMeta := range vf.Apis {
		cmd.AddCommand(buildActionCmdStub(p, apiName, apiMeta.Summary))
	}
	return cmd
}

// buildActionCmdStub registers a cobra command with only the API name and
// summary. The schema file (schema/{service}/{ApiName}.json) is loaded lazily
// when the command actually runs or when shell completion is triggered.
func buildActionCmdStub(p *meta.Product, apiName, summary string) *cobra.Command {
	return &cobra.Command{
		Use:                apiName,
		Short:              summary,
		Annotations:        map[string]string{"action": "true"},
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			api, err := meta.LoadApi(strings.ToLower(p.Code), apiName)
			if err != nil {
				return err
			}
			return runAction(cmd, p, api, args)
		},
		// ValidArgsFunction provides --flag tab-completion by lazily loading the
		// API schema. With DisableFlagParsing=true cobra cannot complete flags on
		// its own, so this function handles the full completion path.
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if !strings.HasPrefix(toComplete, "-") {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			api, err := meta.LoadApi(strings.ToLower(p.Code), apiName)
			if err != nil {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			lang := i18n.GetLanguage()
			r := getApiResolver(lang, strings.ToLower(p.Code), apiName)
			completions := make([]string, 0, len(api.Parameters))
			for _, param := range api.Parameters {
				flag := "--" + cliParamName(param.Name)
				if strings.HasPrefix(flag, toComplete) {
					completions = append(completions, flag+"\t"+r.Resolve(param.DescKey))
				}
			}
			return completions, cobra.ShellCompDirectiveNoFileComp
		},
	}
}

func runAction(cmd *cobra.Command, p *meta.Product, api *meta.Api, args []string) error {
	// First pass: set language. Must happen before any output so that --help
	// and --generate-cli-skeleton both print in the correct language regardless
	// of where --language appears in argv.
	for i, a := range args {
		switch {
		case a == "--language" && i+1 < len(args):
			if err := i18n.SetLanguage(args[i+1]); err != nil {
				return err
			}
		case strings.HasPrefix(a, "--language="):
			if err := i18n.SetLanguage(strings.TrimPrefix(a, "--language=")); err != nil {
				return err
			}
		}
	}

	// Second pass: handle early-exit flags.
	for _, a := range args {
		switch {
		case a == "-h" || a == "--help":
			printActionHelp(cmd, p, api)
			return nil
		case a == "--generate-cli-skeleton":
			fmt.Fprintln(cmd.OutOrStdout(), buildSkeletonJSON(api.Parameters))
			return nil
		}
	}

	apiParams, globalFlags, err := parseArgs(args, api.Parameters)
	if err != nil {
		return err
	}

	// --cli-input-json: load parameters from file, CLI flags take precedence
	if fileArg := globalFlags["cli-input-json"]; fileArg != "" {
		fileParams, err := loadParamsFromFile(fileArg)
		if err != nil {
			return fmt.Errorf("--cli-input-json: %w", err)
		}
		fileParams, err = resolveFileParams(fileParams, api.Parameters)
		if err != nil {
			return fmt.Errorf("--cli-input-json: %w", err)
		}
		for k, v := range fileParams {
			if _, exists := apiParams[k]; !exists {
				apiParams[k] = v
			}
		}
	}

	if err := checkShadowedParams(api.Parameters, apiParams, globalFlags); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	profileName, _ := cmd.Root().PersistentFlags().GetString("profile")
	if v := globalFlags["profile"]; v != "" {
		profileName = v
	}

	profile := cfg.GetProfile(profileName)
	if profile == nil {
		if profileName != "" {
			return fmt.Errorf("profile %q not found; run `bce configure list` to see available profiles", profileName)
		}
		return fmt.Errorf("no profile configured; run `bce configure set` to set up your credentials")
	}
	profile.Resolve()

	ov := buildOverrides(cmd, globalFlags)

	// Language priority: --language flag > profile.Language > $BCE_LANGUAGE (init default)
	lang := ov.Language
	if lang == "" {
		lang = profile.Language
	}
	if err := i18n.SetLanguage(lang); err != nil {
		return err
	}

	product, err := meta.GetProduct(p.Code)
	if err != nil {
		return err
	}

	// Validate pagination flags: only valid for APIs with a paginator spec.
	pg := apiPaginator(api)
	if pg == nil && (ov.Pager || ov.TotalCount > 0) {
		return fmt.Errorf("this API does not support pagination (--pager / --total-count)")
	}
	if ov.TotalCount > 0 && !ov.Pager {
		return fmt.Errorf("--total-count requires --pager")
	}

	var result map[string]interface{}
	if pg != nil && ov.Pager {
		result, err = autoPaginate(profile, api, pg, apiParams, ov, product)
	} else {
		inv, err2 := NewInvoker(profile, api, apiParams, ov, product)
		if err2 != nil {
			return err2
		}
		result, err = inv.Call()
	}
	if err != nil {
		return err
	}
	if result == nil {
		return nil // dry-run or empty response
	}

	outFmt, outCols, outRows, err := parseOutput(ov.Output)
	if err != nil {
		return err
	}

	return Print(result, OutputOptions{
		Format:  outFmt,
		Query:   ov.Query,
		Rows:    outRows,
		Cols:    outCols,
		NoColor: ov.NoColor,
	})
}

// bizPrefix is prepended to API parameter names that collide with a global flag,
// e.g. an API's own "region" parameter is exposed as --biz-region. Requests are
// still sent with the original parameter name.
const bizPrefix = "biz-"

// globalFlags lists every CLI-level flag accepted by action commands, in help order.
var globalFlags = []struct {
	name, typ  string
	pagination bool // listed under "Pagination Flags", only for paginated APIs
}{
	{"profile", "string", false},
	{"region", "string", false},
	{"endpoint", "string", false},
	{"scheme", "string", false},
	{"language", "string", false},
	{"output", "string", false},
	{"query", "string", false},
	{"timeout", "int", false},
	{"cli-input-json", "string", false},
	{"unfold", "", false},
	{"generate-cli-skeleton", "", false},
	{"dry-run", "", false},
	{"debug", "", false},
	{"no-color", "", false},
	{"pager", "", true},
	{"total-count", "int", true},
}

var globalFlagSet = func() map[string]bool {
	m := make(map[string]bool, len(globalFlags))
	for _, f := range globalFlags {
		m[f.name] = true
	}
	return m
}()

// GlobalFlagNames returns the names of all CLI-level flags accepted by action commands.
func GlobalFlagNames() []string {
	names := make([]string, 0, len(globalFlags))
	for _, f := range globalFlags {
		names = append(names, f.name)
	}
	return names
}

// parseArgs splits raw args into API parameters and recognised global flags.
//
// Supported input forms for each --key:
//   - --key value          single string value
//   - --key=value          single string value (equals form)
//   - --key                boolean flag (value = "true")
//   - --key '{"a":"b"}'    JSON object  (List/Object params, JSON mode)
//   - --key '[{...}]'      JSON array   (List params, JSON mode)
//   - --key a=1 b=2        KV pairs → {"a":"1","b":"2"}  (List/Object params, KV mode)
//
// For List-type params in KV mode, --key may appear multiple times; each occurrence
// contributes one element to the final JSON array.
func parseArgs(args []string, schemaParams []meta.Parameter) (apiParams map[string]string, globals map[string]string, err error) {
	apiParams = make(map[string]string)
	globals = make(map[string]string)
	listAccum := make(map[string][]string) // list-param name → accumulated JSON objects

	// Pre-scan for --unfold so KV mode can be enabled even if the flag appears after params.
	unfold := false
	for _, a := range args {
		if a == "--unfold" {
			unfold = true
			break
		}
	}

	paramTypeMap := paramTypes(schemaParams)

	i := 0
	for i < len(args) {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			return nil, nil, fmt.Errorf("unexpected argument %q: all arguments must be --flag value pairs", arg)
		}
		key := strings.TrimPrefix(arg, "--")

		// --key=value form
		var inlineVal *string
		if idx := strings.IndexByte(key, '='); idx >= 0 {
			val := key[idx+1:]
			key = key[:idx]
			inlineVal = &val
		}

		name, isGlobal, err := resolveFlag(key, paramTypeMap)
		if err != nil {
			return nil, nil, err
		}
		target := apiParams
		if isGlobal {
			target = globals
		}

		if inlineVal != nil {
			target[name] = *inlineVal
			i++
			continue
		}

		i++ // consume --key token

		// no value follows → boolean flag
		if i >= len(args) || strings.HasPrefix(args[i], "--") {
			target[name] = "true"
			continue
		}

		firstToken := args[i]
		paramType := ""
		if !isGlobal {
			paramType = paramTypeMap[name]
		}
		isStructured := paramType == "list" || paramType == "object"

		// --output may be followed by sub-params (cols=, rows=) as separate tokens.
		// Consume them all into a single space-joined value.
		if isGlobal && name == "output" {
			var parts []string
			parts = append(parts, firstToken)
			i++
			for i < len(args) && !strings.HasPrefix(args[i], "--") {
				parts = append(parts, args[i])
				i++
			}
			target[name] = strings.Join(parts, " ")
			continue
		}

		// JSON mode: value is a literal JSON string (starts with { or [)
		if strings.HasPrefix(firstToken, "{") || strings.HasPrefix(firstToken, "[") {
			target[name] = firstToken
			i++
			continue
		}

		// KV mode: only for List/Object params whose first token looks like k=v.
		// Requires --unfold; otherwise structured params must use JSON format.
		if isStructured && strings.Contains(firstToken, "=") {
			if !unfold {
				return nil, nil, fmt.Errorf(
					"parameter %q is a structured type (List/Object); pass a JSON value like --%s '{...}' or add --unfold to enable KV dot-notation",
					key, key)
			}
			var kvTokens []string
			for i < len(args) && !strings.HasPrefix(args[i], "--") {
				kvTokens = append(kvTokens, args[i])
				i++
			}
			obj := kvTokensToJSON(kvTokens)
			if paramType == "list" {
				listAccum[name] = append(listAccum[name], obj)
			} else {
				// Object: only one occurrence makes sense
				apiParams[name] = obj
			}
			continue
		}

		// Plain single-value mode
		target[name] = firstToken
		i++
	}

	// Merge accumulated list items into a JSON array
	for key, items := range listAccum {
		apiParams[key] = "[" + strings.Join(items, ",") + "]"
	}

	return apiParams, globals, nil
}

// paramTypes maps each top-level API parameter name to its lower-cased type.
func paramTypes(schemaParams []meta.Parameter) map[string]string {
	m := make(map[string]string, len(schemaParams))
	for _, p := range schemaParams {
		m[p.Name] = strings.ToLower(p.Type)
	}
	return m
}

// resolveFlag maps a user-facing flag name to either a global flag or an API
// parameter name. --biz-<name> is accepted only when the API really has a
// parameter <name> that collides with a global flag.
func resolveFlag(key string, paramTypeMap map[string]string) (name string, isGlobal bool, err error) {
	if orig, ok := strings.CutPrefix(key, bizPrefix); ok && globalFlagSet[orig] {
		if _, exists := paramTypeMap[orig]; !exists {
			return "", false, fmt.Errorf("unknown parameter --%s: this API has no %q parameter", key, orig)
		}
		return orig, false, nil
	}
	if globalFlagSet[key] {
		return key, true, nil
	}
	return key, false, nil
}

// cliParamName returns the flag name users type for a top-level API parameter.
func cliParamName(name string) string {
	if globalFlagSet[name] {
		return bizPrefix + name
	}
	return name
}

// checkShadowedParams catches a required API parameter that collides with a
// global flag (e.g. "region") being passed as the global flag instead of --biz-<name>.
func checkShadowedParams(schemaParams []meta.Parameter, apiParams, globals map[string]string) error {
	for _, p := range schemaParams {
		if !p.Required || p.Default != nil || !globalFlagSet[p.Name] {
			continue
		}
		if _, ok := apiParams[p.Name]; ok {
			continue
		}
		if _, ok := globals[p.Name]; ok {
			return fmt.Errorf("missing required parameter --%s: --%s sets the global %s, pass this API's %q parameter as --%s",
				cliParamName(p.Name), p.Name, p.Name, p.Name, cliParamName(p.Name))
		}
	}
	return nil
}

// kvTokensToJSON converts ["k1=v1", "a.b=v2"] → {"k1":"v1","a":{"b":"v2"}}.
// Dot-separated keys are expanded into nested objects, enabling multi-level KV input.
func kvTokensToJSON(tokens []string) string {
	root := make(map[string]interface{})
	for _, t := range tokens {
		idx := strings.IndexByte(t, '=')
		if idx < 0 {
			continue
		}
		key := t[:idx]
		val := t[idx+1:]
		setNestedKey(root, strings.Split(key, "."), val)
	}
	b, _ := json.Marshal(root)
	return string(b)
}

// setNestedKey walks/creates intermediate maps for each key segment and sets the leaf value.
func setNestedKey(m map[string]interface{}, keys []string, val string) {
	if len(keys) == 1 {
		m[keys[0]] = val
		return
	}
	sub, ok := m[keys[0]]
	if !ok {
		sub = make(map[string]interface{})
		m[keys[0]] = sub
	}
	subMap, ok := sub.(map[string]interface{})
	if !ok {
		subMap = make(map[string]interface{})
		m[keys[0]] = subMap
	}
	setNestedKey(subMap, keys[1:], val)
}

func buildOverrides(cmd *cobra.Command, globals map[string]string) *config.FlagOverrides {
	rootFlags := cmd.Root().PersistentFlags()
	getString := func(flagName string) string {
		if v := globals[flagName]; v != "" {
			return v
		}
		v, _ := rootFlags.GetString(flagName)
		return v
	}
	getBool := func(flagName string) bool {
		if globals[flagName] == "true" {
			return true
		}
		v, _ := rootFlags.GetBool(flagName)
		return v
	}
	getInt := func(flagName string) int {
		if v := globals[flagName]; v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				return n
			}
		}
		v, _ := rootFlags.GetInt(flagName)
		return v
	}

	output := getString("output")
	if output == "" {
		output = "json"
	}

	return &config.FlagOverrides{
		Region:     getString("region"),
		Endpoint:   getString("endpoint"),
		Scheme:     getString("scheme"),
		Language:   getString("language"),
		Output:     output,
		Query:      getString("query"),
		DryRun:     getBool("dry-run"),
		Debug:      getBool("debug"),
		NoColor:    getBool("no-color"),
		Timeout:    getInt("timeout"),
		Pager:      getBool("pager"),
		TotalCount: getInt("total-count"),
	}
}

func printActionHelp(cmd *cobra.Command, p *meta.Product, api *meta.Api) {
	w := cmd.OutOrStdout()
	r := getApiResolver(i18n.GetLanguage(), strings.ToLower(p.Code), api.Name)
	fmt.Fprintf(w, "Usage:\n  bce %s %s [--parameter value ...]\n", strings.ToLower(p.Code), api.Name)
	if sum := r.ApiSummary(api.Name); sum != "" {
		fmt.Fprintf(w, "\n%s\n", sum)
	}
	fmt.Fprintf(w, "\nFlags:\n")
	for _, param := range api.Parameters {
		lang := i18n.GetLanguage()
		req := i18n.T(lang, "optional")
		if param.Required {
			req = i18n.T(lang, "required")
		}
		desc := r.Resolve(param.DescKey)
		fmt.Fprintf(w, "      --%-22s %-10s %-10s %s\n", cliParamName(param.Name), param.Type, req, desc)
		if len(param.SubParameters) > 0 {
			printSubParams(w, r, param.SubParameters, 0)
			fmt.Fprintf(w, "        %s\n", i18n.T(lang, "json-format"))
			for _, line := range strings.Split(buildJSONExample(param, nil), "\n") {
				fmt.Fprintf(w, "          %s\n", line)
			}
			fmt.Fprintf(w, "        %s  %s\n", i18n.T(lang, "kv-format"), i18n.T(lang, "kv-unfold-hint"))
			for _, line := range strings.Split(buildKVExample(param), "\n") {
				fmt.Fprintf(w, "          %s\n", line)
			}
			fmt.Fprintln(w)
		}
	}
	fmt.Fprintf(w, "\nGlobal Flags:\n")
	lang := i18n.GetLanguage()
	printFlag := func(name, typ string) {
		desc := i18n.FlagDesc(lang, name)
		if typ != "" {
			fmt.Fprintf(w, "      --%-14s %-8s %s\n", name, typ, desc)
		} else {
			fmt.Fprintf(w, "      --%-23s %s\n", name, desc)
		}
	}
	for _, f := range globalFlags {
		if !f.pagination {
			printFlag(f.name, f.typ)
		}
	}
	if apiPaginator(api) != nil {
		fmt.Fprintf(w, "\nPagination Flags:\n")
		for _, f := range globalFlags {
			if f.pagination {
				printFlag(f.name, f.typ)
			}
		}
	}
}

// buildJSONExample generates an indented JSON skeleton with inline // comments from the resolver.
func buildJSONExample(param meta.Parameter, r *meta.Resolver) string {
	if strings.ToLower(param.Type) == "list" {
		obj := buildJSONObject(param.SubParameters, 1, r)
		return "[\n  " + obj + "\n]"
	}
	return buildJSONObject(param.SubParameters, 0, r)
}

// buildJSONObject recursively produces an indented JSON object at the given depth,
// appending  // desc  comments for each field.
func buildJSONObject(params []meta.Parameter, depth int, r *meta.Resolver) string {
	pad := strings.Repeat("  ", depth)
	fieldPad := pad + "  "

	var sb strings.Builder
	sb.WriteString("{\n")

	for i, sub := range params {
		isLast := i == len(params)-1
		comma := ","
		if isLast {
			comma = ""
		}

		comment := ""
		if r != nil {
			lang := i18n.GetLanguage()
			req := i18n.T(lang, "optional")
			if sub.Required {
				req = i18n.T(lang, "required")
			}
			desc := r.Resolve(sub.DescKey)
			comment = fmt.Sprintf("  // %-10s %-10s %s", sub.Type, req, desc)
			comment = strings.TrimRight(comment, " ")
		}

		if len(sub.SubParameters) > 0 {
			nested := buildJSONObject(sub.SubParameters, depth+1, r)
			if strings.ToLower(sub.Type) == "list" {
				// "key": [  // comment
				//   { ... }
				// ]
				fmt.Fprintf(&sb, "%s\"%s\": [%s\n%s  %s\n%s  ]%s\n",
					fieldPad, sub.Name, comment, fieldPad, nested, fieldPad, comma)
			} else {
				// "key": {  // comment   ← comment on the opening-brace line
				nestedWithComment := "{" + comment + nested[1:] // nested[1:] skips the leading "{"
				fmt.Fprintf(&sb, "%s\"%s\": %s%s\n", fieldPad, sub.Name, nestedWithComment, comma)
			}
		} else {
			fmt.Fprintf(&sb, "%s\"%s\": \"...\"%s%s\n", fieldPad, sub.Name, comma, comment)
		}
	}

	sb.WriteString(pad + "}")
	return sb.String()
}

// buildKVExample generates a KV-style usage example, using dot-notation for nested fields.
func buildKVExample(param meta.Parameter) string {
	fields := flattenSubParamPaths(param.SubParameters, "")
	kv := "--" + cliParamName(param.Name) + " " + strings.Join(fields, " ")
	if strings.ToLower(param.Type) == "list" {
		return kv + "\n" + kv + "  " + fmt.Sprintf(i18n.T(i18n.GetLanguage(), "kv-repeat-hint"), cliParamName(param.Name))
	}
	return kv
}

// flattenSubParamPaths recursively flattens sub_parameters into dot-notation path=... tokens.
func flattenSubParamPaths(params []meta.Parameter, prefix string) []string {
	var paths []string
	for _, sub := range params {
		name := prefix + sub.Name
		if len(sub.SubParameters) > 0 {
			paths = append(paths, flattenSubParamPaths(sub.SubParameters, name+".")...)
		} else {
			paths = append(paths, name+"=...")
		}
	}
	return paths
}

// printSubParams recursively prints sub_parameters at increasing indentation levels.
func printSubParams(w io.Writer, r *meta.Resolver, params []meta.Parameter, depth int) {
	indent := strings.Repeat("  ", depth)
	lang := i18n.GetLanguage()
	for _, sub := range params {
		subReq := i18n.T(lang, "optional")
		if sub.Required {
			subReq = i18n.T(lang, "required")
		}
		subDesc := r.Resolve(sub.DescKey)
		fmt.Fprintf(w, "        %s%-22s %-10s %-10s %s\n", indent, sub.Name, sub.Type, subReq, subDesc)
		if len(sub.SubParameters) > 0 {
			printSubParams(w, r, sub.SubParameters, depth+1)
		}
	}
}

// loadParamsFromFile reads a JSON object from a file specified as "file://path"
// and returns its fields as string values suitable for merging into apiParams.
func loadParamsFromFile(arg string) (map[string]string, error) {
	path := strings.TrimPrefix(arg, "file://")
	if path == arg {
		return nil, fmt.Errorf("value must start with file://, got: %s", arg)
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home dir: %w", err)
		}
		path = home + path[1:]
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file %q: %w", path, err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse JSON from %q: %w", path, err)
	}
	result := make(map[string]string, len(raw))
	for k, v := range raw {
		// Preserve objects/arrays as JSON strings; unwrap quoted strings.
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			result[k] = s
		} else {
			result[k] = string(v)
		}
	}
	return result, nil
}

// resolveFileParams maps top-level --cli-input-json keys, which use the same
// names as CLI flags (e.g. "biz-region"), back to the original API parameter names.
func resolveFileParams(fileParams map[string]string, schemaParams []meta.Parameter) (map[string]string, error) {
	paramTypeMap := paramTypes(schemaParams)
	out := make(map[string]string, len(fileParams))
	for k, v := range fileParams {
		name, isGlobal, err := resolveFlag(k, paramTypeMap)
		if err != nil {
			return nil, err
		}
		if isGlobal {
			if _, exists := paramTypeMap[name]; exists {
				return nil, fmt.Errorf("key %q collides with a global flag; use %q for this API's parameter", k, cliParamName(name))
			}
		}
		out[name] = v
	}
	return out, nil
}

// buildSkeletonJSON generates an indented JSON skeleton for all API parameters,
// using type-appropriate placeholder values.
func buildSkeletonJSON(params []meta.Parameter) string {
	return buildSkeletonObject(params, 0)
}

func buildSkeletonObject(params []meta.Parameter, depth int) string {
	pad := strings.Repeat("  ", depth)
	fieldPad := pad + "  "

	var sb strings.Builder
	sb.WriteString("{\n")
	for idx, p := range params {
		isLast := idx == len(params)-1
		comma := ","
		if isLast {
			comma = ""
		}
		// Top-level keys mirror the CLI flag names; nested keys are sent as-is.
		key := p.Name
		if depth == 0 {
			key = cliParamName(p.Name)
		}
		if len(p.SubParameters) > 0 {
			nested := buildSkeletonObject(p.SubParameters, depth+1)
			if strings.ToLower(p.Type) == "list" {
				fmt.Fprintf(&sb, "%s\"%s\": [\n%s  %s\n%s  ]%s\n",
					fieldPad, key, fieldPad, nested, fieldPad, comma)
			} else {
				fmt.Fprintf(&sb, "%s\"%s\": %s%s\n", fieldPad, key, nested, comma)
			}
		} else {
			placeholder := skeletonPlaceholder(p)
			fmt.Fprintf(&sb, "%s\"%s\": %s%s\n", fieldPad, key, placeholder, comma)
		}
	}
	sb.WriteString(pad + "}")
	return sb.String()
}

func skeletonPlaceholder(p meta.Parameter) string {
	if p.Default != nil {
		b, err := json.Marshal(p.Default)
		if err == nil {
			return string(b)
		}
	}
	switch strings.ToLower(p.Type) {
	case "integer", "int", "long":
		return "0"
	case "boolean", "bool":
		return "false"
	case "list":
		return "[]"
	default:
		return `""`
	}
}

// apiPaginator returns the effective PaginatorSpec for an API.
// It uses the explicit spec from the schema when defined; otherwise it infers
// BCE-convention pagination from the presence of a "marker" request parameter:
//   - InputToken:  "marker"
//   - OutputToken: "nextMarker"
//   - LimitKey:    "maxKeys"  (when the parameter exists)
//   - ResultKey:   ""         (resolved dynamically from the first response)
//
// Returns nil when the API has no pagination support.
func apiPaginator(api *meta.Api) *meta.PaginatorSpec {
	if api.Paginator != nil {
		return api.Paginator
	}
	hasMarker := false
	hasMaxKeys := false
	for _, p := range api.Parameters {
		switch p.Name {
		case "marker":
			hasMarker = true
		case "maxKeys":
			hasMaxKeys = true
		}
	}
	if !hasMarker {
		return nil
	}
	spec := &meta.PaginatorSpec{
		InputToken:  "marker",
		OutputToken: "nextMarker",
	}
	if hasMaxKeys {
		spec.LimitKey = "maxKeys"
	}
	return spec
}
var (
	resolverMu    sync.RWMutex
	resolverCache = make(map[string]*meta.Resolver)
)

func getApiResolver(lang, service, apiName string) *meta.Resolver {
	key := lang + ":" + service + ":" + apiName

	resolverMu.RLock()
	r, ok := resolverCache[key]
	resolverMu.RUnlock()
	if ok {
		return r
	}

	resolverMu.Lock()
	defer resolverMu.Unlock()
	if r, ok = resolverCache[key]; ok {
		return r
	}
	r = meta.NewApiResolver(lang, service, apiName)
	resolverCache[key] = r
	return r
}

// RefreshServiceDescs updates the Short description of every dynamically-registered
// API command to match the current language. Called from SetHelpFunc when
// --language overrides the active language at help-render time.
func RefreshServiceDescs(root *cobra.Command) {
	lang := i18n.GetLanguage()
	for _, cmd := range root.Commands() {
		svc, ok := cmd.Annotations["service"]
		if !ok {
			continue
		}
		vf, err := meta.LoadVersionFile(lang, svc)
		if err != nil {
			continue
		}
		if vf.Description != "" {
			cmd.Short = vf.Description
		}
		for _, sub := range cmd.Commands() {
			if m, ok := vf.Apis[sub.Use]; ok && m.Summary != "" {
				sub.Short = m.Summary
			}
		}
	}
}
