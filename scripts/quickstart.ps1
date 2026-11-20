<#
.SYNOPSIS
  V3 本地快速启动脚本（加载同目录 .env，可选幂等导入演示语料，支持 --setup-ui 引导态）。
.EXAMPLE
  .\scripts\quickstart.ps1
  .\scripts\quickstart.ps1 -Seed
  .\scripts\quickstart.ps1 -SetupUI
#>
param(
  [switch]$Seed,
  [switch]$SetupUI
)

$ErrorActionPreference = 'Stop'
$RootDir = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
Set-Location $RootDir

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
  Write-Error "未检测到 Go 运行环境，请先安装 Go 1.26+。"
}

$EnvFile = Join-Path $RootDir '.env'
if (Test-Path $EnvFile) {
  Write-Host "[quickstart] 正在加载 .env 环境变量..."
  Get-Content -Encoding UTF8 $EnvFile | ForEach-Object {
    $line = $_.Trim()
    if (-not $line -or $line.StartsWith('#')) { return }
    $idx = $line.IndexOf('=')
    if ($idx -le 0) { return }
    $k = $line.Substring(0, $idx).Trim()
    $v = $line.Substring($idx + 1).Trim()
    if (($v.StartsWith('"') -and $v.EndsWith('"')) -or ($v.StartsWith("'") -and $v.EndsWith("'"))) {
      $v = $v.Substring(1, $v.Length - 2)
    }
    [Environment]::SetEnvironmentVariable($k, $v, 'Process')
  }
}

if (-not $env:MYSQL_DSN) {
  Write-Error "未检测到 MYSQL_DSN。请先复制 .env.example 为 .env 并填写 MYSQL_DSN，或先在终端设置环境变量。"
}

$requiredModels = @('LLM_BASE_URL', 'LLM_API_KEY', 'LLM_CHAT_MODEL', 'LLM_EMBED_MODEL')
$hasAllModels = $true
foreach ($name in $requiredModels) {
  $val = [Environment]::GetEnvironmentVariable($name, 'Process')
  if ([string]::IsNullOrWhiteSpace($val)) {
    $hasAllModels = $false
  }
}

if ($Seed) {
  if (-not $hasAllModels) {
    Write-Error "执行 -Seed 需要先提供完整模型变量：LLM_BASE_URL, LLM_API_KEY, LLM_CHAT_MODEL, LLM_EMBED_MODEL。"
  }
  Write-Host "[quickstart] 正在幂等导入多项目多格式演示语料与示例数据..."
  & go run ./scripts/seed_demo_v2.go
  if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
  }
}

$listenAddr = if ($env:HTTP_ADDR) { $env:HTTP_ADDR } else { ':8080' }
if ($SetupUI -or (-not $hasAllModels) -or ($env:ALLOW_WEB_SETUP -eq 'true')) {
  Write-Host "[quickstart] 启动服务（--setup-ui 引导配置态，密钥仅存内存，监听 $listenAddr）..."
  & go run . --setup-ui
  exit $LASTEXITCODE
}

Write-Host "[quickstart] 启动服务（监听 $listenAddr）..."
& go run .
exit $LASTEXITCODE
