# Copilot Hooks Validation Spike
# ================================
# This script validates GitHub Copilot hooks (Preview, Feb 2026) on a real machine.
#
# What it does:
#   - Reads stdin JSON from Copilot hooks framework
#   - Logs the full payload to copilot-spike.log for analysis
#   - For PreToolUse: optionally denies tool execution (set $DenyBash=$true below)
#   - For UserPromptSubmit: logs only (output is ignored by Copilot)
#
# Setup instructions:
#   1. Copy copilot-spike-hooks.json into .github/hooks/ in a test repo
#      (VS Code loads all *.json files from that directory.)
#   2. Alternatively, add the hooks directory to VS Code settings:
#        "chat.hookFilesLocations": {
#            "scripts": true
#        }
#      (chat.hookFilesLocations is an object mapping paths to booleans, NOT an array.)
#   3. Open the test repo in VS Code with GitHub Copilot extension
#   4. Submit a prompt in Copilot Chat and check %LOCALAPPDATA%\Themisto\logs\copilot-spike.log
#
# Questions this spike answers:
#   Q1: Does chat.hookFilesLocations load hooks from arbitrary directories?
#   Q2: Does UserPromptSubmit fire for Copilot Chat prompts?
#   Q3: Does PreToolUse fire for coding agent tool calls? What tool_name values appear?
#   Q4: What is the exact stdin JSON format?
#   Q5: Does hookSpecificOutput with permissionDecision=deny actually block tool execution?
#   Q6: What happens if the hook script is slow (>15s)?
#
# VS Code stdin format (observed on Windows, Mar 26 2026):
#   Common: { timestamp, cwd, session_id, hook_event_name, transcript_path }
#   PreToolUse adds: tool_name, tool_input (object), tool_use_id
#   UserPromptSubmit adds: prompt
#
# VS Code PreToolUse deny output (documented):
#   {
#     "hookSpecificOutput": {
#       "hookEventName": "PreToolUse",
#       "permissionDecision": "deny",
#       "permissionDecisionReason": "reason text"
#     }
#   }
#
# Copilot CLI stdin format (documented):
#   Common: { timestamp (unix ms), cwd }
#   preToolUse adds: toolName, toolArgs (stringified JSON)
#   Deny output is flat: { "permissionDecision": "deny", "permissionDecisionReason": "..." }

# --- Configuration ---
$DenyBash = $true  # Set to $true to test blocking bash/terminal tool calls

# --- Log setup ---
$LogDir = Join-Path $env:LOCALAPPDATA 'Themisto\logs'
if (-not (Test-Path $LogDir)) { New-Item -ItemType Directory -Path $LogDir -Force | Out-Null }
$LogPath = Join-Path $LogDir 'copilot-spike.log'

function Write-SpikeLog($msg) {
    try {
        $ts = (Get-Date).ToString('yyyy-MM-dd HH:mm:ss.fff')
        [IO.File]::AppendAllText($LogPath, "$ts  $msg`r`n")
    } catch { }
}

# Rotate log if > 1MB
try {
    if ((Test-Path $LogPath) -and (Get-Item $LogPath).Length -gt 1048576) {
        $tail = Get-Content $LogPath -Tail 200
        [IO.File]::WriteAllText($LogPath, ($tail -join "`r`n"))
    }
} catch { }

Write-SpikeLog "=== COPILOT HOOK SPIKE INVOKED ==="
Write-SpikeLog "PID=$PID  Args=$($args -join ' ')"

# --- Read stdin ---
try {
    $stdinStream = [Console]::OpenStandardInput()
    $reader = New-Object System.IO.StreamReader($stdinStream, [System.Text.Encoding]::UTF8)
    $readTask = $reader.ReadToEndAsync()
    if (-not $readTask.Wait(5000)) {
        Write-SpikeLog "stdin read timed out after 5s"
        $reader.Dispose()
        exit 0
    }
    $inputJson = $readTask.Result
    $reader.Dispose()
} catch {
    Write-SpikeLog "stdin read error: $($_.Exception.Message)"
    exit 0
}

$stdinLen = if ($inputJson) { $inputJson.Length } else { 0 }
Write-SpikeLog "stdin bytes=$stdinLen"

if (-not $inputJson) {
    Write-SpikeLog "empty stdin -- exit 0"
    exit 0
}

# Log the raw JSON for analysis
Write-SpikeLog "RAW STDIN:"
Write-SpikeLog $inputJson

# --- Parse payload ---
try {
    $payload = $inputJson | ConvertFrom-Json
} catch {
    Write-SpikeLog "JSON parse error: $($_.Exception.Message)"
    exit 0
}

# Log all top-level keys
$keys = ($payload | Get-Member -MemberType NoteProperty).Name -join ', '
Write-SpikeLog "Top-level keys: $keys"

# Detect hook type. VS Code currently sends snake_case hook_event_name;
# older docs/examples may still refer to hookEventName/sessionId.
$hookName = $null
if ($payload.hook_event_name) { $hookName = [string]$payload.hook_event_name }
elseif ($payload.hookEventName) { $hookName = [string]$payload.hookEventName }

Write-SpikeLog "hook name: $hookName"

# --- Handle UserPromptSubmit (VS Code) ---
# Per docs: output is ignored, cannot block. We just log.
if ($hookName -eq 'UserPromptSubmit') {
    Write-SpikeLog "EVENT: UserPromptSubmit"

    if ($payload.prompt) {
        $promptText = [string]$payload.prompt
        $truncated = if ($promptText.Length -gt 200) { $promptText.Substring(0, 200) + '...' } else { $promptText }
        Write-SpikeLog "Prompt text ($($promptText.Length) chars): $truncated"
    } else {
        Write-SpikeLog "No 'prompt' field found -- logging all keys for analysis"
    }

    if ($payload.session_id) { Write-SpikeLog "session_id: $($payload.session_id)" }
    elseif ($payload.sessionId) { Write-SpikeLog "sessionId: $($payload.sessionId)" }
    if ($payload.cwd) { Write-SpikeLog "cwd: $($payload.cwd)" }
    if ($payload.transcript_path) { Write-SpikeLog "transcript_path: $($payload.transcript_path)" }

    Write-SpikeLog "UserPromptSubmit: no output (audit only per docs)"
    exit 0
}

# --- Handle PreToolUse (VS Code) ---
if ($hookName -eq 'PreToolUse') {
    Write-SpikeLog "EVENT: PreToolUse"

    # VS Code uses snake_case: tool_name, tool_input, tool_use_id
    $toolName = $null
    if ($payload.tool_name) { $toolName = [string]$payload.tool_name }
    Write-SpikeLog "tool_name: $toolName"

    if ($payload.tool_use_id) { Write-SpikeLog "tool_use_id: $($payload.tool_use_id)" }
    if ($payload.session_id) { Write-SpikeLog "session_id: $($payload.session_id)" }
    elseif ($payload.sessionId) { Write-SpikeLog "sessionId: $($payload.sessionId)" }

    # Log tool input if present (tool_input is an object in VS Code format)
    if ($payload.tool_input) {
        $inputStr = $payload.tool_input | ConvertTo-Json -Depth 3 -Compress
        $truncated = if ($inputStr.Length -gt 500) { $inputStr.Substring(0, 500) + '...' } else { $inputStr }
        Write-SpikeLog "tool_input: $truncated"
    }

    # Test deny for bash/terminal tool
    if ($DenyBash -and ($toolName -eq 'bash' -or $toolName -eq 'terminal' -or $toolName -eq 'runCommand' -or $toolName -eq 'run_in_terminal')) {
        Write-SpikeLog "DENYING tool: $toolName (VS Code hookSpecificOutput format)"
        # VS Code documented deny format: wrapped in hookSpecificOutput
        $denyResponse = @{
            hookSpecificOutput = @{
                hookEventName = "PreToolUse"
                permissionDecision = "deny"
                permissionDecisionReason = "Themisto spike test: blocking $toolName tool execution"
            }
        } | ConvertTo-Json -Depth 3 -Compress
        Write-SpikeLog "Deny response: $denyResponse"
        Write-Output $denyResponse
        exit 0
    }

    Write-SpikeLog "ALLOWING tool: $toolName (no output = allow)"
    exit 0
}

# --- Fallback: Copilot CLI format (no hookEventName) ---
# CLI uses toolName (camelCase) and toolArgs (stringified JSON)
if ($payload.toolName) {
    $toolName = [string]$payload.toolName
    Write-SpikeLog "EVENT: preToolUse (CLI format)"
    Write-SpikeLog "toolName: $toolName"

    if ($payload.toolArgs) { Write-SpikeLog "toolArgs: $($payload.toolArgs)" }

    if ($DenyBash -and ($toolName -eq 'bash' -or $toolName -eq 'terminal' -or $toolName -eq 'runCommand' -or $toolName -eq 'run_in_terminal')) {
        Write-SpikeLog "DENYING tool: $toolName (CLI flat format)"
        # CLI documented deny format: flat JSON on stdout
        $denyResponse = @{
            permissionDecision = "deny"
            permissionDecisionReason = "Themisto spike test: blocking $toolName tool execution"
        } | ConvertTo-Json -Compress
        Write-SpikeLog "Deny response: $denyResponse"
        Write-Output $denyResponse
        exit 0
    }

    Write-SpikeLog "ALLOWING tool: $toolName"
    exit 0
}

# CLI UserPromptSubmit fallback (prompt field without hookEventName)
if ($payload.prompt -and -not $hookName) {
    Write-SpikeLog "EVENT: userPromptSubmit (CLI format, inferred from prompt field)"
    $promptText = [string]$payload.prompt
    $truncated = if ($promptText.Length -gt 200) { $promptText.Substring(0, 200) + '...' } else { $promptText }
    Write-SpikeLog "Prompt text ($($promptText.Length) chars): $truncated"
    Write-SpikeLog "userPromptSubmit: no output (audit only per docs)"
    exit 0
}

# --- Unknown hook type ---
Write-SpikeLog "UNKNOWN hook type. hook_name='$hookName'. Full payload logged above for analysis."
exit 0
