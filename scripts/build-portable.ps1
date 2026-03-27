[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Version,

    [string]$OutputPath,

    [string]$FixedRuntimeSourcePath
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$desktopRoot = Join-Path $repoRoot 'desktop'
$webuiRoot = Join-Path $repoRoot 'webui'
$distRoot = Join-Path $repoRoot 'dist'
$buildRoot = Join-Path $distRoot '.portable-build'
$stageRoot = Join-Path $buildRoot 'stage'
$desktopBuildBinRoot = Join-Path $desktopRoot 'build\bin'
$portableName = 'resinat-windows-amd64-portable.zip'
$portableZipPath = if ([string]::IsNullOrWhiteSpace($OutputPath)) {
    Join-Path $distRoot $portableName
} else {
    $resolvedOutput = [System.IO.Path]::GetFullPath($OutputPath)
    if ([System.IO.Path]::GetExtension($resolvedOutput) -eq '') {
        Join-Path $resolvedOutput $portableName
    } else {
        $resolvedOutput
    }
}

$coreOutputPath = Join-Path $buildRoot 'resin-core.exe'
$desktopExeName = 'resinat-desktop.exe'
$desktopExeOutputPath = Join-Path $desktopBuildBinRoot $desktopExeName
$desktopShimPath = Join-Path $desktopRoot 'main.go'
$desktopShimTemplatePath = Join-Path $PSScriptRoot 'build-portable-main.go.tmpl'

function Write-Step {
    param([string]$Message)
    Write-Host "==> $Message"
}

function Invoke-NativeCommand {
    param(
        [Parameter(Mandatory = $true)]
        [string]$FilePath,

        [string[]]$ArgumentList = @(),

        [string]$WorkingDirectory = $repoRoot,

        [hashtable]$Environment = @{}
    )

    $previous = @{}
    foreach ($entry in $Environment.GetEnumerator()) {
        $previous[$entry.Key] = [Environment]::GetEnvironmentVariable($entry.Key, 'Process')
        [Environment]::SetEnvironmentVariable($entry.Key, [string]$entry.Value, 'Process')
    }

    Push-Location $WorkingDirectory
    try {
        & $FilePath @ArgumentList
        if ($LASTEXITCODE -ne 0) {
            throw "Command failed: $FilePath $($ArgumentList -join ' ')"
        }
    }
    finally {
        Pop-Location
        foreach ($entry in $Environment.GetEnumerator()) {
            [Environment]::SetEnvironmentVariable($entry.Key, $previous[$entry.Key], 'Process')
        }
    }
}

function Get-GitCommit {
    $previousPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $commit = (& git -C $repoRoot rev-parse --short=8 HEAD 2>$null)
        if ($LASTEXITCODE -eq 0 -and -not [string]::IsNullOrWhiteSpace($commit)) {
            return $commit.Trim()
        }
        return 'unknown'
    }
    finally {
        $ErrorActionPreference = $previousPreference
    }
}

function Get-BuildTime {
    return [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')
}

function New-DesktopShim {
    if (Test-Path $desktopShimPath) {
        throw "Refusing to overwrite existing desktop entrypoint: $desktopShimPath"
    }

    if (-not (Test-Path $desktopShimTemplatePath)) {
        throw "Missing desktop shim template: $desktopShimTemplatePath"
    }

    $source = [System.IO.File]::ReadAllText($desktopShimTemplatePath, [System.Text.UTF8Encoding]::new($false))
    [System.IO.File]::WriteAllText($desktopShimPath, $source, [System.Text.UTF8Encoding]::new($false))
}

function Remove-DesktopShim {
    if (Test-Path $desktopShimPath) {
        Remove-Item -LiteralPath $desktopShimPath -Force
    }
}

function Resolve-FixedRuntimeSourcePath {
    param([string]$ExplicitPath)

    if (-not [string]::IsNullOrWhiteSpace($ExplicitPath)) {
        $resolvedExplicit = [System.IO.Path]::GetFullPath($ExplicitPath)
        if (-not (Test-Path $resolvedExplicit)) {
            throw "Specified WebView2 runtime directory does not exist: $resolvedExplicit"
        }
        $candidateExe = Join-Path $resolvedExplicit 'msedgewebview2.exe'
        if (-not (Test-Path $candidateExe)) {
            throw "Specified directory is not a fixed runtime root, missing msedgewebview2.exe: $resolvedExplicit"
        }
        return $resolvedExplicit
    }

    $candidateRoots = @(
        'C:\Program Files (x86)\Microsoft\EdgeWebView\Application',
        'C:\Program Files\Microsoft\EdgeWebView\Application'
    )

    foreach ($candidateRoot in $candidateRoots) {
        if (-not (Test-Path $candidateRoot)) {
            continue
        }

        $versions = Get-ChildItem -LiteralPath $candidateRoot -Directory |
            Where-Object { $_.Name -match '^\d+(\.\d+){3}$' } |
            Sort-Object { [Version]$_.Name } -Descending

        foreach ($version in $versions) {
            $candidateExe = Join-Path $version.FullName 'msedgewebview2.exe'
            if (Test-Path $candidateExe) {
                return $version.FullName
            }
        }
    }

    return Download-FixedRuntimeFromOfficialSource
}

function Download-FixedRuntimeFromOfficialSource {
    $downloadRoot = Join-Path $buildRoot 'webview2-download'
    $expandedRoot = Join-Path $downloadRoot 'expanded'
    $cabPath = Join-Path $downloadRoot 'Microsoft.WebView2.FixedVersionRuntime.x64.cab'

    if (Test-Path $downloadRoot) {
        Remove-Item -LiteralPath $downloadRoot -Recurse -Force
    }
    New-Item -ItemType Directory -Path $expandedRoot -Force | Out-Null

    Write-Step 'Resolve fixed runtime CAB URL'
    $page = Invoke-WebRequest -Uri 'https://developer.microsoft.com/en-us/microsoft-edge/webview2' -UseBasicParsing
    $regex = [regex]'https://msedge\.sf\.dl\.delivery\.mp\.microsoft\.com/filestreamingservice/files/[^\"]+/Microsoft\.WebView2\.FixedVersionRuntime\.[^\"]+\.x64\.cab'
    $match = $regex.Match($page.Content)
    if (-not $match.Success) {
        throw 'Unable to resolve an x64 fixed runtime CAB link from the official WebView2 download page.'
    }

    Write-Step 'Download fixed runtime CAB'
    Invoke-WebRequest -Uri $match.Value -OutFile $cabPath -UseBasicParsing

    Write-Step 'Expand fixed runtime CAB'
    Invoke-NativeCommand -FilePath 'expand.exe' -ArgumentList @(
        $cabPath,
        '-F:*',
        $expandedRoot
    )

    $runtimeExe = Get-ChildItem -LiteralPath $expandedRoot -Recurse -File -Filter 'msedgewebview2.exe' | Select-Object -First 1
    if ($null -eq $runtimeExe) {
        throw 'msedgewebview2.exe was not found after expanding the official fixed runtime CAB.'
    }

    return $runtimeExe.Directory.FullName
}

function Copy-FixedRuntime {
    param(
        [Parameter(Mandatory = $true)]
        [string]$SourceRoot,

        [Parameter(Mandatory = $true)]
        [string]$TargetRoot
    )

    if (Test-Path $TargetRoot) {
        Remove-Item -LiteralPath $TargetRoot -Force -Recurse
    }

    $parent = Split-Path -Parent $TargetRoot
    if (-not (Test-Path $parent)) {
        New-Item -ItemType Directory -Path $parent | Out-Null
    }

    Copy-Item -LiteralPath $SourceRoot -Destination $TargetRoot -Recurse -Force

    $runtimeExe = Join-Path $TargetRoot 'msedgewebview2.exe'
    if (-not (Test-Path $runtimeExe)) {
        throw "Fixed runtime copy is missing msedgewebview2.exe: $runtimeExe"
    }
}

function Copy-LicenseFiles {
    param(
        [Parameter(Mandatory = $true)]
        [string]$DestinationRoot
    )

    $licenseFiles = @(Get-ChildItem -LiteralPath $repoRoot -File | Where-Object { $_.Name -like 'LICENSE*' })
    if ($licenseFiles.Count -eq 0) {
        throw 'No LICENSE* files were found in the repository root.'
    }

    foreach ($licenseFile in $licenseFiles) {
        Copy-Item -LiteralPath $licenseFile.FullName -Destination (Join-Path $DestinationRoot $licenseFile.Name) -Force
    }
}

try {
    New-Item -ItemType Directory -Path $distRoot -Force | Out-Null
    New-Item -ItemType Directory -Path $buildRoot -Force | Out-Null

    if (Test-Path $stageRoot) {
        Remove-Item -LiteralPath $stageRoot -Force -Recurse
    }
    New-Item -ItemType Directory -Path $stageRoot | Out-Null

    if (Test-Path $coreOutputPath) {
        Remove-Item -LiteralPath $coreOutputPath -Force
    }
    if (Test-Path $desktopExeOutputPath) {
        Remove-Item -LiteralPath $desktopExeOutputPath -Force
    }
    if (Test-Path $portableZipPath) {
        Remove-Item -LiteralPath $portableZipPath -Force
    }

    $gitCommit = Get-GitCommit
    $buildTime = Get-BuildTime
    $ldflags = "-s -w -X github.com/Resinat/Resin/internal/buildinfo.Version=$Version -X github.com/Resinat/Resin/internal/buildinfo.GitCommit=$gitCommit -X github.com/Resinat/Resin/internal/buildinfo.BuildTime=$buildTime"

    Write-Step 'Build webui'
    Invoke-NativeCommand -FilePath 'npm.cmd' -ArgumentList @('ci') -WorkingDirectory $webuiRoot
    Invoke-NativeCommand -FilePath 'npm.cmd' -ArgumentList @('run', 'build') -WorkingDirectory $webuiRoot

    Write-Step 'Build resin-core.exe'
    Invoke-NativeCommand -FilePath 'go.exe' -ArgumentList @(
        'build',
        '-trimpath',
        '-tags',
        'with_quic with_wireguard with_grpc with_utls with_embedded_tor with_naive_outbound',
        '-ldflags',
        $ldflags,
        '-o',
        $coreOutputPath,
        './cmd/resin'
    ) -WorkingDirectory $repoRoot -Environment @{
        GOOS = 'windows'
        GOARCH = 'amd64'
        CGO_ENABLED = '0'
    }

    Write-Step 'Build Wails shell'
    New-DesktopShim
    try {
        Invoke-NativeCommand -FilePath 'go.exe' -ArgumentList @(
            'run',
            'github.com/wailsapp/wails/v2/cmd/wails@v2.12.0',
            'build',
            '-clean',
            '-s',
            '-skipbindings',
            '-nopackage',
            '-platform',
            'windows/amd64',
            '-trimpath',
            '-m',
            '-nosyncgomod',
            '-o',
            $desktopExeName
        ) -WorkingDirectory $desktopRoot
    }
    finally {
        Remove-DesktopShim
    }

    if (-not (Test-Path $desktopExeOutputPath)) {
        throw "Wails shell output was not found: $desktopExeOutputPath"
    }

    Write-Step 'Prepare fixed WebView2 runtime'
    $fixedRuntimeSource = Resolve-FixedRuntimeSourcePath -ExplicitPath $FixedRuntimeSourcePath

    Write-Step 'Assemble portable layout'
    $binRoot = Join-Path $stageRoot 'bin'
    $runtimeRoot = Join-Path $stageRoot 'runtime'
    $fixedRuntimeTarget = Join-Path $runtimeRoot 'webview2-fixed'
    New-Item -ItemType Directory -Path $binRoot -Force | Out-Null
    New-Item -ItemType Directory -Path $runtimeRoot -Force | Out-Null

    Copy-Item -LiteralPath $desktopExeOutputPath -Destination (Join-Path $stageRoot $desktopExeName) -Force
    Copy-Item -LiteralPath $coreOutputPath -Destination (Join-Path $binRoot 'resin-core.exe') -Force
    Copy-Item -LiteralPath (Join-Path $repoRoot 'README.md') -Destination (Join-Path $stageRoot 'README.md') -Force
    Copy-LicenseFiles -DestinationRoot $stageRoot
    Copy-FixedRuntime -SourceRoot $fixedRuntimeSource -TargetRoot $fixedRuntimeTarget

    Write-Step 'Create portable ZIP'
    New-Item -ItemType Directory -Path (Split-Path -Parent $portableZipPath) -Force | Out-Null
    Compress-Archive -Path (Join-Path $stageRoot '*') -DestinationPath $portableZipPath -CompressionLevel Optimal

    Write-Host "PORTABLE_ZIP=$portableZipPath"
}
finally {
    Remove-DesktopShim
}
