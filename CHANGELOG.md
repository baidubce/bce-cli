2026-06-18 Version: v0.1.29
- 新增BCC事件管理及预授权规则相关接口

2026-06-18 Version: v0.1.28
- 新增STS临时身份凭证相关接口

2026-06-17 Version: v0.1.27
- 新增BCC专属集群、预留实例券及EHC集群相关接口

2026-06-17 Version: v0.1.26
- BLS日志服务多接口字段结构调整

2026-06-17 Version: v0.1.25
- BLB

2026-06-17 Version: v0.1.24
- 新增BLS日志服务全套管理接口

2026-06-17 Version: v0.1.23
- 新增BCM报警模版、实例组及通知模版相关接口

2026-06-17 Version: v0.1.22
- BLB SDK版本更新

2026-06-16 Version: v0.1.21
- BCC

2026-06-16 Version: v0.1.20
- BLB

2026-06-16 Version: v0.1.19
- CFW

2026-06-16 Version: v0.1.18
- 新增CFW网络型策略管理接口并完善应用型策略相关能力

2026-06-16 Version: v0.1.17
- BLS

2026-06-16 Version: v0.1.16
- Try to update baidu-cc client, current version is [2.1.158.2] ...

2026-06-16 Version: v0.1.15
- Try to update baidu-cc client, current version is [2.1.158.2] ...

2026-06-16 Version: v0.1.14
- 新增ET删除物理专线接口

2026-06-16 Version: v0.1.13
- RAPIDFS

2026-06-15 Version: v0.1.12
- AIHC SDK版本更新

2026-06-15 Version: v0.1.11
- 新增BCC实例全量管理接口

2026-06-15 Version: v0.1.10
- RAPIDFS元数据同步规则及缓存任务字段命名规范化

2026-06-12 Version: v0.1.9
- AIHC SDK版本更新

2026-06-11 Version: v0.1.8
- Try to update baidu-cc client, current version is [2.1.158.1] ...

2026-06-04 Version: v0.1.7
- [ops] sync all merged metadata for RAPIDFS

2026-06-04 Version: v0.1.6
- [ops] sync all merged metadata for pfs

2026-06-04 Version: v0.1.5
- [ops] sync all merged metadata for bls

2026-06-03 Version: v0.1.4
- RAPIDFS多个接口移除query.action字段

2026-06-03 Version: v0.1.3
- CCR SDK版本更新

2026-06-03 Version: v0.1.2
- 新增PFS数据流动任务与生命周期规则相关接口

2026-06-03 Version: v0.1.1
- workflows

2026-05-15 Version: v0.1.0
- **多 Profile 管理**：支持多账号配置，`bce configure` 子命令完整覆盖增删改查切换
- **灵活参数格式**：复杂参数支持 JSON 字符串和 `--unfold` KV 点号两种传入方式
- **`--cli-input-json`**：从 JSON 文件加载请求参数，支持 `--generate-cli-skeleton` 生成参数骨架
- **输出格式**：支持 `json`（默认）、`table`（含 `rows=` / `cols=` 子参数）、`text` 三种格式
- **`--query`**：JMESPath 表达式过滤响应结果
- **调试工具**：`--dry-run` 打印请求内容不发送，`--debug` 打印完整 HTTP 请求/响应
- **自动补全**：支持 Bash、Zsh、Fish、PowerShell Tab 补全
- **智能命令建议**：输入错误命令时自动推荐最近似命令
- **多语言支持**：`--language zh-CN / en-US`，优先级：flag > profile > `$BCE_LANGUAGE` > 系统语言 > 默认中文
- **环境变量**：支持通过 `BCE_ACCESS_KEY_ID`、`BCE_SECRET_ACCESS_KEY`、`BCE_SECURITY_TOKEN`、`BCE_REGION`、`BCE_LANGUAGE` 配置