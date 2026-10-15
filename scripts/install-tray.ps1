# Reversible per-user migration. Does not modify the Alloy service or credentials.
[CmdletBinding()]
param(
    [string]$Binary = (Join-Path $PSScriptRoot '..\entra-token-agent-tray.exe'),
    [string]$InstallDir = (Join-Path $env:LOCALAPPDATA 'HomeLab\EntraMetrics'),
    [string]$RestoreBackup
)
$ErrorActionPreference = 'Stop'
$InstallDir = [IO.Path]::GetFullPath($InstallDir)
$runKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
$valueName = 'HomeLabEntraMetrics'
$target = Join-Path $InstallDir 'entra-token-agent-tray.exe'
$config = Join-Path $InstallDir 'config.yaml'

if (-not ('EntraTrayMigration' -as [type])) {
    Add-Type @'
using System;
using System.Runtime.InteropServices;
public static class EntraTrayMigration {
    public delegate bool Callback(IntPtr hwnd, IntPtr data);
    [DllImport("user32.dll")] static extern bool EnumWindows(Callback callback, IntPtr data);
    [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr hwnd, out uint pid);
    [DllImport("user32.dll")] static extern bool PostMessage(IntPtr hwnd, uint message, IntPtr wparam, IntPtr lparam);
    public static void Exit(uint target) {
        EnumWindows((hwnd, data) => {
            uint pid; GetWindowThreadProcessId(hwnd, out pid);
            if (pid == target) PostMessage(hwnd, 0x111, new IntPtr(3), IntPtr.Zero);
            return true;
        }, IntPtr.Zero);
    }
}
'@
}

function Stop-InstalledAgent {
    # Never trust a stale PID file or stop by executable name alone.
    $processes = @(Get-CimInstance Win32_Process)
    foreach ($p in $processes) {
        $isTray = $p.ExecutablePath -eq $target
        $isAgent = $p.ExecutablePath -eq (Join-Path $InstallDir 'agent.exe')
        $isLauncher = $p.Name -eq 'powershell.exe' -and
            $p.CommandLine -like ('*-File "' + (Join-Path $InstallDir 'start-agent.ps1') + '"*')
        if ($isTray -or $isAgent -or $isLauncher) {
            $current = Get-CimInstance Win32_Process -Filter "ProcessId=$($p.ProcessId)"
            if ($current -and $current.CreationDate -eq $p.CreationDate -and
                $current.ExecutablePath -eq $p.ExecutablePath -and $current.CommandLine -eq $p.CommandLine) {
                if ($isTray) {
                    [EntraTrayMigration]::Exit($p.ProcessId)
                    $running = Get-Process -Id $p.ProcessId -ErrorAction SilentlyContinue
                    if (-not $running -or $running.WaitForExit(10000)) { continue }
                    throw 'Tray did not stop cleanly; leaving installation unchanged.'
                }
                Stop-Process -Id $p.ProcessId -Force
            }
        }
    }
}

function Restore-Installation([string]$backup) {
    $manifest = Get-Content -LiteralPath (Join-Path $backup 'manifest.json') -Raw | ConvertFrom-Json
    if ($manifest.InstallDir -ne $InstallDir) { throw 'Backup belongs to a different installation.' }
    Stop-InstalledAgent
    foreach ($name in @('agent.exe', 'start-agent.ps1', 'entra-token-agent-tray.exe')) {
        $saved = Join-Path $backup $name
        if (Test-Path -LiteralPath $saved) { Copy-Item -LiteralPath $saved -Destination (Join-Path $InstallDir $name) -Force }
        elseif ($name -eq 'entra-token-agent-tray.exe' -and (Test-Path -LiteralPath $target)) {
            Remove-Item -LiteralPath $target -Force
        }
    }
    if ($manifest.HadRunValue) {
        New-ItemProperty -Path $runKey -Name $valueName -Value $manifest.RunValue -PropertyType String -Force | Out-Null
    } else { Remove-ItemProperty -Path $runKey -Name $valueName -ErrorAction SilentlyContinue }
    if ($manifest.WasRunning) {
        if ($manifest.RunValue -like '*entra-token-agent-tray.exe*') {
            Start-Process -FilePath $target -ArgumentList @('-config', ('"' + $config + '"')) -WorkingDirectory $InstallDir -WindowStyle Hidden
        } else {
            Start-Process -FilePath 'powershell.exe' -ArgumentList @('-NoProfile','-WindowStyle','Hidden','-File',('"' + (Join-Path $InstallDir 'start-agent.ps1') + '"')) -WorkingDirectory $InstallDir -WindowStyle Hidden
        }
    }
}

if ($RestoreBackup) {
    Restore-Installation ([IO.Path]::GetFullPath($RestoreBackup))
    Write-Output "Restored: $RestoreBackup"
    return
}
if (-not (Test-Path -LiteralPath $config)) { throw "Existing configuration required: $config" }
$address = '127.0.0.1:9099'
$configText = Get-Content -LiteralPath $config -Raw
if ($configText -match '(?m)^metrics_addr:\s*([^\r\n#]+)') {
    $address = $Matches[1].Trim().Trim([char]34, [char]39)
}
if ($address.StartsWith(':')) { $address = '127.0.0.1' + $address }
if ($address.StartsWith('0.0.0.0:')) { $address = $address.Replace('0.0.0.0:', '127.0.0.1:') }
$healthUrl = 'http://' + $address + '/healthz'
$Binary = (Resolve-Path -LiteralPath $Binary).Path
if ($Binary -eq $target) { throw 'Use a staged binary outside the installation directory.' }
$image = [IO.File]::ReadAllBytes($Binary)
$peOffset = [BitConverter]::ToInt32($image, 0x3c)
if ([BitConverter]::ToUInt16($image, ($peOffset + 24 + 68)) -ne 2) {
    throw 'Expected a Windows GUI-subsystem executable (build with -H windowsgui).'
}
$backup = Join-Path $InstallDir ('backups\' + (Get-Date -Format 'yyyyMMdd-HHmmss-fff'))
New-Item -ItemType Directory -Path $backup -Force | Out-Null
$oldRun = (Get-ItemProperty -Path $runKey -ErrorAction SilentlyContinue).$valueName
$wasRunning = @(Get-CimInstance Win32_Process | Where-Object {
    $_.ExecutablePath -eq $target -or $_.ExecutablePath -eq (Join-Path $InstallDir 'agent.exe')
}).Count -gt 0
@{ InstallDir=$InstallDir; HadRunValue=($null -ne $oldRun); RunValue=$oldRun; WasRunning=$wasRunning } |
    ConvertTo-Json | Set-Content -LiteralPath (Join-Path $backup 'manifest.json') -Encoding UTF8
foreach ($name in @('agent.exe', 'start-agent.ps1', 'entra-token-agent-tray.exe', 'agent-stderr.log', 'agent-stdout.log')) {
    $source = Join-Path $InstallDir $name
    if (Test-Path -LiteralPath $source) { Copy-Item -LiteralPath $source -Destination $backup }
}
try {
    Stop-InstalledAgent
    Copy-Item -LiteralPath $Binary -Destination $target -Force
    New-Item -Path $runKey -Force | Out-Null
    New-ItemProperty -Path $runKey -Name $valueName -PropertyType String -Force -Value ('"' + $target + '" -config "' + $config + '"') | Out-Null
    $p = Start-Process -FilePath $target -ArgumentList @('-config', ('"' + $config + '"')) -WorkingDirectory $InstallDir -WindowStyle Hidden -PassThru
    Start-Sleep -Seconds 3
    $p.Refresh()
    if ($p.HasExited) { throw 'Tray exited during startup; see agent-stderr.log.' }
    $pidPath = Join-Path $InstallDir 'agent.pid'
    if (-not (Test-Path -LiteralPath $pidPath) -or [int](Get-Content -LiteralPath $pidPath) -ne $p.Id) {
        throw 'Tray did not acquire the instance mutex.'
    }
    $health = Invoke-WebRequest -Uri $healthUrl -UseBasicParsing -TimeoutSec 5
    if ($health.StatusCode -ne 204) { throw 'Agent health check failed.' }
    Write-Output "Installed tray PID: $($p.Id)"
    Write-Output "Backup: $backup"
    Write-Output "Restore: .\scripts\install-tray.ps1 -InstallDir `"$InstallDir`" -RestoreBackup `"$backup`""
} catch {
    $failure = $_
    Restore-Installation $backup
    throw $failure
}
