# BCE CLI

[English](README_en.md)

百度云命令行工具（BCE CLI）是管理百度云资源的统一命令行工具。通过 BCE CLI，您可以在终端中快速调用百度云 API，管理 BCC、VPC、EIP、BLB 等云服务资源，也可以将其集成到自动化脚本和 CI/CD 流程中。

---

## 目录

- [安装](#安装)
- [配置](#配置)
- [基本用法](#基本用法)
- [参数格式](#参数格式)
- [输出格式](#输出格式)
- [高级功能](#高级功能)
- [升级](#升级)
- [自动补全](#自动补全)
- [全局参数](#全局参数)
- [环境变量](#环境变量)

---

## 安装

### 下载预编译二进制

前往 [Releases](https://github.com/baidubce/bce-cli/releases) 页面下载对应操作系统的版本。

**macOS / Linux**

解压后将 `bce` 可执行文件移动到 `$PATH` 目录：

```bash
# macOS (Apple Silicon)
tar -xzf bce-cli-darwin-arm64.tar.gz
sudo mv bce /usr/local/bin/

# macOS (Intel)
tar -xzf bce-cli-darwin-amd64.tar.gz
sudo mv bce /usr/local/bin/

# Linux (amd64)
tar -xzf bce-cli-linux-amd64.tar.gz
sudo mv bce /usr/local/bin/
```

**Windows**

下载 `bce-cli-windows-amd64.zip`，解压后将 `bce.exe` 放到已加入 `PATH` 的目录，或在 PowerShell 中执行以下命令将其安装到用户目录：

```powershell
# 解压（PowerShell 5.1+）
Expand-Archive bce-cli-windows-amd64.zip -DestinationPath .

# 创建用户 bin 目录并加入 PATH（首次使用执行一次）
New-Item -ItemType Directory -Force "$env:USERPROFILE\bin" | Out-Null
[Environment]::SetEnvironmentVariable("Path", $env:Path + ";$env:USERPROFILE\bin", "User")

# 移动 bce.exe
Move-Item bce.exe "$env:USERPROFILE\bin\"
```

验证安装：

```bash
bce version
```

### 从源码编译

需要 Go 1.21 及以上版本：

```bash
git clone https://github.com/baidubce/bce-cli.git
cd bce-cli

# macOS / Linux
go build -o bce .
sudo mv bce /usr/local/bin/

# Windows（PowerShell）
go build -o bce.exe .
Move-Item bce.exe $env:USERPROFILE\bin\
```

---

## 配置

### 交互式配置

不带任何 credential flag 执行 `configure set`，按提示逐步输入：

```bash
# 配置默认 profile（AK 模式）
bce configure set
Access Key Id: YOUR_ACCESS_KEY_ID
Secret Access Key: YOUR_SECRET_ACCESS_KEY
Region: bj

# 配置指定 profile（AK 模式）
bce configure set prod

# 配置 STS 临时凭证——仅传 --mode StsToken
bce configure set sts --mode StsToken
Access Key Id: TEMP_AK
Secret Access Key: TEMP_SK
Region: bj
Security Token: YOUR_STS_TOKEN
```

### 非交互式配置

适合脚本或 CI/CD 流程，通过 flag 直接传入参数，跳过交互提示：

```bash
# 配置默认 profile
bce configure set --access-key-id YOUR_AK --secret-access-key YOUR_SK --region bj

# 配置指定 profile
bce configure set prod --access-key-id YOUR_AK --secret-access-key YOUR_SK --region gz

# 仅更新部分字段（其他字段保持不变）
bce configure set prod --region su

# 配置 STS 临时凭证
bce configure set sts \
  --access-key-id TEMP_AK \
  --secret-access-key TEMP_SK \
  --security-token YOUR_STS_TOKEN \
  --mode StsToken
```

配置文件保存在 `~/.bce/config.json`。

### 多 Profile 管理

BCE CLI 支持多个 profile，适用于同时管理多个账号或环境：

```bash
# 创建名为 prod 的 profile
bce configure set prod

# 查看指定 profile 的配置
bce configure get prod

# 列出所有 profile
bce configure list

# 切换当前使用的 profile
bce configure use prod

# 删除 profile
bce configure delete prod
```

默认使用 current profile，需要临时切换时可通过 `--profile` 指定：

```bash
bce vpc DescribeVpcs --profile prod
```

### 配置文件格式

`~/.bce/config.json` 示例：

```json
{
  "current": "default",
  "profiles": [
    {
      "name": "default",
      "mode": "AK",
      "access_key_id": "YOUR_AK",
      "secret_access_key": "YOUR_SK",
      "region": "bj"
    },
    {
      "name": "sts",
      "mode": "StsToken",
      "access_key_id": "TEMP_AK",
      "secret_access_key": "TEMP_SK",
      "security_token": "YOUR_STS_TOKEN",
      "region": "bj"
    }
  ]
}
```

---

## 基本用法

```bash
bce <服务名> <接口名> [--参数名 参数值 ...]
```

查看帮助：

```bash
# 查看支持的服务列表
bce --help

# 查看服务支持的接口列表
bce vpc --help

# 查看接口的参数说明
bce vpc CreateVpc --help
```

调用接口示例：

```bash
# 查询 VPC 列表
bce vpc DescribeVpcs

# 创建 VPC
bce vpc CreateVpc --name myVpc --cidr 192.168.0.0/16

# 查询 VPN 网关列表
bce vpc DescribeVpns --vpcId vpc-xxxxxxxx
```

---

## 参数格式

### 简单参数

```bash
bce vpc CreateVpc --name myVpc --cidr 192.168.0.0/16
```

### JSON 格式（复杂参数默认方式）

对于 List 或 Object 类型的参数，使用 JSON 字符串传递：

```bash
# 数组参数
bce vpc CreateVpc \
  --name myVpc \
  --cidr 192.168.0.0/16 \
  --tags '[{"tagKey":"env","tagValue":"prod"},{"tagKey":"team","tagValue":"infra"}]'

# 对象参数
bce vpc CreateVpn \
  --vpcId vpc-xxxxxxxx \
  --vpnName myVpn \
  --resourceGroupId rg-xxxxxxxx \
  --billing '{"paymentTiming":"Postpaid"}'
```

### KV 点号格式（需加 --unfold）

对于结构化参数，可以使用点号嵌套的 KV 格式替代 JSON，需要加 `--unfold` 开关：

```bash
# 单层对象
bce vpc CreateVpn --unfold \
  --billing paymentTiming=Postpaid

# 多层嵌套对象（点号分隔层级）
bce vpc CreateVpn --unfold \
  --billing paymentTiming=PrePaid reservation.reservationLength=1 reservation.reservationTimeUnit=month

# List 参数（每个 --tags 为一个数组元素）
bce vpc CreateVpc --unfold \
  --tags tagKey=env tagValue=prod \
  --tags tagKey=team tagValue=infra
```

### 从文件加载参数

```bash
# 生成参数骨架
bce vpc CreateVpc --generate-cli-skeleton > params.json

# 编辑 params.json 填入真实值，然后加载
bce vpc CreateVpc --cli-input-json file://params.json

# 支持 ~ 路径
bce vpc CreateVpc --cli-input-json file://~/.bce/CreateVpc.json
```

命令行参数优先级高于文件中的参数，可以用命令行覆盖文件中的某个值：

```bash
bce vpc CreateVpc \
  --cli-input-json file://params.json \
  --name override-name
```

---

## 输出格式

BCE CLI 支持三种输出格式：`json`（默认）、`table`、`text`。

### JSON 输出（默认）

```bash
bce vpc DescribeVpcs
```

```json
{
  "vpcs": [
    {
      "vpcId": "vpc-xxxxxxxx",
      "name": "myVpc",
      "cidr": "192.168.0.0/16"
    }
  ]
}
```

### 表格输出

使用 `--output table` 输出表格，可附加 `cols=` 和 `rows=` 子参数：

```bash
# 指定行来源，自动推断列名
bce vpc DescribeVpcs --output table rows=vpcs

# 指定行来源和列名
bce vpc DescribeVpcs --output table rows=vpcs cols=vpcId,name,cidr
```

输出示例：

```
cidr              name    vpcId
192.168.0.0/16    myVpc   vpc-xxxxxxxx
```

`--output` 子参数说明：

| 子参数    | 说明                                                                   |
| --------- | ---------------------------------------------------------------------- |
| `rows=` | JMESPath 表达式，指定作为表格行来源的数组，在 `--query` 执行之后运行 |
| `cols=` | 逗号分隔的列名，需与 JSON 字段名对应，例如 `cols=vpcId,name,cidr`    |

### JMESPath 过滤

使用 `--query` 对响应做 JMESPath 过滤，适用于所有输出格式：

```bash
# 只输出第一个 VPC 的 ID
bce vpc DescribeVpcs --query 'vpcs[0].vpcId'

# 输出所有 VPC 的名称列表
bce vpc DescribeVpcs --query 'vpcs[].name'

# 提取指定字段并重命名
bce vpc DescribeVpcs --query 'vpcs[].{ID:vpcId,Name:name}'
```

`--query` 与 `rows=` 按两阶段流水线顺序执行：先对原始响应运行 `--query`，再对其输出运行 `rows=`。两者可独立使用，也可组合：

```bash
# --query 先变换响应，rows= 再从变换结果中提取数组
bce vpc DescribeVpcs \
  --query '{matched: vpcs[?starts_with(cidr, `192.168`)]}' \
  --output table rows=matched cols=vpcId,name,cidr
```

### Text 输出

适用于输出标量值，配合 `--query` 使用：

```bash
bce vpc DescribeVpcs --query 'vpcs[0].vpcId' --output text
```

---

## 高级功能

### 指定 Region

通过 `--region` 指定请求的目标地域，也可以在 profile 中配置 `region` 字段作为默认值：

```bash
# 指定广州地域
bce vpc DescribeVpcs --region gz
```

支持的 region：`bj`（北京）、`gz`（广州）、`su`（苏州）、`bd`（保定）、`fwh`（武汉）、`nj`（南京）、`yq`（阳泉）、`cd`（成都）、`hkg`（香港）、`global`（全局）。

### 预演模式（Dry Run）

打印即将发送的请求内容，不实际发送：

```bash
bce vpc CreateVpc \
  --name myVpc \
  --cidr 192.168.0.0/16 \
  --dry-run
```

```
[DRY-RUN] POST https://bcc.bj.baidubce.com/v1/vpc
[DRY-RUN] Body:
{
  "cidr": "192.168.0.0/16",
  "name": "myVpc"
}
```

### 调试模式

打印 CLI 构造的请求参数和响应详情：

```bash
bce vpc DescribeVpcs --debug
```

```
[DEBUG] > GET https://bcc.bj.baidubce.com/v1/vpc
[DEBUG] < Status: 200
[DEBUG] < Body:
{ ... }
```

### 智能命令建议

输入错误命令时，CLI 会自动推荐最近似的命令：

```bash
bce vpc DescribeVpc  # 拼写有误
```

```
你是否想输入：
  bce vpc DescribeVpcs
  bce vpc DescribeVpns
```

### 指定请求协议

通过 `--scheme` 强制指定请求使用 `http` 或 `https` 协议。若不指定，默认使用 `https`：

```bash
# 强制使用 http
bce vpc DescribeVpcs --scheme http

# 强制使用 https
bce vpc DescribeVpcs --scheme https
```

> 注意：若指定的协议不被目标服务支持，命令将报错退出。

### 自动分页

对于支持分页的 List 类接口，默认返回第一页原始响应。使用 `--pager` 开关可开启自动翻页，将所有分页结果聚合后一次性返回：

```bash
# 默认：仅返回第一页
bce vpc DescribeVpcs

# 开启自动翻页，聚合所有分页结果
bce vpc DescribeVpcs --pager
```

请求参数中含有 `marker` 字段的列表类接口支持分页，可通过 `<接口名> --help` 查看是否出现 `Pagination Flags` 段来确认。

分页相关 flag：

| Flag                  | 说明                                                                                                      |
| --------------------- | --------------------------------------------------------------------------------------------------------- |
| `--pager`           | 开启自动翻页，聚合所有分页结果后输出                                                                     |
| `--total-count <N>` | 限制返回的总条目数（需配合 `--pager` 使用）；达到上限后停止翻页，响应中返回 `nextMarker` 字段作为续传游标 |

分页游标和每页大小通过 API 原生参数传递，无需额外 flag：

```bash
# 开启翻页，每页 20 条（maxKeys 为 API 原生参数）
bce vpc DescribeVpcs --pager --maxKeys 20

# 从指定位置开始翻页（marker 为 API 原生参数）
bce vpc DescribeVpcs --pager --marker vpc-xxxxxxxx

# 最多返回 50 条；达到上限时响应中返回 nextMarker 字段作为续传游标
bce vpc DescribeVpcs --pager --total-count 50

# 将上次响应中的 nextMarker 值作为 --marker 传入，从中断处继续翻页
bce vpc DescribeVpcs --pager --total-count 50 --marker <上次响应中的 nextMarker 值>
```

> 对不支持分页的接口使用 `--pager` / `--total-count` 时会报错。`--total-count` 必须与 `--pager` 一起使用。

### 多语言支持

BCE CLI 支持中文和英文两种语言：

```bash
# 使用英文
bce vpc CreateVpc --help --language en-US

# 使用中文（默认）
bce vpc CreateVpc --help --language zh-CN
```

也可以在 profile 中设置默认语言（`language: "en-US"`），或通过环境变量 `BCE_LANGUAGE` 指定。

---

## 升级

BCE CLI 内置自更新命令，无需手动下载安装包。

### 升级到最新版本

```bash
bce upgrade
```

执行后会先检查 GitHub Releases 上的最新版本，若有新版本则交互询问是否升级：

```
正在检查最新版本...
发现新版本 0.2.0（当前版本 0.1.0）
是否升级？[y/N] y
正在下载 0.2.0...
升级成功，当前版本 0.2.0
```

### 安装指定版本（降级/锁定版本）

```bash
bce upgrade --version 0.3.0
```

指定版本时跳过最新版本检查，直接下载并安装目标版本（可用于回退到旧版）：

```
即将安装 0.3.0（当前版本 0.4.0）
是否升级？[y/N] y
正在下载 0.3.0...
升级成功，当前版本 0.3.0
```

### 跳过确认（适合 CI/CD）

```bash
bce upgrade --yes
bce upgrade --version 0.3.0 --yes
```

升级命令的 flag 说明：

| Flag              | 说明                                      |
| ----------------- | ----------------------------------------- |
| `--version`     | 安装指定版本，例如：`0.3.0`（默认升到最新版） |
| `--yes` / `-y` | 跳过确认直接升级                          |

> 权限不足时，macOS/Linux 请以 `sudo bce upgrade` 运行，Windows 请以管理员身份运行。

---

## 自动补全

BCE CLI 支持 Bash、Zsh、Fish、PowerShell 的 Tab 自动补全。

### Bash

```bash
bce completion bash > /etc/bash_completion.d/bce
# 或加到 ~/.bashrc：
echo 'source <(bce completion bash)' >> ~/.bashrc
source ~/.bashrc
```

### Zsh

```bash
echo "autoload -U compinit; compinit" >> ~/.zshrc
bce completion zsh > "${fpath[1]}/_bce"
source ~/.zshrc
```

### Fish

```bash
bce completion fish > ~/.config/fish/completions/bce.fish
```

### PowerShell

```powershell
bce completion powershell > $env:USERPROFILE\Documents\PowerShell\Microsoft.PowerShell_profile.ps1
# 或追加到已有 profile：
bce completion powershell >> $PROFILE
```

---

## 全局参数

以下参数适用于所有接口：

| 参数                        | 类型   | 说明                                                                              |
| --------------------------- | ------ | --------------------------------------------------------------------------------- |
| `--profile`               | string | 使用指定的 profile（默认使用 current profile）                                    |
| `--region`                | string | 覆盖 region，例如：`bj` / `gz` / `su`                                       |
| `--endpoint`              | string | 覆盖请求域名                                                                      |
| `--output`                | string | 输出格式：`json` / `table` / `text`；table 支持 `cols=字段` `rows=路径` |
| `--query`                 | string | JMESPath 表达式，对响应结果进行过滤                                               |
| `--language`              | string | 输出语言：`zh-CN` / `en-US`                                                   |
| `--cli-input-json`        | string | 从 JSON 文件加载请求参数，格式：`file://path/to/params.json`                    |
| `--generate-cli-skeleton` |        | 生成请求参数的 JSON 骨架并输出到 stdout                                           |
| `--unfold`                |        | 为 List/Object 参数启用 KV 点号语法                                               |
| `--dry-run`               |        | 打印请求内容但不实际发送                                                          |
| `--debug`                 |        | 打印详细 HTTP 请求/响应信息                                                       |
| `--scheme`                | string | 强制指定请求协议：`http` 或 `https`                                           |
| `--no-color`              |        | 关闭 ANSI 颜色输出                                                                |
| `--timeout`               | int    | HTTP 请求超时（秒），默认 15                                                      |

---

## 环境变量

如果没有在配置文件中指定，BCE CLI 会尝试从以下环境变量读取配置：

| 环境变量                  | 说明                            | 优先级（高 → 低）                                                              |
| ------------------------- | ------------------------------- | ------------------------------------------------------------------------------ |
| `BCE_ACCESS_KEY_ID`     | Access Key ID                   | `--profile` 指定的 profile > 环境变量 > 默认 profile                          |
| `BCE_SECRET_ACCESS_KEY` | Secret Access Key               | `--profile` 指定的 profile > 环境变量 > 默认 profile                          |
| `BCE_SECURITY_TOKEN`    | STS 临时安全令牌                | `--profile` 指定的 profile > 环境变量 > 默认 profile                          |
| `BCE_REGION`            | 默认 region                     | `--region` 参数 > `--profile` 指定的 profile > 环境变量 > 默认 profile        |
| `BCE_LANGUAGE`          | 显示语言：`zh-CN` / `en-US` | `--language` 参数 > profile 配置 > 环境变量 > 系统语言（`$LANG`）> `zh-CN` |
