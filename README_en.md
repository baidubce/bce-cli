# BCE CLI

[中文](README.md)

BCE CLI (Baidu Cloud CLI) is a unified command-line tool for managing Baidu Cloud resources. It lets you call Baidu Cloud APIs directly from a terminal to manage services such as BCC, VPC, EIP, and BLB, and can be integrated into automation scripts and CI/CD pipelines.

---

## Table of Contents

- [Installation](#installation)
- [Configuration](#configuration)
- [Basic Usage](#basic-usage)
- [Parameter Formats](#parameter-formats)
- [Output Formats](#output-formats)
- [Advanced Features](#advanced-features)
- [Upgrade](#upgrade)
- [Shell Completion](#shell-completion)
- [Global Flags](#global-flags)
- [Environment Variables](#environment-variables)

---

## Installation

### Download Pre-built Binaries

Visit the [Releases](https://github.com/baidubce/bce-cli/releases) page and download the archive for your operating system.

**macOS / Linux**

Extract the archive and move the `bce` binary to a directory in your `$PATH`:

```bash
# macOS (Apple Silicon)
tar -xzf bce-macosx-0.1.0-arm64.tar.gz
sudo mv bce /usr/local/bin/

# macOS (Intel)
tar -xzf bce-macosx-0.1.0-amd64.tar.gz
sudo mv bce /usr/local/bin/

# Linux (amd64)
tar -xzf bce-linux-0.1.0-amd64.tar.gz
sudo mv bce /usr/local/bin/
```

**Windows**

Download `bce-windows-0.1.0-amd64.zip`, extract it, and place `bce.exe` in a directory that is on your `PATH`. Alternatively, run the following in PowerShell to install it to your user directory:

```powershell
# Extract (PowerShell 5.1+)
Expand-Archive bce-windows-0.1.0-amd64.zip -DestinationPath .

# Create a user bin directory and add it to PATH (run once)
New-Item -ItemType Directory -Force "$env:USERPROFILE\bin" | Out-Null
[Environment]::SetEnvironmentVariable("Path", $env:Path + ";$env:USERPROFILE\bin", "User")

# Move bce.exe
Move-Item bce.exe "$env:USERPROFILE\bin\"
```

Verify the installation:

```bash
bce version
```

### Build from Source

Requires Go 1.21 or later:

```bash
git clone https://github.com/baidubce/bce-cli.git
cd bce-cli

# macOS / Linux
go build -o bce .
sudo mv bce /usr/local/bin/

# Windows (PowerShell)
go build -o bce.exe .
Move-Item bce.exe $env:USERPROFILE\bin\
```

---

## Configuration

### Interactive Setup

Run `configure set` without any credential flags and follow the prompts:

```bash
# Configure the default profile (AK mode)
bce configure set
Access Key Id: YOUR_ACCESS_KEY_ID
Secret Access Key: YOUR_SECRET_ACCESS_KEY
Region: bj

# Configure a named profile (AK mode)
bce configure set prod

# Configure STS temporary credentials — pass --mode StsToken only
bce configure set sts --mode StsToken
Access Key Id: TEMP_AK
Secret Access Key: TEMP_SK
Region: bj
Security Token: YOUR_STS_TOKEN
```

### Non-interactive Setup

Suitable for scripts and CI/CD pipelines — pass credentials directly as flags to skip the interactive prompts:

```bash
# Configure the default profile
bce configure set --access-key-id YOUR_AK --secret-access-key YOUR_SK --region bj

# Configure a named profile
bce configure set prod --access-key-id YOUR_AK --secret-access-key YOUR_SK --region gz

# Update only specific fields (other fields remain unchanged)
bce configure set prod --region su

# Configure STS temporary credentials
bce configure set sts \
  --access-key-id TEMP_AK \
  --secret-access-key TEMP_SK \
  --security-token YOUR_STS_TOKEN \
  --mode StsToken
```

The configuration file is saved to `~/.bce/config.json`.

### Multiple Profiles

BCE CLI supports multiple profiles, making it easy to manage multiple accounts or environments:

```bash
# Create a profile named "prod"
bce configure set prod

# Show a specific profile
bce configure get prod

# List all profiles
bce configure list

# Switch the active profile
bce configure use prod

# Delete a profile
bce configure delete prod
```

The current (active) profile is used by default. Use `--profile` only when you need to temporarily switch:

```bash
bce vpc DescribeVpcs --profile prod
```

### Configuration File Format

Example `~/.bce/config.json`:

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

## Basic Usage

```bash
bce <service> <action> [--param value ...]
```

Getting help:

```bash
# List available services
bce --help

# List actions for a service
bce vpc --help

# Show parameters for an action
bce vpc CreateVpc --help
```

Examples:

```bash
# List VPCs
bce vpc DescribeVpcs

# Create a VPC
bce vpc CreateVpc --name myVpc --cidr 192.168.0.0/16

# List VPN gateways
bce vpc DescribeVpns --vpcId vpc-xxxxxxxx
```

---

## Parameter Formats

### Simple Parameters

```bash
bce vpc CreateVpc --name myVpc --cidr 192.168.0.0/16
```

### JSON Format (default for structured types)

For `List` or `Object` parameters, pass a JSON string:

```bash
# Array parameter
bce vpc CreateVpc \
  --name myVpc \
  --cidr 192.168.0.0/16 \
  --tags '[{"tagKey":"env","tagValue":"prod"},{"tagKey":"team","tagValue":"infra"}]'

# Object parameter
bce vpc CreateVpn \
  --vpcId vpc-xxxxxxxx \
  --vpnName myVpn \
  --resourceGroupId rg-xxxxxxxx \
  --billing '{"paymentTiming":"Postpaid"}'
```

### KV Dot-Notation (requires --unfold)

Structured parameters can be passed as dot-separated key-value pairs instead of JSON. The `--unfold` flag must be provided:

```bash
# Single-level object
bce vpc CreateVpn --unfold \
  --billing paymentTiming=Postpaid

# Nested object (dot notation for nested fields)
bce vpc CreateVpn --unfold \
  --billing paymentTiming=PrePaid reservation.reservationLength=1 reservation.reservationTimeUnit=month

# List parameter (each --tags occurrence is one array element)
bce vpc CreateVpc --unfold \
  --tags tagKey=env tagValue=prod \
  --tags tagKey=team tagValue=infra
```

### Loading Parameters from a File

```bash
# Generate a parameter skeleton
bce vpc CreateVpc --generate-cli-skeleton > params.json

# Fill in the values and load the file
bce vpc CreateVpc --cli-input-json file://params.json

# Tilde paths are supported
bce vpc CreateVpc --cli-input-json file://~/.bce/CreateVpc.json
```

Command-line flags take precedence over file parameters, so you can override individual values:

```bash
bce vpc CreateVpc \
  --cli-input-json file://params.json \
  --name override-name
```

---

## Output Formats

BCE CLI supports three output formats: `json` (default), `table`, and `text`.

### JSON Output (default)

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

### Table Output

Use `--output table` with optional `cols=` and `rows=` sub-parameters:

```bash
# Specify the row source, auto-detect column names
bce vpc DescribeVpcs --output table rows=vpcs

# Specify both row source and column names
bce vpc DescribeVpcs --output table rows=vpcs cols=vpcId,name,cidr
```

Example output:

```
cidr              name    vpcId
192.168.0.0/16    myVpc   vpc-xxxxxxxx
```

`--output` sub-parameters:

| Sub-parameter | Description                                                                          |
| ------------- | ------------------------------------------------------------------------------------ |
| `rows=`     | JMESPath expression selecting the array to use as table rows; runs after `--query` |
| `cols=`     | Comma-separated column names matching JSON field names, e.g.`cols=vpcId,name,cidr` |

### JMESPath Filtering

Use `--query` to filter the response with a JMESPath expression. Works with all output formats:

```bash
# Output the first VPC's ID
bce vpc DescribeVpcs --query 'vpcs[0].vpcId'

# Output all VPC names as a list
bce vpc DescribeVpcs --query 'vpcs[].name'

# Extract and rename fields
bce vpc DescribeVpcs --query 'vpcs[].{ID:vpcId,Name:name}'
```

`--query` and `rows=` run as a two-stage pipeline: `--query` is applied to the raw response first, then `rows=` is applied to that result. They can be used independently or combined:

```bash
# --query transforms the response first, rows= then extracts the array from the result
bce vpc DescribeVpcs \
  --query '{matched: vpcs[?starts_with(cidr, `192.168`)]}' \
  --output table rows=matched cols=vpcId,name,cidr
```

### Text Output

Useful for scalar values, typically combined with `--query`:

```bash
bce vpc DescribeVpcs --query 'vpcs[0].vpcId' --output text
```

---

## Advanced Features

### Specifying a Region

Use `--region` to target a specific region, or set the `region` field in your profile as the default:

```bash
# Use the Guangzhou region
bce vpc DescribeVpcs --region gz
```

Supported regions: `bj` (Beijing), `gz` (Guangzhou), `su` (Suzhou), `bd` (Baoding), `fwh` (Wuhan), `nj` (Nanjing), `yq` (Yangquan), `cd` (Chengdu), `hkg` (Hong Kong), `global`.

### Dry Run

Print the request that would be sent without actually sending it:

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

### Debug Mode

Print the request parameters constructed by the CLI and the response details (does not include headers automatically added by the SDK, such as signature headers):

```bash
bce vpc DescribeVpcs --debug
```

```
[DEBUG] > GET https://bcc.bj.baidubce.com/v1/vpc
[DEBUG] < Status: 200
[DEBUG] < Body:
{ ... }
```

### Command Suggestions

When a command is mistyped, the CLI automatically suggests the closest matches:

```bash
bce vpc DescribeVpc  # typo
```

```
Did you mean:
  bce vpc DescribeVpcs
  bce vpc DescribeVpns
```

### Specifying the Request Scheme

Use `--scheme` to force requests to use `http` or `https`. When omitted, the CLI defaults to `https`:

```bash
# Force HTTP
bce vpc DescribeVpcs --scheme http

# Force HTTPS
bce vpc DescribeVpcs --scheme https
```

> Note: If the specified scheme is not supported by the target service, the command will exit with an error.

### Auto-pagination

For List-type APIs that declare pagination metadata, BCE CLI returns only the first page by default. Add `--pager` to enable automatic iteration through all pages and have the results merged into a single response:

```bash
# Default: return only the first page
bce vpc DescribeVpcs

# Enable auto-pagination and merge all pages
bce vpc DescribeVpcs --pager
```

APIs that include a `marker` request parameter support pagination. Run `<action> --help` and look for a `Pagination Flags` section to confirm.

Pagination flags:

| Flag                  | Description                                                                                                              |
| --------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| `--pager`           | Enable auto-pagination and aggregate all pages into a single output                                                     |
| `--total-count <N>` | Cap the total items returned (requires `--pager`); returns `nextMarker` in the output as a resume cursor when truncated |

The page cursor and page size are passed as native API parameters — no extra flags needed:

```bash
# Enable paging, 20 items per page (maxKeys is a native API parameter)
bce vpc DescribeVpcs --pager --maxKeys 20

# Start from a specific position (marker is a native API parameter)
bce vpc DescribeVpcs --pager --marker vpc-xxxxxxxx

# Return at most 50 items; nextMarker is returned in the output as a resume cursor
bce vpc DescribeVpcs --pager --total-count 50

# Pass the nextMarker value from the previous response as --marker to resume
bce vpc DescribeVpcs --pager --total-count 50 --marker <nextMarker from previous output>
```

> Using `--pager` or `--total-count` on an API that does not support pagination will result in an error. `--total-count` must be used together with `--pager`.

### Multi-language Support

BCE CLI supports Chinese and English:

```bash
# English output
bce vpc CreateVpc --help --language en-US

# Chinese output (default)
bce vpc CreateVpc --help --language zh-CN
```

You can also set the default language in your profile (`"language": "en-US"`) or via the `BCE_LANGUAGE` environment variable.

---

## Upgrade

BCE CLI has a built-in self-update command — no need to manually download a new release.

### Upgrade to the latest version

```bash
bce upgrade
```

This checks the latest release on GitHub Releases and prompts for confirmation if a newer version is available:

```
Checking for latest version...
New version available: 0.2.0 (current: 0.1.0)
Upgrade now? [y/N] y
Downloading 0.2.0...
Upgrade complete. Current version: 0.2.0
```

### Install a specific version (downgrade / pin a version)

```bash
bce upgrade --version 0.3.0
```

Skips the latest-version check and installs the specified version directly. Useful for rolling back to an older release:

```
About to install 0.3.0 (current: 0.4.0)
Upgrade now? [y/N] y
Downloading 0.3.0...
Upgrade complete. Current version: 0.3.0
```

### Skip confirmation (CI/CD)

```bash
bce upgrade --yes
bce upgrade --version 0.3.0 --yes
```

Upgrade flags:

| Flag              | Description                                                   |
| ----------------- | ------------------------------------------------------------- |
| `--version`     | Install a specific version, e.g. `0.3.0` (default: latest) |
| `--yes` / `-y` | Skip confirmation and upgrade immediately                     |

> If the upgrade fails with a permission error, run `sudo bce upgrade` on macOS/Linux, or run as Administrator on Windows.

---

## Shell Completion

BCE CLI supports Tab completion for Bash, Zsh, Fish, and PowerShell.

### Bash

```bash
bce completion bash > /etc/bash_completion.d/bce
# Or add to ~/.bashrc:
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
# Or append to an existing profile:
bce completion powershell >> $PROFILE
```

---

## Global Flags

The following flags apply to all actions:

| Flag                        | Type   | Description                                                                                 |
| --------------------------- | ------ | ------------------------------------------------------------------------------------------- |
| `--profile`               | string | Use a named profile (default: current profile)                                              |
| `--region`                | string | Override region, e.g.:`bj` / `gz` / `su`                                              |
| `--endpoint`              | string | Override the request domain (hostname)                                              |
| `--output`                | string | Output format:`json` / `table` / `text`; table supports `cols=fields` `rows=path` |
| `--query`                 | string | JMESPath expression to filter the response                                                  |
| `--language`              | string | Output language:`zh-CN` / `en-US`                                                       |
| `--cli-input-json`        | string | Load request parameters from a JSON file, e.g.:`file://path/to/params.json`               |
| `--generate-cli-skeleton` |        | Print a JSON parameter skeleton to stdout                                                   |
| `--unfold`                |        | Enable KV dot-notation for List/Object parameters                                           |
| `--dry-run`               |        | Print the request without sending it                                                        |
| `--debug`                 |        | Print request parameters and response details                                               |
| `--scheme`                | string | Force request scheme:`http` or `https`                                                  |
| `--no-color`              |        | Disable ANSI color output                                                                   |
| `--timeout`               | int    | HTTP request timeout in seconds (default 15)                                                |

---

## Environment Variables

If not set in the configuration file, BCE CLI reads credentials and settings from the following environment variables:

| Variable                  | Description                            |
| ------------------------- | -------------------------------------- |
| `BCE_ACCESS_KEY_ID`     | Access Key ID                          |
| `BCE_SECRET_ACCESS_KEY` | Secret Access Key                      |
| `BCE_SECURITY_TOKEN`    | STS temporary security token           |
| `BCE_REGION`            | Default region                         |
| `BCE_LANGUAGE`          | Display language:`zh-CN` / `en-US` |

Language priority: `--language` flag > profile setting > `BCE_LANGUAGE` env var > system locale (`$LANG`) > default `zh-CN`.
