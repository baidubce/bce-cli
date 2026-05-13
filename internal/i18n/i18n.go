package i18n

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// --- Language state ---------------------------------------------------------

var (
	langMu      sync.RWMutex
	currentLang = func() string {
		if l := os.Getenv("BCE_LANGUAGE"); l != "" {
			return l
		}
		return langFromSystem()
	}()
)

// langFromSystem infers a language code from the system $LANG env var.
// "zh_CN*" → "zh-CN", "zh_TW*" → "zh-TW", "en_*" → "en-US", others → "zh-CN".
func langFromSystem() string {
	lang := os.Getenv("LANG")
	switch {
	case strings.HasPrefix(lang, "zh_CN"):
		return "zh-CN"
	case strings.HasPrefix(lang, "zh_TW"), strings.HasPrefix(lang, "zh_HK"):
		return "zh-TW"
	case strings.HasPrefix(lang, "en_"):
		return "en-US"
	default:
		return "zh-CN"
	}
}

// SetLanguage overrides the active display language. Empty string is a no-op.
// Returns an error for unrecognised language codes.
func SetLanguage(lang string) error {
	if lang == "" {
		return nil
	}
	switch strings.ToLower(strings.ReplaceAll(lang, "_", "-")) {
	case "zh-cn":
		lang = "zh-CN"
	case "zh-tw", "zh-hk":
		lang = "zh-TW"
	case "en-us", "en":
		lang = "en-US"
	default:
		return fmt.Errorf("unsupported language %q, supported: zh-CN, en-US", lang)
	}
	langMu.Lock()
	currentLang = lang
	langMu.Unlock()
	return nil
}

// GetLanguage returns the active display language (e.g. "zh-CN", "en-US").
func GetLanguage() string {
	langMu.RLock()
	defer langMu.RUnlock()
	return currentLang
}

// --- Translation tables ------------------------------------------------------

var flagDescs = map[string]map[string]string{
	"zh-CN": {
		"profile":               "使用指定的配置 profile（默认使用 current profile）",
		"region":                "覆盖 region，例如：bj / gz / su",
		"endpoint":              "覆盖请求域名",
		"scheme":                "强制指定请求协议：http 或 https（默认由服务元数据决定）",
		"language":              "输出语言：zh-CN / en-US（默认来自 profile 或 $BCE_LANGUAGE）",
		"output":                "输出格式：json / table / text；table 支持子参数 cols=字段1,字段2 rows=JMESPath",
		"query":                 "JMESPath 过滤表达式，作用于 API 响应",
		"unfold":                "为 List/Object 参数启用 KV 点号语法",
		"dry-run":               "打印请求内容但不实际发送",
		"debug":                 "打印详细 HTTP 请求/响应信息",
		"no-color":              "关闭 ANSI 颜色输出",
		"timeout":               "HTTP 请求超时（秒），默认 15",
		"cli-input-json":        "从 JSON 文件加载请求参数，格式：file://path/to/params.json",
		"generate-cli-skeleton": "生成请求参数的 JSON 骨架并输出到 stdout",
		"pager":                 "开启自动翻页，聚合所有分页结果后输出",
		"total-count":           "限制返回的总条目数（需配合 --pager 使用），达到上限时响应中返回 nextMarker 字段作为续传游标",
		"upgrade-yes":           "跳过确认直接升级",
		"upgrade-version":       "安装指定版本，例如：0.3.0（不指定则升到最新版）",
		"cfg-delete-yes":        "跳过确认直接删除",
	},
	"en-US": {
		"profile":               "use named profile (default: current profile)",
		"region":                "override region, e.g.: bj / gz / su",
		"endpoint":              "override the request domain (hostname)",
		"scheme":                "force request scheme: http or https (default: determined by service metadata)",
		"language":              "output language: zh-CN / en-US (default: from profile or $BCE_LANGUAGE)",
		"output":                "output format: json / table / text; table supports sub-params cols=F1,F2 rows=jmespath",
		"query":                 "JMESPath expression to filter output",
		"unfold":                "enable KV dot-notation for List/Object params",
		"dry-run":               "print request without sending",
		"debug":                 "print HTTP request/response details",
		"no-color":              "disable colour output",
		"timeout":               "HTTP request timeout in seconds (default 15)",
		"cli-input-json":        "load request parameters from a JSON file, e.g.: file://path/to/params.json",
		"generate-cli-skeleton": "print a JSON parameter skeleton to stdout",
		"pager":                 "enable auto-pagination and aggregate all pages into a single output",
		"total-count":           "maximum total items to return (requires --pager); retains nextMarker in output as resume cursor when truncated",
		"upgrade-yes":           "skip confirmation and upgrade directly",
		"upgrade-version":       "install a specific version, e.g.: 0.3.0 (default: latest)",
		"cfg-delete-yes":        "skip confirmation and delete directly",
	},
}

var labels = map[string]map[string]string{
	"zh-CN": {
		// help output
		"required":       "必填",
		"optional":       "可选",
		"json-format":    "JSON 格式:",
		"kv-format":      "KV  格式:",
		"kv-unfold-hint": "（需配合 --unfold 使用）",
		"kv-repeat-hint": "（可重复，每个 --%s 为一个元素）",

		// command suggestion
		"suggest-hint": "\n你是否想输入：\n",

		// configure
		"cfg-no-profiles":     "暂无 profile，请运行 `bce configure set` 创建",
		"cfg-profile-saved":   "profile %q 已保存至 %s",
		"cfg-profile-deleted": "profile %q 已删除",
		"cfg-switched":        "已切换到 profile %q",
		"cfg-not-found":       "profile %q 不存在",
		"cfg-mode":            "模式",
		"cfg-field-name":      "名称",
		"cfg-field-mode":      "认证模式",
		"cfg-field-region":    "Region",
		"cfg-field-language":  "语言",
		"cfg-field-endpoint":  "Endpoint",
		"cfg-profile-flag":    "profile 名称（默认使用当前 profile）",
		"cfg-delete-confirm": "确认删除 profile %q？[y/N] ",

		// command descriptions
		"cmd-root-short":       "百度云命令行工具",
		"cmd-root-long":        "bce 是百度云统一命令行工具，提供云上服务的命令行操作能力。",
		"cmd-configure":        "管理凭证和配置 profile",
		"cmd-configure-set":    "配置 profile（支持交互式或通过 flag 传参）",
		"cmd-configure-list":   "列出所有 profile",
		"cmd-configure-get":    "查看指定 profile 的配置详情",
		"cmd-configure-use":    "切换当前使用的 profile",
		"cmd-configure-delete": "删除指定 profile",
		"cmd-version":          "显示版本信息",
		"cmd-completion":       "生成 shell 自动补全脚本",
		"cmd-help":             "获取命令帮助信息",

		// upgrade
		"cmd-upgrade":         "升级 BCE CLI 到最新版本",
		"upgrade-checking":    "正在检查最新版本...",
		"upgrade-up-to-date":  "已是最新版本 %s",
		"upgrade-available":   "发现新版本 %s（当前版本 %s）",
		"upgrade-confirm":     "是否升级？[y/N] ",
		"upgrade-downloading": "正在下载 %s...",
		"upgrade-success":     "升级成功，当前版本 %s",
		"upgrade-cancelled":   "已取消",
		"upgrade-pinned-confirm-msg": "即将安装 %s（当前版本 %s）",
	},
	"en-US": {
		// help output
		"required":       "required",
		"optional":       "optional",
		"json-format":    "JSON format:",
		"kv-format":      "KV  format:",
		"kv-unfold-hint": "(requires --unfold)",
		"kv-repeat-hint": "(repeatable, each --%s is one element)",

		// command suggestion
		"suggest-hint": "\nDid you mean:\n",

		// configure
		"cfg-no-profiles":     "No profiles configured. Run `bce configure set` to create one.",
		"cfg-profile-saved":   "Profile %q saved to %s",
		"cfg-profile-deleted": "Profile %q deleted.",
		"cfg-switched":        "Switched to profile %q",
		"cfg-not-found":       "profile %q not found",
		"cfg-mode":            "mode",
		"cfg-field-name":      "Name",
		"cfg-field-mode":      "Mode",
		"cfg-field-region":    "Region",
		"cfg-field-language":  "Language",
		"cfg-field-endpoint":  "Endpoint",
		"cfg-profile-flag":    "profile name (default: current profile)",
		"cfg-delete-confirm": "Delete profile %q? [y/N] ",

		// command descriptions
		"cmd-root-short":       "Baidu Cloud CLI",
		"cmd-root-long":        "bce is the Baidu Cloud unified CLI for cloud services.",
		"cmd-configure":        "Manage credentials and profiles",
		"cmd-configure-set":    "Configure a profile (interactive or via flags)",
		"cmd-configure-list":   "List all profiles",
		"cmd-configure-get":    "Show profile configuration details",
		"cmd-configure-use":    "Switch the active profile",
		"cmd-configure-delete": "Delete a profile",
		"cmd-version":          "Show version information",
		"cmd-completion":       "Generate shell autocompletion script",
		"cmd-help":             "Help about any command",

		// upgrade
		"cmd-upgrade":         "Upgrade BCE CLI to the latest version",
		"upgrade-checking":    "Checking for latest version...",
		"upgrade-up-to-date":  "Already up to date: %s",
		"upgrade-available":   "New version available: %s (current: %s)",
		"upgrade-confirm":     "Upgrade now? [y/N] ",
		"upgrade-downloading": "Downloading %s...",
		"upgrade-success":     "Upgrade complete. Current version: %s",
		"upgrade-cancelled":   "Cancelled.",
		"upgrade-pinned-confirm-msg": "About to install %s (current: %s)",
	},
}

// --- Lookup helpers ----------------------------------------------------------

// FlagDesc returns the description for a global CLI flag, falling back to zh-CN.
func FlagDesc(lang, flagName string) string {
	if descs, ok := flagDescs[lang]; ok {
		if desc, ok := descs[flagName]; ok {
			return desc
		}
	}
	return flagDescs["zh-CN"][flagName]
}

// T returns a UI label string for the given language, falling back to zh-CN.
func T(lang, key string) string {
	if m, ok := labels[lang]; ok {
		if s, ok := m[key]; ok {
			return s
		}
	}
	return labels["zh-CN"][key]
}
