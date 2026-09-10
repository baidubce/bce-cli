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
		"auth-no-browser":       "不自动打开浏览器，仅打印授权链接",
		"auth-iam-base":         "覆盖 IAM 地址（默认使用线上地址）",
		"auth-sts-base":         "覆盖 STS 地址（默认使用线上地址）",
		"auth-callback-timeout": "等待浏览器回调的超时时间（秒）",
		"auth-format":           "输出格式：text / json（默认 text）；业务结果（登录成功/失败/已登录等）用 json 输出的 success/authenticated 字段判断，退出码固定 0；配置文件读写等基础设施错误仍会以非 JSON 文本写入 stderr 并返回非 0 退出码",
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
		"auth-no-browser":       "do not open a browser; only print the authorization URL",
		"auth-iam-base":         "override the IAM base address (default: the production address)",
		"auth-sts-base":         "override the STS base address (default: the production address)",
		"auth-callback-timeout": "seconds to wait for the browser callback",
		"auth-format":           "output format: text / json (default text); check success/authenticated in the json output for the business result (login success/failure/already logged in, etc.), which always exits 0; infrastructure errors (e.g. reading/writing the config file) still print plain text to stderr and exit non-zero",
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
		"cfg-delete-confirm":  "确认删除 profile %q？[y/N] ",

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
		"cmd-upgrade":                "升级 BCE CLI 到最新版本",
		"upgrade-checking":           "正在检查最新版本...",
		"upgrade-up-to-date":         "已是最新版本 %s",
		"upgrade-available":          "发现新版本 %s（当前版本 %s）",
		"upgrade-confirm":            "是否升级？[y/N] ",
		"upgrade-downloading":        "正在下载 %s...",
		"upgrade-success":            "升级成功，当前版本 %s",
		"upgrade-cancelled":          "已取消",
		"upgrade-pinned-confirm-msg": "即将安装 %s（当前版本 %s）",

		// auth (OAuth browser login)
		"cmd-auth":               "通过浏览器登录管理临时凭证（IAM OAuth）",
		"cmd-auth-login":         "打开浏览器完成 IAM OAuth 登录并获取临时凭证",
		"cmd-auth-logout":        "清除本地 OAuth 登录状态",
		"cmd-auth-status":        "查看当前 OAuth 登录状态",
		"cmd-auth-refresh":       "续期本地 OAuth 临时凭证",
		"auth-open-hint":         "请在浏览器中完成登录，若未自动打开请手动访问：",
		"auth-waiting":           "正在等待浏览器回调...",
		"auth-login-success":     "登录成功",
		"auth-refreshed":         "临时凭证已就绪（如已过期会自动续期）",
		"auth-logged-out":        "登出成功：本地登录状态已清除",
		"auth-not-logged-in":     "尚未登录，请运行 `bce auth login`",
		"auth-login-hint":        "请运行 `bce auth login` 为该 profile 获取凭证",
		"auth-current-switched":  "当前 profile 已切换为 %q（用 `bce configure use <name>` 可切回）",
		"auth-not-current":       "当前 profile 是 %q，并非该 OAuth 登录；运行 `bce configure use %s` 切换后即可生效",
		"auth-profile-converted": "警告：profile %q 将转为 OAuth 模式，其 access_key_id / secret_access_key / security_token 会被清除",
		"auth-insecure-base":     "警告：%s 不是 HTTPS，凭证将以明文经过网络",
		"auth-ak-ignored":        "警告：OAuth 模式下会忽略 access-key-id / secret-access-key / security-token",
		"auth-already":           "已登录，Account ID：%s",
		"auth-already-anonymous": "已登录",
		"auth-already-expires":   "凭证将在 %s 后过期",
		"auth-already-relogin":   "如需重新登录，请先执行：%s",
		"auth-field-profile":     "Profile",
		"auth-field-iam":         "IAM 地址",
		"auth-field-sts":         "STS 地址",
		"auth-field-user":        "登录用户",
		"auth-field-account":     "账号 ID",
		"auth-field-type":        "主体类型",
		"auth-field-scope":       "授权范围",
		"auth-field-idexp":       "ID Token 过期时间",
		"auth-field-stsexp":      "临时凭证过期时间",
		"auth-field-rtexp":       "Refresh Token 过期时间",
		"auth-authenticated":     "已登录",
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
		"cfg-delete-confirm":  "Delete profile %q? [y/N] ",

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
		"cmd-upgrade":                "Upgrade BCE CLI to the latest version",
		"upgrade-checking":           "Checking for latest version...",
		"upgrade-up-to-date":         "Already up to date: %s",
		"upgrade-available":          "New version available: %s (current: %s)",
		"upgrade-confirm":            "Upgrade now? [y/N] ",
		"upgrade-downloading":        "Downloading %s...",
		"upgrade-success":            "Upgrade complete. Current version: %s",
		"upgrade-cancelled":          "Cancelled.",
		"upgrade-pinned-confirm-msg": "About to install %s (current: %s)",

		// auth (OAuth browser login)
		"cmd-auth":               "Manage temporary credentials via browser login (IAM OAuth)",
		"cmd-auth-login":         "Log in with IAM OAuth in a browser and obtain temporary credentials",
		"cmd-auth-logout":        "Clear the local OAuth login state",
		"cmd-auth-status":        "Show the current OAuth login state",
		"cmd-auth-refresh":       "Renew the local OAuth temporary credentials",
		"auth-open-hint":         "Complete the login in your browser. If it did not open, visit:",
		"auth-waiting":           "Waiting for the browser callback...",
		"auth-login-success":     "Logged in",
		"auth-refreshed":         "Temporary credentials are ready (renewed if they had expired)",
		"auth-logged-out":        "Logged out successfully: local login state cleared",
		"auth-not-logged-in":     "not logged in; run `bce auth login`",
		"auth-login-hint":        "run `bce auth login` to obtain credentials for this profile",
		"auth-current-switched":  "current profile switched to %q (use `bce configure use <name>` to switch back)",
		"auth-not-current":       "current profile is %q, not this OAuth login; run `bce configure use %s` to make it take effect",
		"auth-profile-converted": "warning: profile %q will be converted to OAuth mode; its access_key_id / secret_access_key / security_token will be cleared",
		"auth-insecure-base":     "warning: %s is not HTTPS; credentials will traverse the network in plaintext",
		"auth-ak-ignored":        "warning: access-key-id / secret-access-key / security-token are ignored in OAuth mode",
		"auth-already":           "Already authenticated as %s",
		"auth-already-anonymous": "Already authenticated",
		"auth-already-expires":   "Token expires in %s",
		"auth-already-relogin":   "To re-login, run: %s first",
		"auth-field-profile":     "Profile",
		"auth-field-iam":         "IAM base",
		"auth-field-sts":         "STS base",
		"auth-field-user":        "User",
		"auth-field-account":     "Account ID",
		"auth-field-type":        "Principal type",
		"auth-field-scope":       "Scope",
		"auth-field-idexp":       "ID Token expires",
		"auth-field-stsexp":      "Credentials expire",
		"auth-field-rtexp":       "Refresh Token expires",
		"auth-authenticated":     "Authenticated",
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
