# --- tq (managed by Tentaqles; set TQ_ENABLED=0 to fall back to the legacy launcher below) ---
if ($env:TQ_ENABLED -eq '0') {
    # --- Tentaqles multi-identity launcher (managed; backup: *.bak-pre-dbi) ---
    $ClaudeExe = "$env:USERPROFILE\.local\bin\claude.exe"

    function Get-ClientContext {
        $p = (Get-Location).Path
        if ($p -like 'C:\repos\dirtybird*') { return 'dirtybird' }
        if ($p -like 'C:\repos\yduqs*')     { return 'yduqs' }
        if ($p -like 'C:\repos\uplabs*')    { return 'uplabs' }
        return 'personal'
    }

    function Set-IdentityEnv {
        switch (Get-ClientContext) {
            'dirtybird' {
                $env:AZURE_CONFIG_DIR = "$env:USERPROFILE\.cli-identities\az-ppu"
                $env:GH_CONFIG_DIR    = "$env:USERPROFILE\.cli-identities\gh-dirtybird"
            }
            'yduqs' {
                $env:AZURE_CONFIG_DIR = "$env:USERPROFILE\.cli-identities\az-estruturante"
                Remove-Item Env:GH_CONFIG_DIR -ErrorAction SilentlyContinue   # yduqs uses Azure DevOps; gh blocked
            }
            'uplabs' {
                $env:GH_CONFIG_DIR = "$env:USERPROFILE\.cli-identities\gh-uplabs"
                Remove-Item Env:AZURE_CONFIG_DIR -ErrorAction SilentlyContinue
            }
            default {
                Remove-Item Env:AZURE_CONFIG_DIR -ErrorAction SilentlyContinue
                Remove-Item Env:GH_CONFIG_DIR    -ErrorAction SilentlyContinue
            }
        }
    }

    # claude: auto-routes to the work (dirtybird) account under C:\repos\dirtybird, personal elsewhere
    function claude {
        if ((Get-ClientContext) -eq 'dirtybird') { claude-dbi @args; return }
        # if ((Get-ClientContext) -eq 'uplabs') { claude-uplabs @args; return }  # enable once the uplabs Claude account is logged in
        Remove-Item Env:CLAUDE_CONFIG_DIR -ErrorAction SilentlyContinue
        & $ClaudeExe --dangerously-skip-permissions @args
    }
    # claude-dbi: force the work account from anywhere
    function claude-dbi {
        $env:CLAUDE_CONFIG_DIR = "$env:USERPROFILE\.claude-dbi"
        try { & $ClaudeExe --dangerously-skip-permissions @args }
        finally { Remove-Item Env:CLAUDE_CONFIG_DIR -ErrorAction SilentlyContinue }
    }
    # claude-uplabs: force the uplabs account from anywhere
    function claude-uplabs {
        $env:CLAUDE_CONFIG_DIR = "$env:USERPROFILE\.claude-uplabs"
        try { & $ClaudeExe --dangerously-skip-permissions @args }
        finally { Remove-Item Env:CLAUDE_CONFIG_DIR -ErrorAction SilentlyContinue }
    }
    # claude-personal: force the personal account from anywhere
    function claude-personal {
        Remove-Item Env:CLAUDE_CONFIG_DIR -ErrorAction SilentlyContinue
        & $ClaudeExe --dangerously-skip-permissions @args
    }

    # Re-evaluate identity env on every prompt (i.e. after every cd).
    # If a prompt theme (oh-my-posh/ccstatusline) later overrides `prompt`, call Set-IdentityEnv from it.
    function prompt {
        Set-IdentityEnv
        "PS $($executionContext.SessionState.Path.CurrentLocation)$('>' * ($nestedPromptLevel + 1)) "
    }
    Set-IdentityEnv
} else {
    $tqBin = Join-Path $env:LOCALAPPDATA 'tentaqlesin'
    if (($env:Path -split ';') -notcontains $tqBin) { $env:Path = "$env:Path;$tqBin" }
    if (Get-Command tq -ErrorAction SilentlyContinue) {
        tq activate powershell | Out-String | Invoke-Expression
    } else {
        Write-Warning 'tq not found: terminal identity is NOT enforced (install tq or set TQ_ENABLED=0)'
    }
    # Preserve today's launch behaviour until manifests carry claude.permission_mode (tq migrate).
    function claude { & "$env:USERPROFILE\.local\bin\claude.exe" --dangerously-skip-permissions @args }
}
