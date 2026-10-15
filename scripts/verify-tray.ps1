# Smoke test for an already-running installation; does not alter credentials.
[CmdletBinding()]
param(
    [string]$InstallDir = (Join-Path $env:LOCALAPPDATA 'HomeLab\EntraMetrics'),
    [string]$BaseUrl = 'http://127.0.0.1:9099'
)
$ErrorActionPreference = 'Stop'
if (-not ('EntraTrayProbe' -as [type])) {
    Add-Type @'
using System;
using System.Runtime.InteropServices;
using System.Text;
public static class EntraTrayProbe {
    public delegate bool Callback(IntPtr hwnd, IntPtr data);
    [StructLayout(LayoutKind.Sequential)] struct IconId { public uint Size; public IntPtr Window; public uint ID; public Guid Guid; }
    [StructLayout(LayoutKind.Sequential)] struct Rect { public int Left, Top, Right, Bottom; }
    [DllImport("user32.dll")] static extern bool EnumWindows(Callback callback, IntPtr data);
    [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr hwnd, out uint pid);
    [DllImport("user32.dll")] static extern bool IsWindowVisible(IntPtr hwnd);
    [DllImport("user32.dll", CharSet=CharSet.Unicode)] static extern int GetClassName(IntPtr hwnd, StringBuilder text, int size);
    [DllImport("shell32.dll")] static extern int Shell_NotifyIconGetRect(ref IconId id, out Rect rect);
    public static string Check(uint target) {
        bool found=false, icon=false, visible=false;
        EnumWindows((hwnd, data) => {
            uint pid; GetWindowThreadProcessId(hwnd, out pid);
            if (pid != target) return true;
            var name=new StringBuilder(256); GetClassName(hwnd,name,256);
            if (name.ToString() == "EntraTokenAgentTray") {
                found=true; visible=IsWindowVisible(hwnd);
                var id=new IconId { Size=(uint)Marshal.SizeOf(typeof(IconId)), Window=hwnd, ID=1 };
                Rect rect; icon=Shell_NotifyIconGetRect(ref id,out rect)==0;
            }
            return true;
        },IntPtr.Zero);
        if (!found || !icon) throw new Exception("Tray window or notification icon missing.");
        return "Tray registered; sign-in window visible="+visible;
    }
}
'@
}
$target = Join-Path $InstallDir 'entra-token-agent-tray.exe'
$instance = [int](Get-Content -LiteralPath (Join-Path $InstallDir 'agent.pid'))
$process = Get-CimInstance Win32_Process -Filter "ProcessId=$instance"
if (-not $process -or $process.ExecutablePath -ne $target) { throw 'PID does not identify the installed tray.' }
Write-Output ([EntraTrayProbe]::Check($instance))
foreach ($endpoint in @('healthz','readyz','metrics')) {
    $response = Invoke-WebRequest -Uri ($BaseUrl.TrimEnd('/') + '/' + $endpoint) -UseBasicParsing -TimeoutSec 5
    Write-Output "$endpoint : $($response.StatusCode)"
    if ($endpoint -eq 'metrics' -and $response.Content -notmatch 'entra_token_agent_last_refresh_success_timestamp_seconds') { throw 'Agent metrics missing.' }
}
$duplicate = Start-Process -FilePath $target -ArgumentList @('-config', ('"' + (Join-Path $InstallDir 'config.yaml') + '"')) -WindowStyle Hidden -PassThru
if (-not $duplicate.WaitForExit(10000) -or $duplicate.ExitCode -ne 0) { throw 'Duplicate instance did not exit cleanly.' }
if ([int](Get-Content -LiteralPath (Join-Path $InstallDir 'agent.pid')) -ne $instance) { throw 'Duplicate changed the PID file.' }
Write-Output 'Duplicate launch exited without replacing the running agent.'
