package cmd

import (
	"github.com/baidubce/bce-cli/internal/openapi"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:           "bce",
	Short:         "百度云命令行工具",
	Long:          "bce 是百度云统一命令行工具，提供 BCC、VPC、EIP、BLB 等云服务的命令行操作能力。",
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute is the entry point called from main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		rootCmd.PrintErrln("Error:", err)
	}
}

func init() {
	// Global persistent flags available to every command
	pf := rootCmd.PersistentFlags()
	pf.String("profile", "", "使用指定的配置 profile（默认使用 current profile）")
	pf.String("region", "", "覆盖 region，例如：bj / gz / su")
	pf.String("endpoint", "", "覆盖服务 endpoint")
	pf.String("output", "json", "输出格式：json / table / text")
	pf.String("query", "", "JMESPath 过滤表达式，作用于 API 响应")
	pf.Bool("dry-run", false, "打印请求内容但不实际发送")
	pf.Bool("debug", false, "打印详细 HTTP 请求/响应信息")
	pf.Bool("no-color", false, "关闭 ANSI 颜色输出")

	// Dynamically register one cobra.Command per service from products.json
	openapi.RegisterCommands(rootCmd)
}
