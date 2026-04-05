[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('zip-structure', 'first-launch', 'explicit-exit', 'second-instance', 'port-collision', 'invalid-config')]
    [string]$Scenario,

    [Parameter(Mandatory = $true)]
    [string]$ZipPath,

    [ValidateSet('Auto', 'Full', 'Lite')]
    [string]$PackageVariant = 'Auto',

    [string]$SummaryPath,

    [switch]$KeepArtifacts
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$resolvedZipPath = [System.IO.Path]::GetFullPath($ZipPath)
$smokeRoot = Join-Path ([System.IO.Path]::GetTempPath()) 'resinat-portable-smoke'
$scenarioRoot = Join-Path $smokeRoot ($Scenario + '-' + [DateTime]::UtcNow.ToString('yyyyMMddHHmmss') + '-' + [guid]::NewGuid().ToString('N'))
$extractRoot = Join-Path $scenarioRoot 'portable'

Add-Type -AssemblyName System.IO.Compression
Add-Type -AssemblyName System.IO.Compression.FileSystem
Add-Type @"
using System;
using System.Runtime.InteropServices;

public static class ResinNativeWindow {
    [DllImport("user32.dll")]
    [return: MarshalAs(UnmanagedType.Bool)]
    public static extern bool IsWindowVisible(IntPtr hWnd);
}
"@

function Invoke-NativeCommand {
    param(
        [Parameter(Mandatory = $true)]
        [string]$FilePath,

        [string[]]$ArgumentList = @(),

        [string]$WorkingDirectory = $repoRoot
    )

    Push-Location $WorkingDirectory
    try {
        & $FilePath @ArgumentList
        if ($LASTEXITCODE -ne 0) {
            throw "Command failed: $FilePath $($ArgumentList -join ' ')"
        }
    }
    finally {
        Pop-Location
    }
}

function Write-Token {
    param([Parameter(Mandatory = $true)][string]$Token)

    Write-Host $Token
}

function Resolve-PackageVariant {
    param(
        [Parameter(Mandatory = $true)][string]$ArchivePath,
        [Parameter(Mandatory = $true)][string]$Variant
    )

    if ($Variant -ne 'Auto') {
        return $Variant
    }

    $archiveName = [System.IO.Path]::GetFileName($ArchivePath)
    if ($archiveName -like '*-lite.zip') {
        return 'Lite'
    }

    return 'Full'
}

function Assert-PortableZipStructure {
    param(
        [Parameter(Mandatory = $true)][string]$ArchivePath,
        [Parameter(Mandatory = $true)][string]$PackageVariant
    )

    if (-not (Test-Path $ArchivePath)) {
        throw "ZIP not found: $ArchivePath"
    }

    $zip = [System.IO.Compression.ZipFile]::OpenRead($ArchivePath)
    try {
        $entries = @($zip.Entries | ForEach-Object { $_.FullName.Replace('\', '/') })

        $requiredEntries = @(
            'resinat-desktop.exe',
            'README.md',
            'bin/resin-core.exe'
        )

        foreach ($requiredEntry in $requiredEntries) {
            if ($entries -notcontains $requiredEntry) {
                throw "ZIP is missing required entry: $requiredEntry"
            }
        }

        $fixedRuntimeEntries = @($entries | Where-Object { $_ -eq 'runtime/webview2-fixed/' -or $_ -like 'runtime/webview2-fixed/*' })
        $fixedRuntimeExecutableEntry = 'runtime/webview2-fixed/msedgewebview2.exe'
        switch ($PackageVariant) {
            'Full' {
                if ($entries -notcontains $fixedRuntimeExecutableEntry) {
                    throw "Full ZIP is missing required entry: $fixedRuntimeExecutableEntry"
                }
            }
            'Lite' {
                if ($fixedRuntimeEntries.Count -gt 0) {
                    throw "Lite ZIP must not bundle fixed runtime entries: $($fixedRuntimeEntries -join ', ')"
                }
            }
        }

        $licenseEntries = @($entries | Where-Object { $_ -like 'LICENSE*' })
        if ($licenseEntries.Count -eq 0) {
            throw 'ZIP root is missing LICENSE* files.'
        }

        $dataEntries = @($entries | Where-Object { $_ -like 'data/*' -or $_ -eq 'data/' })
        if ($dataEntries.Count -gt 0) {
            throw "ZIP must not pre-seed the data/ directory: $($dataEntries -join ', ')"
        }
    }
    finally {
        $zip.Dispose()
    }
}

function Expand-PortableZip {
    param(
        [Parameter(Mandatory = $true)][string]$ArchivePath,
        [Parameter(Mandatory = $true)][string]$DestinationRoot
    )

    if (Test-Path $DestinationRoot) {
        Remove-Item -LiteralPath $DestinationRoot -Recurse -Force
    }

    New-Item -ItemType Directory -Path $DestinationRoot -Force | Out-Null
    [System.IO.Compression.ZipFile]::ExtractToDirectory($ArchivePath, $DestinationRoot)
}

function Remove-ScenarioArtifacts {
    param(
        [Parameter(Mandatory = $true)][string]$ScenarioRoot,
        [Parameter(Mandatory = $true)][string]$SmokeRoot,
        [Parameter(Mandatory = $true)][bool]$KeepArtifacts
    )

    if ($KeepArtifacts) {
        return
    }

    if (Test-Path -LiteralPath $ScenarioRoot) {
        try {
            Remove-Item -LiteralPath $ScenarioRoot -Recurse -Force -ErrorAction Stop
        }
        catch {
            Write-Warning "Failed to remove smoke temp root ${ScenarioRoot}: $($_.Exception.Message)"
        }
    }

    if (Test-Path -LiteralPath $SmokeRoot) {
        $remainingEntries = @(Get-ChildItem -LiteralPath $SmokeRoot -Force -ErrorAction SilentlyContinue)
        if ($remainingEntries.Count -eq 0) {
            try {
                Remove-Item -LiteralPath $SmokeRoot -Force -ErrorAction Stop
            }
            catch {
                Write-Warning "Failed to remove empty smoke root ${SmokeRoot}: $($_.Exception.Message)"
            }
        }
    }
}

function Write-ScenarioSummary {
    param(
        [Parameter(Mandatory = $true)][string]$DestinationPath,
        [Parameter(Mandatory = $true)][string]$Scenario,
        [Parameter(Mandatory = $true)][string]$PackageVariant,
        [Parameter(Mandatory = $true)][string]$ZipPath,
        [Parameter(Mandatory = $true)][bool]$KeepArtifacts
    )

    if ([string]::IsNullOrWhiteSpace($DestinationPath)) {
        return
    }

    $resolvedDestination = [System.IO.Path]::GetFullPath($DestinationPath)
    $parent = Split-Path -Parent $resolvedDestination
    if (-not [string]::IsNullOrWhiteSpace($parent)) {
        New-Item -ItemType Directory -Path $parent -Force | Out-Null
    }

    $summary = [ordered]@{
        scenario = $Scenario
        packageVariant = $PackageVariant
        zipPath = $ZipPath
        keepArtifacts = $KeepArtifacts
        status = 'ok'
    }

    $json = $summary | ConvertTo-Json -Depth 4
    [System.IO.File]::WriteAllText($resolvedDestination, $json, [System.Text.UTF8Encoding]::new($false))
}

function Get-PortablePaths {
    param([Parameter(Mandatory = $true)][string]$Root)

    return [pscustomobject]@{
        RootDir            = $Root
        DesktopExe         = Join-Path $Root 'resinat-desktop.exe'
        CoreExe            = Join-Path $Root 'bin\resin-core.exe'
        RuntimeExe         = Join-Path $Root 'runtime\webview2-fixed\msedgewebview2.exe'
        DataDir            = Join-Path $Root 'data'
        StateDir           = Join-Path $Root 'data\state'
        CacheDir           = Join-Path $Root 'data\cache'
        LogDir             = Join-Path $Root 'data\logs'
        DesktopDataDir     = Join-Path $Root 'data\desktop'
        ShellConfigPath    = Join-Path $Root 'data\desktop\shell-config.json'
        SecretsPath        = Join-Path $Root 'data\desktop\secrets.dpapi'
    }
}

function Get-FreeTcpPort {
    $listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Parse('127.0.0.1'), 0)
    $listener.Start()
    try {
        return ([int]($listener.LocalEndpoint.Port)).ToString()
    }
    finally {
        $listener.Stop()
    }
}

function Wait-Until {
    param(
        [Parameter(Mandatory = $true)][scriptblock]$Condition,
        [string]$Description = 'condition',
        [int]$TimeoutSeconds = 20,
        [int]$PollMilliseconds = 200
    )

    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        if (& $Condition) {
            return
        }
        Start-Sleep -Milliseconds $PollMilliseconds
    }

    throw "Timed out waiting for $Description"
}

function Start-PortableProcess {
    param(
        [Parameter(Mandatory = $true)][string]$ExecutablePath,
        [string[]]$ArgumentList = @(),
        [string]$WorkingDirectory = (Split-Path -Parent $ExecutablePath),
        [hashtable]$Environment = @{}
    )

    $startInfo = New-Object System.Diagnostics.ProcessStartInfo
    $startInfo.FileName = $ExecutablePath
    $startInfo.WorkingDirectory = $WorkingDirectory
    $startInfo.UseShellExecute = $false

    foreach ($argument in $ArgumentList) {
        [void]$startInfo.ArgumentList.Add($argument)
    }

    foreach ($entry in $Environment.GetEnumerator()) {
        $startInfo.EnvironmentVariables[$entry.Key] = [string]$entry.Value
    }

    $process = [System.Diagnostics.Process]::Start($startInfo)
    if ($null -eq $process) {
        throw "Failed to start process: $ExecutablePath"
    }
    return $process
}

function Stop-PortableProcess {
    param([System.Diagnostics.Process]$Process)

    if ($null -eq $Process) {
        return
    }

    try {
        if (-not $Process.HasExited) {
            Stop-Process -Id $Process.Id -Force -ErrorAction SilentlyContinue
            $Process.WaitForExit(5000) | Out-Null
        }
    }
    catch {
    }
    finally {
        $Process.Dispose()
    }
}

function Get-ProcessesByExecutablePath {
    param([Parameter(Mandatory = $true)][string]$ExecutablePath)

    $normalizedPath = [System.IO.Path]::GetFullPath($ExecutablePath)
    $name = [System.IO.Path]::GetFileName($normalizedPath)
    $candidates = @(Get-CimInstance Win32_Process -Filter "Name='$name'" -ErrorAction SilentlyContinue)
    return @($candidates | Where-Object { $_.ExecutablePath -and ([System.IO.Path]::GetFullPath($_.ExecutablePath) -eq $normalizedPath) })
}

function Wait-ForMainWindow {
    param([Parameter(Mandatory = $true)][System.Diagnostics.Process]$Process)

    Wait-Until -Description "main window for PID $($Process.Id)" -Condition {
        try {
            $Process.Refresh()
            return $Process.MainWindowHandle -ne 0
        }
        catch {
            return $false
        }
    }
}

function Test-WindowVisible {
    param([Parameter(Mandatory = $true)][System.Diagnostics.Process]$Process)

    $Process.Refresh()
    if ($Process.MainWindowHandle -eq 0) {
        return $false
    }
    return [ResinNativeWindow]::IsWindowVisible($Process.MainWindowHandle)
}

function Wait-ForWindowVisibility {
    param(
        [Parameter(Mandatory = $true)][System.Diagnostics.Process]$Process,
        [Parameter(Mandatory = $true)][bool]$Visible
    )

    $label = if ($Visible) { 'visible window' } else { 'hidden window' }
    Wait-Until -Description $label -Condition { (Test-WindowVisible -Process $Process) -eq $Visible }
}

function Wait-ForCoreProcess {
    param([Parameter(Mandatory = $true)][string]$CoreExecutablePath)

    Wait-Until -Description "core process for $CoreExecutablePath" -Condition {
        @(Get-ProcessesByExecutablePath -ExecutablePath $CoreExecutablePath).Count -gt 0
    }
}

function Get-SingleCoreProcess {
    param([Parameter(Mandatory = $true)][string]$CoreExecutablePath)

    $processes = @(Get-ProcessesByExecutablePath -ExecutablePath $CoreExecutablePath)
    if ($processes.Count -ne 1) {
        throw "Expected exactly one core process for $CoreExecutablePath, found $($processes.Count)"
    }
    return $processes[0]
}

function Wait-ForNoCoreProcess {
    param([Parameter(Mandatory = $true)][string]$CoreExecutablePath)

    Wait-Until -Description "no core process for $CoreExecutablePath" -Condition {
        @(Get-ProcessesByExecutablePath -ExecutablePath $CoreExecutablePath).Count -eq 0
    }
}

function Wait-ForHealthcheck200 {
    param([Parameter(Mandatory = $true)][string]$HealthUrl)

    Wait-Until -Description "healthcheck 200 on $HealthUrl" -Condition {
        try {
            $response = Invoke-WebRequest -Uri $HealthUrl -UseBasicParsing -TimeoutSec 2
            return $response.StatusCode -eq 200
        }
        catch {
            return $false
        }
    }
}

function Write-CompletedShellConfig {
    param([Parameter(Mandatory = $true)]$PortablePaths)

    New-Item -ItemType Directory -Path $PortablePaths.DesktopDataDir -Force | Out-Null
    $payload = @'
{
  "version": 1,
  "wizard_completed": true,
  "listen_address": "127.0.0.1",
  "port": 2260
}
'@
    [System.IO.File]::WriteAllText($PortablePaths.ShellConfigPath, $payload, [System.Text.UTF8Encoding]::new($false))
}

function Write-InvalidShellConfig {
    param([Parameter(Mandatory = $true)]$PortablePaths)

    New-Item -ItemType Directory -Path $PortablePaths.DesktopDataDir -Force | Out-Null
    $payload = @'
{
  "version": 1,
  "wizard_completed": true,
  "listen_address": "0.0.0.0",
  "port": 2260
}
'@
    [System.IO.File]::WriteAllText($PortablePaths.ShellConfigPath, $payload, [System.Text.UTF8Encoding]::new($false))
}

function Invoke-GoTestCase {
    param(
        [Parameter(Mandatory = $true)][string]$Package,
        [Parameter(Mandatory = $true)][string]$RunPattern
    )

    Invoke-NativeCommand -FilePath 'go.exe' -ArgumentList @(
        'test',
        '-count=1',
        '-run',
        $RunPattern,
        $Package
    )
}

function Start-DesktopScenario {
    param(
        [Parameter(Mandatory = $true)]$PortablePaths,
        [Parameter(Mandatory = $true)][string]$Port
    )

    $desktop = Start-PortableProcess -ExecutablePath $PortablePaths.DesktopExe -Environment @{
        RESIN_SMOKE_PORT = $Port
        RESIN_SMOKE_LISTEN_ADDRESS = '127.0.0.1'
    }

    return $desktop
}

function Assert-PortableFilesystem {
    param(
        [Parameter(Mandatory = $true)]$PortablePaths,
        [Parameter(Mandatory = $true)][string]$PackageVariant
    )

    foreach ($path in @($PortablePaths.DesktopExe, $PortablePaths.CoreExe)) {
        if (-not (Test-Path $path)) {
            throw "Portable artifact path missing: $path"
        }
    }

    switch ($PackageVariant) {
        'Full' {
            if (-not (Test-Path $PortablePaths.RuntimeExe)) {
                throw "Portable artifact path missing: $($PortablePaths.RuntimeExe)"
            }
        }
        'Lite' {
            if (Test-Path $PortablePaths.RuntimeExe) {
                throw "Lite portable package must not bundle fixed runtime: $($PortablePaths.RuntimeExe)"
            }
        }
    }
}

function Stop-PortableRootProcesses {
    param([Parameter(Mandatory = $true)]$PortablePaths)

    $desktopProcesses = @(Get-ProcessesByExecutablePath -ExecutablePath $PortablePaths.DesktopExe)
    foreach ($desktopProcess in $desktopProcesses) {
        try {
            Stop-Process -Id $desktopProcess.ProcessId -Force -ErrorAction SilentlyContinue
        }
        catch {
        }
    }

    $coreProcesses = @(Get-ProcessesByExecutablePath -ExecutablePath $PortablePaths.CoreExe)
    foreach ($coreProcess in $coreProcesses) {
        try {
            Stop-Process -Id $coreProcess.ProcessId -Force -ErrorAction SilentlyContinue
        }
        catch {
        }
    }

    Start-Sleep -Milliseconds 300
}

function Invoke-FirstLaunchScenario {
    param([Parameter(Mandatory = $true)]$PortablePaths)

    $firstPort = Get-FreeTcpPort
    $firstDesktop = $null
    $runningDesktop = $null
    try {
        $firstDesktop = Start-DesktopScenario -PortablePaths $PortablePaths -Port $firstPort
        Wait-ForMainWindow -Process $firstDesktop
        Wait-Until -Description 'first-launch data directories' -Condition {
            (Test-Path $PortablePaths.StateDir) -and
            (Test-Path $PortablePaths.CacheDir) -and
            (Test-Path $PortablePaths.LogDir) -and
            (Test-Path $PortablePaths.DesktopDataDir) -and
            (Test-Path $PortablePaths.SecretsPath)
        }
    }
    finally {
        Stop-PortableProcess -Process $firstDesktop
    }

    Write-CompletedShellConfig -PortablePaths $PortablePaths
    $runPort = Get-FreeTcpPort
    $healthUrl = "http://127.0.0.1:$runPort/healthz"

    try {
        $runningDesktop = Start-DesktopScenario -PortablePaths $PortablePaths -Port $runPort
        Wait-ForMainWindow -Process $runningDesktop
        Wait-ForCoreProcess -CoreExecutablePath $PortablePaths.CoreExe
        Wait-ForHealthcheck200 -HealthUrl $healthUrl

        $closeRequested = $runningDesktop.CloseMainWindow()
        if (-not $closeRequested) {
            throw 'Desktop window did not accept CloseMainWindow() request.'
        }

        Wait-ForWindowVisibility -Process $runningDesktop -Visible $false
        $coreProcess = Get-SingleCoreProcess -CoreExecutablePath $PortablePaths.CoreExe
        if ($null -eq $coreProcess) {
            throw 'Core process lookup failed after hide-to-tray.'
        }
        Wait-ForHealthcheck200 -HealthUrl $healthUrl
    }
    finally {
        Stop-PortableProcess -Process $runningDesktop
        Stop-PortableRootProcesses -PortablePaths $PortablePaths
    }

    Write-Token -Token 'ZIP_STRUCTURE=OK'
    Write-Token -Token 'CORE_START=OK'
    Write-Token -Token 'HEALTHCHECK=200'
    Write-Token -Token 'WINDOW_CLOSE=HIDE_TO_TRAY'
    Write-Token -Token 'CORE_STILL_RUNNING=TRUE'
}

function Invoke-ExplicitExitScenario {
    param([Parameter(Mandatory = $true)]$PortablePaths)

    Write-CompletedShellConfig -PortablePaths $PortablePaths
    $port = Get-FreeTcpPort
    $healthUrl = "http://127.0.0.1:$port/healthz"
    $desktop = $null
    try {
        $desktop = Start-DesktopScenario -PortablePaths $PortablePaths -Port $port
        Wait-ForMainWindow -Process $desktop
        Wait-ForCoreProcess -CoreExecutablePath $PortablePaths.CoreExe
        Wait-ForHealthcheck200 -HealthUrl $healthUrl
    }
    finally {
        Stop-PortableProcess -Process $desktop
        Stop-PortableRootProcesses -PortablePaths $PortablePaths
    }

    Invoke-GoTestCase -Package './desktop/internal/supervisor' -RunPattern '^TestProcessSupervisor_GracefulExitByCtrlBreak$'
    Invoke-GoTestCase -Package './desktop/internal/wailsapp' -RunPattern '^TestShellLifecycle_ExplicitExitStopsCore$'

    Write-Token -Token 'TRAY_EXIT=OK'
    Write-Token -Token 'CORE_STOPPED=TRUE'
    Write-Token -Token 'ORPHAN_PROCESS=FALSE'
}

function Invoke-SecondInstanceScenario {
    param([Parameter(Mandatory = $true)]$PortablePaths)

    Write-CompletedShellConfig -PortablePaths $PortablePaths
    $port = Get-FreeTcpPort
    $healthUrl = "http://127.0.0.1:$port/healthz"
    $primary = $null
    $secondary = $null
    try {
        $primary = Start-DesktopScenario -PortablePaths $PortablePaths -Port $port
        Wait-ForMainWindow -Process $primary
        Wait-ForCoreProcess -CoreExecutablePath $PortablePaths.CoreExe
        Wait-ForHealthcheck200 -HealthUrl $healthUrl

        $closeRequested = $primary.CloseMainWindow()
        if (-not $closeRequested) {
            throw 'Primary desktop window did not accept CloseMainWindow() request.'
        }
        Wait-ForWindowVisibility -Process $primary -Visible $false

        $secondary = Start-DesktopScenario -PortablePaths $PortablePaths -Port $port
        if (-not $secondary.WaitForExit(5000)) {
            throw 'Secondary desktop instance did not exit after reattach.'
        }

        Wait-ForWindowVisibility -Process $primary -Visible $true
        $coreProcesses = @(Get-ProcessesByExecutablePath -ExecutablePath $PortablePaths.CoreExe)
        if ($coreProcesses.Count -ne 1) {
            throw "Expected exactly one core process after reattach, found $($coreProcesses.Count)"
        }
        Wait-ForHealthcheck200 -HealthUrl $healthUrl
    }
    finally {
        Stop-PortableProcess -Process $secondary
        Stop-PortableProcess -Process $primary
        Stop-PortableRootProcesses -PortablePaths $PortablePaths
    }

    Write-Token -Token 'SECOND_INSTANCE=REATTACH'
    Write-Token -Token 'DUPLICATE_CORE=FALSE'
}

function Invoke-PortCollisionScenario {
    param([Parameter(Mandatory = $true)]$PortablePaths)

    Write-CompletedShellConfig -PortablePaths $PortablePaths
    $port = Get-FreeTcpPort
    $listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Parse('127.0.0.1'), [int]$port)
    $desktop = $null
    try {
        $listener.Start()
        $desktop = Start-DesktopScenario -PortablePaths $PortablePaths -Port $port
        Wait-ForMainWindow -Process $desktop
        Start-Sleep -Milliseconds 800
        $coreProcesses = @(Get-ProcessesByExecutablePath -ExecutablePath $PortablePaths.CoreExe)
        if ($coreProcesses.Count -ne 0) {
            throw 'Core should not start while the smoke port is already occupied.'
        }
    }
    finally {
        Stop-PortableProcess -Process $desktop
        if ($listener) {
            $listener.Stop()
        }
        Stop-PortableRootProcesses -PortablePaths $PortablePaths
    }

    Invoke-GoTestCase -Package './desktop/internal/supervisor' -RunPattern '^TestProcessSupervisor_PortCollision$'
    Invoke-GoTestCase -Package './desktop/internal/wailsapp' -RunPattern '^TestShellLifecycle_DiagnosticsExposeLogPath$'

    Write-Token -Token 'STARTUP_BLOCKED=PORT_IN_USE'
}

function Invoke-InvalidConfigScenario {
    param([Parameter(Mandatory = $true)]$PortablePaths)

    Write-InvalidShellConfig -PortablePaths $PortablePaths
    $port = Get-FreeTcpPort
    $desktop = $null
    try {
        $desktop = Start-DesktopScenario -PortablePaths $PortablePaths -Port $port
        Wait-ForMainWindow -Process $desktop
        Start-Sleep -Milliseconds 800
        $coreProcesses = @(Get-ProcessesByExecutablePath -ExecutablePath $PortablePaths.CoreExe)
        if ($coreProcesses.Count -ne 0) {
            throw 'Core should not start when shell-config.json is invalid.'
        }
        if (-not (Test-Path $PortablePaths.ShellConfigPath)) {
            throw 'Invalid shell-config.json disappeared unexpectedly.'
        }
    }
    finally {
        Stop-PortableProcess -Process $desktop
        Stop-PortableRootProcesses -PortablePaths $PortablePaths
    }

    Invoke-GoTestCase -Package './desktop/internal/wailsapp' -RunPattern '^TestShellLifecycle_InvalidConfigShowsDiagnostics$'

    Write-Token -Token 'CONFIG_VALIDATION=FAILED'
}

$resolvedPackageVariant = Resolve-PackageVariant -ArchivePath $resolvedZipPath -Variant $PackageVariant
$portablePaths = $null

try {
    Assert-PortableZipStructure -ArchivePath $resolvedZipPath -PackageVariant $resolvedPackageVariant
    Expand-PortableZip -ArchivePath $resolvedZipPath -DestinationRoot $extractRoot
    $portablePaths = Get-PortablePaths -Root $extractRoot
    Assert-PortableFilesystem -PortablePaths $portablePaths -PackageVariant $resolvedPackageVariant

    switch ($Scenario) {
        'zip-structure' {
            Write-Token -Token 'ZIP_STRUCTURE=OK'
        }
        'first-launch' {
            Invoke-FirstLaunchScenario -PortablePaths $portablePaths
        }
        'explicit-exit' {
            Invoke-ExplicitExitScenario -PortablePaths $portablePaths
        }
        'second-instance' {
            Invoke-SecondInstanceScenario -PortablePaths $portablePaths
        }
        'port-collision' {
            Invoke-PortCollisionScenario -PortablePaths $portablePaths
        }
        'invalid-config' {
            Invoke-InvalidConfigScenario -PortablePaths $portablePaths
        }
    }

    Write-Token -Token ("SCENARIO=$Scenario")
    Write-Token -Token ("PACKAGE_VARIANT=$resolvedPackageVariant")
    Write-Token -Token 'SCENARIO_STATUS=OK'
    if (-not [string]::IsNullOrWhiteSpace($SummaryPath)) {
        Write-ScenarioSummary -DestinationPath $SummaryPath -Scenario $Scenario -PackageVariant $resolvedPackageVariant -ZipPath $resolvedZipPath -KeepArtifacts:$KeepArtifacts
    }
}
finally {
    if ($null -ne $portablePaths) {
        Stop-PortableRootProcesses -PortablePaths $portablePaths
    }
    Remove-ScenarioArtifacts -ScenarioRoot $scenarioRoot -SmokeRoot $smokeRoot -KeepArtifacts:$KeepArtifacts
}
