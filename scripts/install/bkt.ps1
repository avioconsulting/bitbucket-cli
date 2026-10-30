#Requires -Version 5.1
# Public bootstrap for AVIO bkt. No administrator privileges required.
[CmdletBinding()]
param(
    [ValidateSet('stable', 'snapshot')][string]$Channel = 'stable',
    [string]$BinDir,
    [Alias('h')][switch]$Help
)
if ($Help) {
    @'
Usage: .\bkt.ps1 [-Channel stable|snapshot] [-BinDir DIRECTORY] [-Help]

Stable is the default. The default directory is %LOCALAPPDATA%\AVIO\bin and is
added to your user PATH. With -BinDir, manage PATH yourself. Rerun to update.
Set JFROG_ACCESS_TOKEN or enter your token at the hidden prompt.
'@
    return
}
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
if ($env:OS -ne 'Windows_NT' -or
    ($env:PROCESSOR_ARCHITECTURE -ne 'AMD64' -and $env:PROCESSOR_ARCHITEW6432 -ne 'AMD64')) {
    throw 'This installer supports Windows x64 only.'
}
$customDir = $PSBoundParameters.ContainsKey('BinDir')
if (!$customDir) { $BinDir = Join-Path $env:LOCALAPPDATA 'AVIO\bin' }
if ([string]::IsNullOrWhiteSpace($BinDir)) { throw 'BinDir must not be empty.' }
$BinDir = [IO.Path]::GetFullPath($BinDir)
$repository = 'https://avio.jfrog.io/artifactory/tools/bkt'
$token = $env:JFROG_ACCESS_TOKEN
if (!$token) {
    $secure = Read-Host 'JFrog access token' -AsSecureString
    $token = [Net.NetworkCredential]::new('', $secure).Password
}
if ($token -notmatch '^[A-Za-z0-9._~+/=-]+$') { throw 'Invalid bearer token characters.' }
Add-Type -AssemblyName System.Net.Http
Add-Type -AssemblyName System.IO.Compression.FileSystem
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$handler = [Net.Http.HttpClientHandler]::new()
$handler.AllowAutoRedirect = $false
$client = [Net.Http.HttpClient]::new($handler)
$client.Timeout = [TimeSpan]::FromMinutes(5)
$work = Join-Path ([IO.Path]::GetTempPath()) ('bkt-install-' + [guid]::NewGuid().ToString('N'))
[IO.Directory]::CreateDirectory($work) | Out-Null
$lock = $null
$staged = $null
function Download([uri]$Uri, [string]$Destination) {
    $sendToken = $true
    for ($redirect = 0; $redirect -le 5; $redirect++) {
        if ($Uri.Scheme -ne 'https' -or $Uri.UserInfo) { throw 'Unsafe download URL.' }
        $request = [Net.Http.HttpRequestMessage]::new([Net.Http.HttpMethod]::Get, $Uri)
        if ($sendToken) {
            $request.Headers.Authorization = [Net.Http.Headers.AuthenticationHeaderValue]::new('Bearer', $token)
        }
        $response = $null
        try {
            $response = $client.SendAsync($request).GetAwaiter().GetResult()
            $status = [int]$response.StatusCode
            if ($status -in @(301, 302, 303, 307, 308)) {
                if (!$response.Headers.Location) { throw 'Download redirect has no location.' }
                $next = [uri]::new($Uri, $response.Headers.Location)
                if ($next.Authority -ne $Uri.Authority) { $sendToken = $false }
                $Uri = $next
                continue
            }
            if ($status -ne 200) { throw "Download failed (HTTP $status)." }
            [IO.File]::WriteAllBytes($Destination, $response.Content.ReadAsByteArrayAsync().GetAwaiter().GetResult())
            return
        } finally {
            if ($response) { $response.Dispose() }
            $request.Dispose()
        }
    }
    throw 'Too many download redirects.'
}
function Properties([string]$Path) {
    $values = @{}
    foreach ($line in [IO.File]::ReadAllLines($Path)) {
        if ($line -notmatch '^([A-Za-z0-9_.-]+)=([A-Za-z0-9_./-]+)$' -or $values.ContainsKey($Matches[1])) {
            throw 'Invalid or duplicate manifest entry.'
        }
        $values[$Matches[1]] = $Matches[2]
    }
    return $values
}
try {
    Write-Host "Resolving the bkt $Channel channel..."
    Download "$repository/channels/$Channel.properties" "$work\channel.properties"
    $manifest = Properties "$work\channel.properties"
    $version = $manifest['version']
    $suffix = if ($Channel -eq 'snapshot') { '-SNAPSHOT' } else { '' }
    if ($version -notmatch ('^\d+\.\d+\.\d+-avio' + $suffix + '$')) { throw 'Invalid channel version.' }
    $path = $manifest['path']
    if ($path -notmatch ('^builds/' + [regex]::Escape($version) + '/jenkins-\d+-([a-f0-9]{40})$')) {
        throw 'Invalid channel path.'
    }
    $commit = $Matches[1]
    $name = "bkt-native-windows-x64-$version.zip"
    $expected = $manifest["$name.sha256"]
    if ($expected -notmatch '^[a-f0-9]{64}$') { throw 'Windows x64 is unavailable in this channel or its checksum is invalid.' }
    Write-Host "Downloading bkt $version (Windows x64)..."
    Download "$repository/$path/$name" "$work\archive.zip"
    $token = $null
    if ((Get-FileHash "$work\archive.zip" -Algorithm SHA256).Hash -ine $expected) { throw 'Archive checksum mismatch.' }
    $root = "bkt-$version"
    $archive = [IO.Compression.ZipFile]::OpenRead("$work\archive.zip")
    $names = [Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
    try {
        if (!$archive.Entries.Count) { throw 'Empty archive.' }
        foreach ($entry in $archive.Entries) {
            $entryName = $entry.FullName
            $type = ($entry.ExternalAttributes -shr 16) -band 0xf000
            if ($entryName -notmatch '^[A-Za-z0-9_./-]+$' -or
                !$entryName.StartsWith("$root/", [StringComparison]::Ordinal) -or
                $entryName -match '(^|/)\.{1,2}(/|$)' -or !$names.Add($entryName) -or
                $type -notin @(0, 0x4000, 0x8000)) { throw 'Unsafe archive entry.' }
        }
    } finally { $archive.Dispose() }
    [IO.Compression.ZipFile]::ExtractToDirectory("$work\archive.zip", "$work\payload")
    $payload = "$work\payload\$root"
    $metadata = Properties "$payload\package.properties"
    if ($metadata['version'] -ne $version -or $metadata['platform'] -ne 'windows-x64' -or
        $metadata['commit'] -ne $commit -or !(Test-Path "$payload\bin\bkt.exe" -PathType Leaf)) {
        throw 'Downloaded package metadata mismatch.'
    }
    [IO.Directory]::CreateDirectory($BinDir) | Out-Null
    $lock = [IO.File]::Open("$BinDir\.bkt-install.lock", [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
    $staged = Join-Path $BinDir ('.bkt-' + [guid]::NewGuid().ToString('N') + '.exe')
    [IO.File]::Copy("$payload\bin\bkt.exe", $staged)
    $target = Join-Path $BinDir 'bkt.exe'
    if ([IO.File]::Exists($target)) {
        # Windows PowerShell 5.1 converts a null string argument to an empty path.
        $backup = "$staged.previous"
        [IO.File]::Replace($staged, $target, $backup)
        Remove-Item $backup -Force
    }
    else { [IO.File]::Move($staged, $target) }
    $staged = $null
    if (!$customDir) {
        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        if (($userPath -split ';') -notcontains $BinDir) {
            [Environment]::SetEnvironmentVariable('Path', ($BinDir + ';' + $userPath).TrimEnd(';'), 'User')
        }
        if (($env:Path -split ';') -notcontains $BinDir) { $env:Path = "$BinDir;$env:Path" }
    }
    Write-Host "Installed bkt $version to $target"
    Write-Host 'Open a new terminal, then run bkt --help and bkt auth login.'
} finally {
    $token = $null
    $client.Dispose()
    if ($lock) { $lock.Dispose(); Remove-Item "$BinDir\.bkt-install.lock" -Force }
    if ($staged -and (Test-Path $staged)) { Remove-Item $staged -Force }
    Remove-Item $work -Recurse -Force
}
