# 茶与月色 —— Windows 构建脚本（对应 Makefile build）
# 依赖：Go 1.25+、Node 20+
$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $root

Write-Host '[1/3] 安装前端依赖' -ForegroundColor Cyan
npm --prefix web install --no-audit --no-fund
if ($LASTEXITCODE -ne 0) { throw 'npm install 失败' }

Write-Host '[2/3] 构建前端并拷贝到 embed 目录' -ForegroundColor Cyan
npm --prefix web run build
if ($LASTEXITCODE -ne 0) { throw '前端构建失败' }
node scripts/copy-dist.mjs
if ($LASTEXITCODE -ne 0) { throw 'copy-dist 失败' }

Write-Host '[3/3] 编译 Go 单二进制' -ForegroundColor Cyan
New-Item -ItemType Directory -Force bin | Out-Null
$version = 'dev'
try { $version = (git describe --tags --always --dirty 2>$null) } catch { }
if (-not $version) { $version = 'dev' }
go build -trimpath -ldflags "-s -w -X main.version=$version" -o bin/musicd.exe ./server/cmd/musicd
if ($LASTEXITCODE -ne 0) { throw 'go build 失败' }

Write-Host "完成：bin/musicd.exe（$version）。运行 .\bin\musicd.exe 后打开 http://127.0.0.1:8080" -ForegroundColor Green
