[CmdletBinding()]
param(
    [string]$Address = '127.0.0.1:8080',
    [string]$DatabaseUrl = $env:MEMORYS_DATABASE_URL,
    [switch]$AskDeepSeekKey,
    [switch]$CheckOnly
)

$ErrorActionPreference = 'Stop'
$workspace = Split-Path -Parent $PSScriptRoot
$binary = Join-Path $workspace '.local/bin/memorys-local.exe'
$state = Join-Path $workspace '.local/runtime'
$ui = Join-Path $workspace 'dashboard/dist'

if ($Address -notmatch '^127\.0\.0\.1:([0-9]{1,5})$' -or [int]$Matches[1] -lt 1 -or [int]$Matches[1] -gt 65535) {
    throw 'Address must be 127.0.0.1:PORT.'
}
foreach ($required in @($binary, (Join-Path $state 'manifest.json'), (Join-Path $ui 'index.html'))) {
    if (-not (Test-Path -LiteralPath $required -PathType Leaf)) {
        throw 'Local executable, initialized runtime or built dashboard is missing. See docs/architecture/LOCAL_WEB.md.'
    }
}
if ([string]::IsNullOrWhiteSpace($DatabaseUrl)) {
    $DatabaseUrl = 'postgres://postgres@127.0.0.1:5432/memorys?sslmode=disable'
}
if ($CheckOnly) {
    Write-Output 'Local web launcher prerequisites: passed (no database or model connection attempted).'
    return
}

$savedDatabase = $env:MEMORYS_DATABASE_URL
$savedPassword = $env:PGPASSWORD
$savedChatKey = $env:DEEPSEEK_API_KEY
Push-Location -LiteralPath $workspace
try {
    $env:MEMORYS_DATABASE_URL = $DatabaseUrl
    # Use an existing process password or pgpass when provided; never persist it.
    $pgpass = $env:PGPASSFILE
    if ([string]::IsNullOrWhiteSpace($pgpass) -and $env:APPDATA) {
        $pgpass = Join-Path $env:APPDATA 'postgresql/pgpass.conf'
    }
    if ([string]::IsNullOrEmpty($env:PGPASSWORD) -and -not ($pgpass -and (Test-Path -LiteralPath $pgpass -PathType Leaf))) {
        $inputSecret = Read-Host 'PostgreSQL password (not saved)' -AsSecureString
        try { $env:PGPASSWORD = [System.Net.NetworkCredential]::new('', $inputSecret).Password }
        finally { $inputSecret.Dispose() }
    }
    if ($AskDeepSeekKey) {
        $inputSecret = Read-Host 'DeepSeek API key (not saved)' -AsSecureString
        try { $env:DEEPSEEK_API_KEY = [System.Net.NetworkCredential]::new('', $inputSecret).Password }
        finally { $inputSecret.Dispose() }
    }
    Write-Output "Open http://$Address/local/ ; press Ctrl+C here to stop."
    & $binary web --address $Address --state $state --ui-dir $ui
    if ($LASTEXITCODE -ne 0) { throw 'Local web exited with an error. Run memorys-local doctor for diagnostics.' }
} finally {
    $env:MEMORYS_DATABASE_URL = $savedDatabase
    $env:PGPASSWORD = $savedPassword
    $env:DEEPSEEK_API_KEY = $savedChatKey
    Pop-Location
}
