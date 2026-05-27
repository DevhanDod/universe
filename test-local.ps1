# Universe local end-to-end test
# Adapted for Windows / PowerShell / port 5433 / CGO_ENABLED=0
# Run from the repo root: .\test-local.ps1

param([switch]$NoBuild)

$ErrorActionPreference = "Continue"
$pass = 0
$fail = 0

function Test-Pass { param($msg) Write-Host "  [PASS] $msg" -ForegroundColor Green; $script:pass++ }
function Test-Fail { param($msg) Write-Host "  [FAIL] $msg" -ForegroundColor Red;  $script:fail++ }
function Section   { param($msg) Write-Host "`n=== $msg ===" -ForegroundColor Cyan }

# -- TEST 1: Build ------------------------------------------------------------
Section "Test 1: Build"
if (-not $NoBuild) {
    $env:CGO_ENABLED = "0"
    go build -ldflags "-X main.Version=test-local" -o universe.exe ./cmd/universe
    if ($LASTEXITCODE -ne 0) { Test-Fail "go build failed"; exit 1 }
}
$v = .\universe.exe --version 2>&1
if ($v -match "test-local") {
    Test-Pass "binary compiled: $v"
} else {
    Test-Fail "version mismatch: $v"
}

# -- TEST 2: Init -------------------------------------------------------------
Section "Test 2: universe init"
Remove-Item -Recurse -Force .universe -ErrorAction SilentlyContinue
.\universe.exe init 2>&1 | Out-Null
if (Test-Path ".universe\graph.json") {
    $raw = Get-Content ".universe\graph.json" -Raw
    $nodeCount = ([regex]::Matches($raw, '"id"\s*:')).Count
    Test-Pass "graph.json created -- ~$nodeCount nodes"
} else {
    Test-Fail "graph.json not created"
}

# -- TEST 3: DB connection ----------------------------------------------------
Section "Test 3: PostgreSQL connection"
# NOTE: universe-db runs on port 5433, not 5432 (leap-db is on 5432)
.\universe.exe config set db postgres://universe_admin:universe_secret_2024@localhost:5433/universe 2>&1 | Out-Null
$dbStatus = .\universe.exe db status 2>&1
if ($dbStatus -match "Connection successful") {
    Test-Pass "DB connected"
} else {
    Test-Fail "DB connection failed: $dbStatus"
}

# -- TEST 4: Migrations -------------------------------------------------------
Section "Test 4: Migrations"
$migrateOut = .\universe.exe db migrate 2>&1
if ($migrateOut -match "Migrations complete") {
    Test-Pass "migrations applied"
} else {
    Test-Fail "migrate failed: $migrateOut"
}

# -- TEST 5: Status -----------------------------------------------------------
Section "Test 5: universe status"
$status = .\universe.exe status 2>&1
$activeCount = ($status | Select-String "Active").Count
if ($activeCount -ge 5) {
    Test-Pass "all 5 engines Active"
} else {
    Test-Fail "only $activeCount engines Active: $status"
}

# -- TEST 6: Dashboard API ----------------------------------------------------
Section "Test 6: Dashboard API"
$dashProc = Start-Process -NoNewWindow -FilePath ".\universe.exe" -ArgumentList "dashboard","--port","3001","--no-open" -PassThru
Start-Sleep -Seconds 3
try {
    $endpoints = @("/api/overview", "/api/memory?limit=5", "/api/skills", "/api/routing?limit=5", "/api/graph/nodes")
    $allOk = $true
    foreach ($ep in $endpoints) {
        $code = & curl.exe -s -o NUL -w "%{http_code}" "http://localhost:3001$ep"
        if ($code -ne "200") {
            Test-Fail "GET $ep returned HTTP $code"
            $allOk = $false
        }
    }
    if ($allOk) { Test-Pass "all 5 API endpoints return 200" }
    $spa = & curl.exe -s -o NUL -w "%{http_code}" "http://localhost:3001/memory"
    if ($spa -eq "200") {
        Test-Pass "SPA fallback serves index.html"
    } else {
        Test-Fail "SPA fallback returned HTTP $spa"
    }
} finally {
    $dashProc | Stop-Process -ErrorAction SilentlyContinue
    Start-Sleep -Seconds 1
}

# -- TEST 7: MCP server -------------------------------------------------------
Section "Test 7: MCP server handshake"
$env:CGO_ENABLED = "0"
$mcpOut = & go run ./cmd/mcp-test/ 2>&1
$mcpStr = $mcpOut -join "`n"
if ($mcpStr -match "Tools registered: (\d+)") {
    Test-Pass "MCP handshake OK -- $($matches[1]) tools registered"
} else {
    Test-Fail "MCP handshake failed: $mcpStr"
}
if ($mcpStr -match "list_skills responded") {
    Test-Pass "list_skills tool call succeeded"
} else {
    Test-Fail "list_skills tool call failed"
}

# -- Summary ------------------------------------------------------------------
Write-Host "`n$('=' * 50)" -ForegroundColor White
if ($fail -eq 0) {
    Write-Host "ALL $pass TESTS PASSED" -ForegroundColor Green
} else {
    Write-Host "$pass passed, $fail FAILED" -ForegroundColor Red
    exit 1
}
