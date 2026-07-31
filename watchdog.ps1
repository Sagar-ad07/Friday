param([int]$Interval=15)

$LogFile = "D:\Friday - Prototype\go\watchdog.log"
$Instances = @(
    @{Name="BlueGuardian"; Port=8000; Exe="friday.exe";},
    @{Name="Exness30AED";  Port=8002; Exe="friday_exness.exe";}
)

function Log($m) { $t = Get-Date -Format "yyyy-MM-dd HH:mm:ss"; "$t $m" | Add-Content $LogFile; Write-Host "$t $m" }
Log "Watchdog started"

while ($true) {
    foreach ($i in $Instances) {
        $ok = $false
        try { $r = Invoke-WebRequest -Uri "http://localhost:$($i.Port)/health" -TimeoutSec 3 -UseBasicParsing -ErrorAction Stop; $ok = $r.StatusCode -eq 200 } catch {}
        if (!$ok) {
            Log "$($i.Name) DOWN on port $($i.Port). Restarting..."
            $exe = "D:\Friday - Prototype\go\$($i.Exe)"
            if (Test-Path $exe) { Start-Process -WindowStyle Hidden -FilePath $exe -WorkingDirectory "D:\Friday - Prototype\go" }
            Start-Sleep 5
            try { $r = Invoke-WebRequest -Uri "http://localhost:$($i.Port)/health" -TimeoutSec 3 -UseBasicParsing -ErrorAction Stop; Log "$($i.Name) restarted OK" } catch { Log "$($i.Name) RESTART FAILED" }
        }
    }
    Start-Sleep $Interval
}
