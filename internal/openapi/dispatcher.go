package openapi

import (
	"fmt"
	"strings"
	"sync"

	"github.com/baidubce/bce-cli/internal/config"
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
	for i := range pl.Products {
		root.AddCommand(buildServiceCmd(&pl.Products[i]))
	}
}

func buildServiceCmd(p *meta.Product) *cobra.Command {
	svc := strings.ToLower(p.Code)
	r := getResolver(svc)

	cmd := &cobra.Command{
		Use:   svc,
		Short: r.ProductName(),
	}

	apis, err := meta.LoadAllApis(svc)
	if err != nil {
		return cmd
	}
	for i := range apis {
		cmd.AddCommand(buildActionCmd(p, &apis[i]))
	}
	return cmd
}

func buildActionCmd(p *meta.Product, api *meta.Api) *cobra.Command {
	r := getResolver(strings.ToLower(p.Code))

	cmd := &cobra.Command{
		Use:   api.Name,
		Short: r.ApiSummary(api.Name),
		// DisableFlagParsing lets us receive all args as a raw []string and parse
		// --key value pairs ourselves, because the parameter set is fully dynamic.
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAction(cmd, p, api, args)
		},
	}

	// Register parameters as cobra flags for shell completion only.
	// They are never actually parsed by cobra (DisableFlagParsing=true).
	for _, param := range flattenParams(api.Parameters) {
		cmd.Flags().String(param.Name, "", r.Resolve(param.DescKey))
	}
	return cmd
}

func runAction(cmd *cobra.Command, p *meta.Product, api *meta.Api, args []string) error {
	// Manual help check (DisableFlagParsing bypasses cobra's built-in -h handling)
	for _, a := range args {
		if a == "-h" || a == "--help" {
			printActionHelp(cmd, p, api)
			return nil
		}
	}

	apiParams, globalFlags, err := parseArgs(args)
	if err != nil {
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
		profile = &config.Profile{Name: "default"}
	}
	profile.Resolve()

	ov := buildOverrides(cmd, globalFlags)

	product, err := meta.GetProduct(p.Code)
	if err != nil {
		return err
	}

	inv := NewInvoker(profile, api, apiParams, ov, product)
	result, err := inv.Call()
	if err != nil {
		return err
	}
	if result == nil {
		return nil // dry-run or empty response
	}

	return Print(result, OutputOptions{
		Format:  OutputFormat(ov.Output),
		Query:   ov.Query,
		Cols:    splitCSV(globalFlags["cols"]),
		NoColor: ov.NoColor,
	})
}

// parseArgs splits raw args into API parameters and recognised global flags.
// Supported forms: --key value  |  --key=value  |  --bool-flag
func parseArgs(args []string) (apiParams map[string]string, globals map[string]string, err error) {
	apiParams = make(map[string]string)
	globals = make(map[string]string)

	globalFlagSet := map[string]bool{
		"profile": true, "region": true, "endpoint": true,
		"output": true, "query": true, "cols": true,
		"dry-run": true, "debug": true, "no-color": true,
	}

	i := 0
	for i < len(args) {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			return nil, nil, fmt.Errorf("unexpected argument %q: all arguments must be --flag value pairs", arg)
		}
		key := strings.TrimPrefix(arg, "--")

		// --key=value form
		if idx := strings.IndexByte(key, '='); idx >= 0 {
			val := key[idx+1:]
			key = key[:idx]
			store(globalFlagSet, globals, apiParams, key, val)
			i++
			continue
		}

		// --key value form (value does not start with --)
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			store(globalFlagSet, globals, apiParams, key, args[i+1])
			i += 2
			continue
		}

		// boolean flag: --flag with no value
		store(globalFlagSet, globals, apiParams, key, "true")
		i++
	}
	return apiParams, globals, nil
}

func store(globalSet map[string]bool, globals, apiParams map[string]string, key, val string) {
	if globalSet[key] {
		globals[key] = val
	} else {
		apiParams[key] = val
	}
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

	output := getString("output")
	if output == "" {
		output = "json"
	}

	return &config.FlagOverrides{
		Region:   getString("region"),
		Endpoint: getString("endpoint"),
		Output:   output,
		Query:    getString("query"),
		DryRun:   getBool("dry-run"),
		Debug:    getBool("debug"),
		NoColor:  getBool("no-color"),
	}
}

func printActionHelp(cmd *cobra.Command, p *meta.Product, api *meta.Api) {
	r := getResolver(strings.ToLower(p.Code))
	fmt.Printf("Usage:\n  bce %s %s [flags]\n", strings.ToLower(p.Code), api.Name)
	if sum := r.ApiSummary(api.Name); sum != "" {
		fmt.Printf("\n%s\n", sum)
	}
	fmt.Printf("\nFlags:\n")
	for _, param := range api.Parameters {
		req := "optional"
		if param.Required {
			req = "required"
		}
		desc := r.Resolve(param.DescKey)
		fmt.Printf("      --%-22s %-10s %-10s %s\n", param.Name, param.Type, req, desc)
		if len(param.SubParameters) > 0 {
			fmt.Printf("        %-24s %s\n", "示例:", buildJSONExample(param))
			for _, sub := range param.SubParameters {
				subReq := "optional"
				if sub.Required {
					subReq = "required"
				}
				subDesc := r.Resolve(sub.DescKey)
				fmt.Printf("        %-24s %-10s %-10s %s\n", sub.Name, sub.Type, subReq, subDesc)
			}
		}
	}
	fmt.Printf("\nGlobal Flags:\n")
	fmt.Printf("      --profile string    use named profile\n")
	fmt.Printf("      --region  string    override region (default: from profile)\n")
	fmt.Printf("      --endpoint string   override endpoint URL\n")
	fmt.Printf("      --output  string    output format: json|table|text (default: json)\n")
	fmt.Printf("      --query   string    JMESPath expression to filter output\n")
	fmt.Printf("      --dry-run           print request without sending\n")
	fmt.Printf("      --debug             print HTTP request/response details\n")
	fmt.Printf("      --no-color          disable colour output\n")
}

// buildJSONExample generates a compact JSON skeleton from sub_parameters.
func buildJSONExample(param meta.Parameter) string {
	fields := make([]string, 0, len(param.SubParameters))
	for _, sub := range param.SubParameters {
		fields = append(fields, fmt.Sprintf(`"%s":"..."`, sub.Name))
	}
	item := "{" + strings.Join(fields, ",") + "}"
	if strings.ToLower(param.Type) == "list" {
		return `'[` + item + `]'`
	}
	return `'` + item + `'`
}

// flattenParams returns top-level parameters only; sub_parameters are JSON object
// fields, not independent CLI flags, so they are excluded from completion registration.
func flattenParams(params []meta.Parameter) []meta.Parameter {
	return params
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

// resolverCache avoids re-loading i18n files for the same service.
var (
	resolverMu    sync.Mutex
	resolverCache = make(map[string]*meta.Resolver)
)

func getResolver(service string) *meta.Resolver {
	resolverMu.Lock()
	defer resolverMu.Unlock()
	if r, ok := resolverCache[service]; ok {
		return r
	}
	r := meta.NewResolver("zh-CN", service)
	resolverCache[service] = r
	return r
}
