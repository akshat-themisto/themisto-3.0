param(
    [switch]$RegenerateCerts,
    [switch]$RecreateVolumes,
    [switch]$MockQwen,
    [switch]$GatewayLLM,
    [int]$GatewayLLMPort = 17179
)

$ErrorActionPreference = "Stop"

$Root = Resolve-Path (Join-Path $PSScriptRoot "..")
$CertsDir = Join-Path $Root "certs"
$CaDir = Join-Path $CertsDir "ca"
$ServerDir = Join-Path $CertsDir "server"
$EnvPath = Join-Path $Root ".env"
$PostgresInitDir = Join-Path $Root ".codex-tmp\postgres-initdb"

function Test-RegularFile($Path) {
    return (Test-Path -LiteralPath $Path -PathType Leaf)
}

function Invoke-Checked {
    param(
        [string]$FilePath,
        [string[]]$Arguments
    )

    & $FilePath @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$FilePath exited with code $LASTEXITCODE"
    }
}

function Ensure-EnvFile {
    if (Test-Path -LiteralPath $EnvPath -PathType Leaf) {
        return
    }

    @"
POSTGRES_PASSWORD=themisto_dev_password
ADMIN_API_KEY=themisto_dev_admin_key_123
DASHBOARD_HOST_PORT=3010
BACKEND_HOST_PORT=8643
GATEWAY_HOST_PORT=9543
GATEWAY_HEALTH_HOST_PORT=18082
POSTGRES_HOST_PORT=5434
PUBLIC_BACKEND_URL=http://localhost:8643
PUBLIC_GATEWAY_URL=https://localhost:9543
PROMPT_SEMANTICS_ENABLED=false
QWEN_CLASSIFIER_URL=
QWEN_CLASSIFIER_API_KEY=
QWEN_CLASSIFIER_TIMEOUT=900ms
"@ | Set-Content -LiteralPath $EnvPath -Encoding ascii
    Write-Host "Created .env with Themisto 2.0 dev defaults."
}

function Update-LegacyMain20Ports {
    if (-not (Test-Path -LiteralPath $EnvPath -PathType Leaf)) {
        return
    }

    $content = Get-Content -LiteralPath $EnvPath
    $replacements = @{
        "DASHBOARD_HOST_PORT=3001"       = "DASHBOARD_HOST_PORT=3010"
        "BACKEND_HOST_PORT=8543"         = "BACKEND_HOST_PORT=8643"
        "GATEWAY_HOST_PORT=9443"         = "GATEWAY_HOST_PORT=9543"
        "GATEWAY_HEALTH_HOST_PORT=18081" = "GATEWAY_HEALTH_HOST_PORT=18082"
        "POSTGRES_HOST_PORT=5433"        = "POSTGRES_HOST_PORT=5434"
        "PUBLIC_BACKEND_URL=http://localhost:8543" = "PUBLIC_BACKEND_URL=http://localhost:8643"
        "PUBLIC_GATEWAY_URL=https://localhost:9443" = "PUBLIC_GATEWAY_URL=https://localhost:9543"
    }

    $changed = $false
    $next = foreach ($line in $content) {
        if ($replacements.ContainsKey($line)) {
            $changed = $true
            $replacements[$line]
        } else {
            $line
        }
    }

    if ($changed) {
        $next | Set-Content -LiteralPath $EnvPath -Encoding ascii
        Write-Host "Updated .env from old main 2.0 dev ports to non-conflicting ports."
    }
}

function Sync-PostgresInitScripts {
    $migrationsDir = Join-Path $Root "db\migrations"
    if (-not (Test-Path -LiteralPath $migrationsDir -PathType Container)) {
        throw "Missing migrations directory: $migrationsDir"
    }

    if (Test-Path -LiteralPath $PostgresInitDir) {
        Remove-Item -LiteralPath $PostgresInitDir -Recurse -Force
    }
    New-Item -ItemType Directory -Force -Path $PostgresInitDir | Out-Null

    Get-ChildItem -LiteralPath $migrationsDir -Filter "*.up.sql" |
        Sort-Object Name |
        ForEach-Object {
            Copy-Item -LiteralPath $_.FullName -Destination (Join-Path $PostgresInitDir $_.Name) -Force
        }

    $count = (Get-ChildItem -LiteralPath $PostgresInitDir -Filter "*.sql" | Measure-Object).Count
    if ($count -eq 0) {
        throw "No up migrations were staged for Postgres init."
    }
    Write-Host "Staged $count Postgres init migration(s)."
}

function Ensure-OpenSSL {
    $cmd = Get-Command openssl -ErrorAction SilentlyContinue
    if (-not $cmd) {
        throw "OpenSSL was not found on PATH. Install Git for Windows, OpenSSL, or use WSL/Git Bash to run scripts/gen-ca.sh and scripts/gen-server-cert.sh."
    }
}

function New-ThemistoCertsWithBash {
    $bash = Get-Command bash -ErrorAction SilentlyContinue
    if (-not $bash) {
        return $false
    }

    if (Test-Path -LiteralPath $CertsDir) {
        Remove-Item -LiteralPath $CertsDir -Recurse -Force
    }

    Write-Host "Generating Themisto development certificates with Bash scripts..."
    $tmpDir = Join-Path $Root ".codex-tmp\themisto-dev"
    New-Item -ItemType Directory -Force -Path $tmpDir | Out-Null
    $tmpCa = Join-Path $tmpDir "gen-ca.sh"
    $tmpServer = Join-Path $tmpDir "gen-server-cert.sh"
    (Get-Content -Raw -LiteralPath (Join-Path $Root "scripts\gen-ca.sh")).Replace("`r`n", "`n") | Set-Content -LiteralPath $tmpCa -Encoding ascii -NoNewline
    (Get-Content -Raw -LiteralPath (Join-Path $Root "scripts\gen-server-cert.sh")).Replace("`r`n", "`n") | Set-Content -LiteralPath $tmpServer -Encoding ascii -NoNewline

    Invoke-Checked "bash" @("./.codex-tmp/themisto-dev/gen-ca.sh", "./certs")
    Invoke-Checked "bash" @("./.codex-tmp/themisto-dev/gen-server-cert.sh", "./certs", "localhost")
    return $true
}

function New-ThemistoCerts {
    if (-not (Get-Command openssl -ErrorAction SilentlyContinue)) {
        if (New-ThemistoCertsWithBash) {
            return
        }
        Ensure-OpenSSL
    }

    if (Test-Path -LiteralPath $CertsDir) {
        Remove-Item -LiteralPath $CertsDir -Recurse -Force
    }
    New-Item -ItemType Directory -Force -Path $CaDir, $ServerDir | Out-Null

    $rootKey = Join-Path $CaDir "root-ca.key"
    $rootCrt = Join-Path $CaDir "root-ca.crt"
    $issuingKey = Join-Path $CaDir "issuing-ca.key"
    $issuingCsr = Join-Path $CaDir "issuing-ca.csr"
    $issuingCrt = Join-Path $CaDir "issuing-ca.crt"
    $caChain = Join-Path $CaDir "ca-chain.pem"
    $serverKey = Join-Path $ServerDir "server.key"
    $serverCsr = Join-Path $ServerDir "server.csr"
    $serverCrt = Join-Path $ServerDir "server.crt"
    $caExt = Join-Path $CaDir "issuing-ca.ext"
    $serverExt = Join-Path $ServerDir "server.ext"

    Write-Host "Generating Themisto development CA..."
    Invoke-Checked "openssl" @("ecparam", "-genkey", "-name", "secp384r1", "-noout", "-out", $rootKey)
    Invoke-Checked "openssl" @("req", "-new", "-x509", "-sha384", "-key", $rootKey, "-days", "3650", "-subj", "/C=US/O=Themisto Labs/CN=Themisto Root CA", "-addext", "basicConstraints=critical,CA:TRUE,pathlen:1", "-addext", "keyUsage=critical,keyCertSign,cRLSign", "-out", $rootCrt)
    Invoke-Checked "openssl" @("ecparam", "-genkey", "-name", "prime256v1", "-noout", "-out", $issuingKey)
    Invoke-Checked "openssl" @("req", "-new", "-sha256", "-key", $issuingKey, "-subj", "/C=US/O=Themisto Labs/CN=Themisto Issuing CA", "-out", $issuingCsr)
    "basicConstraints=critical,CA:TRUE,pathlen:0`nkeyUsage=critical,keyCertSign,cRLSign" | Set-Content -LiteralPath $caExt -Encoding ascii
    Invoke-Checked "openssl" @("x509", "-req", "-sha384", "-in", $issuingCsr, "-CA", $rootCrt, "-CAkey", $rootKey, "-CAcreateserial", "-days", "1095", "-extfile", $caExt, "-out", $issuingCrt)
    Get-Content -LiteralPath $issuingCrt, $rootCrt | Set-Content -LiteralPath $caChain -Encoding ascii

    Write-Host "Generating Themisto development gateway certificate..."
    Invoke-Checked "openssl" @("ecparam", "-genkey", "-name", "prime256v1", "-noout", "-out", $serverKey)
    Invoke-Checked "openssl" @("req", "-new", "-sha256", "-key", $serverKey, "-subj", "/O=Themisto Labs/CN=localhost", "-out", $serverCsr)
    "subjectAltName=DNS:localhost,IP:127.0.0.1`nkeyUsage=critical,digitalSignature`nextendedKeyUsage=serverAuth" | Set-Content -LiteralPath $serverExt -Encoding ascii
    Invoke-Checked "openssl" @("x509", "-req", "-sha256", "-in", $serverCsr, "-CA", $issuingCrt, "-CAkey", $issuingKey, "-CAcreateserial", "-days", "365", "-extfile", $serverExt, "-out", $serverCrt)
    Invoke-Checked "openssl" @("verify", "-CAfile", $caChain, $serverCrt)

    Remove-Item -LiteralPath $issuingCsr, $serverCsr, $caExt, $serverExt -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath (Join-Path $CaDir "root-ca.srl"), (Join-Path $CaDir "issuing-ca.srl") -Force -ErrorAction SilentlyContinue
}

function Ensure-Certs {
    $required = @(
        (Join-Path $CaDir "ca-chain.pem"),
        (Join-Path $CaDir "issuing-ca.crt"),
        (Join-Path $CaDir "issuing-ca.key"),
        (Join-Path $ServerDir "server.crt"),
        (Join-Path $ServerDir "server.key")
    )

    $invalid = $RegenerateCerts
    foreach ($path in $required) {
        if (-not (Test-RegularFile $path)) {
            $invalid = $true
            break
        }
    }

    if ($invalid) {
        Write-Host "Regenerating Themisto development certificates."
        New-ThemistoCerts
    }
}

function Invoke-DevSchemaFixups {
    Write-Host "Applying Themisto dev database schema fixups..."
    $sql = @"
ALTER TABLE admin_users ADD COLUMN IF NOT EXISTS must_change_password BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE devices ADD COLUMN IF NOT EXISTS renewal_token_hash TEXT;
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS public_backend_url TEXT NOT NULL DEFAULT '';
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS public_gateway_url TEXT NOT NULL DEFAULT '';
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS status_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS status_updated_at TIMESTAMPTZ;
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS status_updated_by TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_organizations_status ON organizations(status);
CREATE TABLE IF NOT EXISTS dlp_events (
    id               BIGSERIAL PRIMARY KEY,
    timestamp        TIMESTAMPTZ NOT NULL DEFAULT now(),
    device_id        TEXT NOT NULL,
    org_id           TEXT NOT NULL,
    request_id       TEXT,
    request_host     TEXT NOT NULL,
    request_path     TEXT,
    request_method   TEXT NOT NULL,
    source_app       TEXT,
    service_category TEXT,
    ai_vendor        TEXT,
    match_types      TEXT[] NOT NULL DEFAULT '{}',
    matched_patterns TEXT[] NOT NULL DEFAULT '{}',
    match_count      INTEGER NOT NULL DEFAULT 0,
    action_taken     TEXT NOT NULL DEFAULT 'alert'
);
ALTER TABLE dlp_events
    ADD COLUMN IF NOT EXISTS request_body_encrypted BYTEA,
    ADD COLUMN IF NOT EXISTS request_body_nonce BYTEA,
    ADD COLUMN IF NOT EXISTS request_body_truncated BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS policy_rule_id TEXT,
    ADD COLUMN IF NOT EXISTS reason_code TEXT,
    ADD COLUMN IF NOT EXISTS reason_detail TEXT,
    ADD COLUMN IF NOT EXISTS protocol TEXT NOT NULL DEFAULT 'http',
    ADD COLUMN IF NOT EXISTS intercepted_https BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS inspection_quality TEXT NOT NULL DEFAULT 'full',
    ADD COLUMN IF NOT EXISTS inspection_skip_reason TEXT,
    ADD COLUMN IF NOT EXISTS direction TEXT NOT NULL DEFAULT 'outbound',
    ADD COLUMN IF NOT EXISTS severity TEXT NOT NULL DEFAULT 'low',
    ADD COLUMN IF NOT EXISTS classification_reason TEXT,
    ADD COLUMN IF NOT EXISTS content_type TEXT,
    ADD COLUMN IF NOT EXISTS file_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS matched_fields TEXT[] NOT NULL DEFAULT '{}';
UPDATE dlp_events SET action_taken = 'alert' WHERE action_taken IS NULL OR action_taken = '';
UPDATE dlp_events SET protocol = 'http' WHERE protocol IS NULL OR protocol = '';
UPDATE dlp_events SET inspection_quality = 'full' WHERE inspection_quality IS NULL OR inspection_quality = '';
UPDATE dlp_events SET direction = 'outbound' WHERE direction IS NULL OR direction = '';
UPDATE dlp_events SET severity = 'low' WHERE severity IS NULL OR severity = '';
UPDATE dlp_events SET match_types = '{}' WHERE match_types IS NULL;
UPDATE dlp_events SET matched_patterns = '{}' WHERE matched_patterns IS NULL;
UPDATE dlp_events SET matched_fields = '{}' WHERE matched_fields IS NULL;
UPDATE dlp_events SET match_count = 0 WHERE match_count IS NULL;
UPDATE dlp_events SET file_count = 0 WHERE file_count IS NULL;
UPDATE dlp_events SET request_body_truncated = FALSE WHERE request_body_truncated IS NULL;
UPDATE dlp_events SET intercepted_https = FALSE WHERE intercepted_https IS NULL;
ALTER TABLE dlp_events
    DROP CONSTRAINT IF EXISTS dlp_events_action_taken_check,
    DROP CONSTRAINT IF EXISTS dlp_events_protocol_check,
    DROP CONSTRAINT IF EXISTS dlp_events_inspection_quality_check,
    DROP CONSTRAINT IF EXISTS dlp_events_direction_check,
    DROP CONSTRAINT IF EXISTS dlp_events_severity_check;
ALTER TABLE dlp_events
    ADD CONSTRAINT dlp_events_action_taken_check CHECK (action_taken IN ('alert', 'block', 'redact')),
    ADD CONSTRAINT dlp_events_protocol_check CHECK (protocol IN ('http', 'https', 'connect')),
    ADD CONSTRAINT dlp_events_inspection_quality_check CHECK (inspection_quality IN ('full', 'metadata_only', 'skipped')),
    ADD CONSTRAINT dlp_events_direction_check CHECK (direction IN ('outbound', 'inbound')),
    ADD CONSTRAINT dlp_events_severity_check CHECK (severity IN ('low', 'medium', 'high', 'critical'));
CREATE INDEX IF NOT EXISTS idx_dlp_events_org_time ON dlp_events (org_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_dlp_events_device ON dlp_events (device_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_dlp_events_ai_vendor ON dlp_events (ai_vendor, timestamp DESC) WHERE ai_vendor IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_dlp_events_policy_rule ON dlp_events (policy_rule_id) WHERE policy_rule_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_dlp_events_protocol_time ON dlp_events (protocol, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_dlp_events_direction_time ON dlp_events (direction, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_dlp_events_org_severity_time ON dlp_events (org_id, severity, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_dlp_events_body_retention ON dlp_events (timestamp) WHERE request_body_encrypted IS NOT NULL;
CREATE TABLE IF NOT EXISTS admin_user_alert_reads (
    user_id      UUID NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
    dlp_event_id BIGINT NOT NULL REFERENCES dlp_events(id) ON DELETE CASCADE,
    read_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, dlp_event_id)
);
CREATE INDEX IF NOT EXISTS idx_alert_reads_user_read_at ON admin_user_alert_reads (user_id, read_at DESC);
"@
    Invoke-Checked "docker" @("compose", "exec", "-T", "postgres", "psql", "-U", "themisto", "-d", "themisto", "-v", "ON_ERROR_STOP=1", "-c", $sql)
}

Set-Location $Root
Ensure-EnvFile
Update-LegacyMain20Ports
Sync-PostgresInitScripts
Ensure-Certs

if ($MockQwen -or $GatewayLLM) {
    $env:PROMPT_SEMANTICS_ENABLED = "true"
    $port = if ($GatewayLLM) { $GatewayLLMPort } else { 17178 }
    $env:QWEN_CLASSIFIER_URL = "http://host.docker.internal:$port/v1/classify"
    $env:QWEN_CLASSIFIER_API_KEY = ""
    if ($GatewayLLM) {
        $env:QWEN_CLASSIFIER_TIMEOUT = "15s"
        Write-Host "Enabled gateway LLM fallback for this Docker Compose run."
    } else {
        $env:QWEN_CLASSIFIER_TIMEOUT = "900ms"
        Write-Host "Enabled mock Qwen gateway fallback for this Docker Compose run."
    }
}

if ($RecreateVolumes) {
    Invoke-Checked "docker" @("compose", "down", "-v")
} else {
    Invoke-Checked "docker" @("compose", "down")
}

Invoke-Checked "docker" @("compose", "up", "-d", "--build")
Invoke-DevSchemaFixups

Write-Host ""
Write-Host "Themisto Labs is starting with standalone dev ports:"
Write-Host "  Dashboard: http://localhost:3010"
Write-Host "  Backend:   http://localhost:8643"
Write-Host "  Gateway:   https://localhost:9543"
Write-Host "  Postgres:  127.0.0.1:5434"
