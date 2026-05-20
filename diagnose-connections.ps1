# Script di diagnostica connessioni browser-server

Write-Host "=== DIAGNOSTICA CONNESSIONI XDCC-SERVER ===" -ForegroundColor Cyan
Write-Host ""

# 1. Verifica processo server
Write-Host "[1] Verifica processo server..." -ForegroundColor Yellow
$proc = Get-Process -Name "xdcc-server" -ErrorAction SilentlyContinue
if ($proc) {
    Write-Host "✓ Server in esecuzione (PID: $($proc.Id))" -ForegroundColor Green
} else {
    Write-Host "✗ Server NON in esecuzione!" -ForegroundColor Red
    exit 1
}

# 2. Verifica porta 8080
Write-Host ""
Write-Host "[2] Verifica porta 8080..." -ForegroundColor Yellow
$listening = netstat -ano | Select-String ":8080" | Select-String "LISTENING"
if ($listening) {
    Write-Host "✓ Porta 8080 in LISTENING" -ForegroundColor Green
} else {
    Write-Host "✗ Porta 8080 NON in listening!" -ForegroundColor Red
}

# 3. Conta connessioni ESTABLISHED
Write-Host ""
Write-Host "[3] Connessioni ESTABLISHED sulla porta 8080..." -ForegroundColor Yellow
$conns = netstat -ano | Select-String ":8080" | Select-String "ESTABLISHED"
Write-Host "   Totale connessioni: $($conns.Count)" -ForegroundColor Cyan

if ($conns.Count -gt 0) {
    Write-Host "   Dettaglio connessioni:" -ForegroundColor Gray
    $conns | ForEach-Object {
        $line = $_ -replace '\s+', ' '
        Write-Host "   $line" -ForegroundColor Gray
    }
}

# Valutazione
Write-Host ""
Write-Host "[4] Analisi..." -ForegroundColor Yellow
if ($conns.Count -eq 0) {
    Write-Host "⚠️  NESSUNA connessione attiva - il browser è collegato?" -ForegroundColor Yellow
} elseif ($conns.Count -lt 6) {
    Write-Host "✓ Connection pool OK ($($conns.Count)/6 connessioni usate)" -ForegroundColor Green
} else {
    Write-Host "⚠️  Connection pool SATURO ($($conns.Count)/6 - PROBLEMA!)" -ForegroundColor Red
}

# 5. Test connessione HTTP
Write-Host ""
Write-Host "[5] Test connessione HTTP..." -ForegroundColor Yellow
try {
    $response = Invoke-WebRequest -Uri "http://localhost:8080/api/stats" -TimeoutSec 5 -UseBasicParsing
    Write-Host "✓ Server risponde (HTTP $($response.StatusCode))" -ForegroundColor Green
} catch {
    Write-Host "✗ Server NON risponde: $($_.Exception.Message)" -ForegroundColor Red
}

# 6. Istruzioni finali
Write-Host ""
Write-Host "=== ISTRUZIONI PER L'UTENTE ===" -ForegroundColor Cyan
Write-Host ""
Write-Host "Se connection pool OK ma search non funziona:" -ForegroundColor White
Write-Host "  1. Apri browser su http://localhost:8080" -ForegroundColor Gray
Write-Host "  2. F12 → Network tab" -ForegroundColor Gray
Write-Host "  3. Hard refresh (Ctrl+Shift+R) per cancellare cache" -ForegroundColor Gray
Write-Host "  4. Cerca 'index-BFKvqU2P.js' nel Network tab" -ForegroundColor Gray
Write-Host "     - Se NON lo vedi → cache vecchia, riprova hard refresh" -ForegroundColor Gray
Write-Host "  5. Prova una ricerca" -ForegroundColor Gray
Write-Host "  6. Controlla se /api/search appare nel Network tab" -ForegroundColor Gray
Write-Host "     - Se SI ma resta pending → guarda Timing tab" -ForegroundColor Gray
Write-Host "     - Se NO → problema JavaScript, guarda Console tab" -ForegroundColor Gray
Write-Host ""
