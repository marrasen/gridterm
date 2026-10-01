# Installs the newest kakel for you alone, with no administrator:
#
#   irm https://raw.githubusercontent.com/marrasen/kakel/main/install.ps1 | iex
#
# It fetches the newest release, checks it against SHA256SUMS, and has
# kakel install itself into %LOCALAPPDATA%\Programs\kakel, with a Start
# menu shortcut and an entry under Installed apps.
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$release = Invoke-RestMethod 'https://api.github.com/repos/marrasen/kakel/releases/latest'
$name = "kakel_$($release.tag_name)_windows_amd64.zip"
$zip = $release.assets | Where-Object name -eq $name
$sums = $release.assets | Where-Object name -eq 'SHA256SUMS'
if (-not $zip -or -not $sums) { throw "Release $($release.tag_name) has no $name or SHA256SUMS." }

$tmp = Join-Path ([IO.Path]::GetTempPath()) ("kakel-" + [guid]::NewGuid())
New-Item -ItemType Directory $tmp | Out-Null
try {
    $file = Join-Path $tmp $name
    Write-Host "Fetching kakel $($release.tag_name)..."
    Invoke-WebRequest $zip.browser_download_url -OutFile $file -UseBasicParsing
    $sumsFile = Join-Path $tmp 'SHA256SUMS'
    Invoke-WebRequest $sums.browser_download_url -OutFile $sumsFile -UseBasicParsing
    $want = (Get-Content $sumsFile |
        Where-Object { $_ -match "\s\*?$([regex]::Escape($name))\s*$" } |
        ForEach-Object { ($_ -split '\s+')[0] }) | Select-Object -First 1
    $have = (Get-FileHash $file -Algorithm SHA256).Hash
    if (-not $want -or $have -ne $want.ToUpper()) { throw "$name does not match SHA256SUMS." }
    Expand-Archive $file -DestinationPath $tmp
    & (Join-Path $tmp 'kakel.exe') -install | Out-Host
    if ($LASTEXITCODE) { throw "kakel -install failed." }
} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
