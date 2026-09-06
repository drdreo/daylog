# Athena on Windows

Build the matching binary (`go build -o daylog.exe .`) and put it at a stable absolute path. Do not point it at old daylog data.

```powershell
$daylog = 'C:\Tools\daylog.exe'
$data = "$env:LOCALAPPDATA\daylog-v2"
& $daylog --data-dir $data init
& $daylog --data-dir $data setup --pi 'C:\Program Files\nodejs\node.exe' `
  --pi-arg 'C:\path\to\pi-coding-agent\dist\cli.js' `
  --pi-agent-dir "$env:USERPROFILE\.pi\agent" `
  --approve-project 'C:\Projects\my-project'
& $daylog --data-dir $data doctor --check-model
```

Use the actual installed Node executable and pi CLI script paths above. `runner.binary` is directly executable; `runner.arguments` stores fixed argv prefixes for launchers such as Node. This avoids relying on a `.cmd` npm shim, shell-constructed commands, or an interactive PATH. Missing/incompatible launchers are explicit queue/doctor errors, never a direct-publication fallback.

For local native recovery, explicitly add narrow `--capture-scope harness=C:\native\directory` settings alongside approved projects. `--install-adapters` preserves unrelated hooks and leaves harness trust review to you. Configure working agent `DAYLOG_SOURCE` environments separately.

Generate/inspect/register the Task Scheduler XML:

```powershell
& $daylog --data-dir $data setup --schedule
Get-Content "$data\daylog-task.xml"
# Explicit activation only:
& $daylog --data-dir $data setup --schedule --activate
schtasks /Query /TN DaylogAthena
```

The task invokes `tick` every minute with the explicit binary/data directory; concurrent instances are ignored and the worker additionally uses an OS lock. For removal, end/delete the task first, then uninstall owned resources:

```powershell
schtasks /End /TN DaylogAthena
schtasks /Delete /TN DaylogAthena
& $daylog --data-dir $data setup --uninstall-resources
```

Set `DAYLOG_PATH` and `DAYLOG_DIR` in the tray widget's per-user environment; see [tray instructions](../windows-plugin/README.md). Separate PR polling remains optional.

Private files/directories use a protected DACL granting current-user and SYSTEM access. Atomic replacement uses `MoveFileEx` with write-through; worker/store locks use the Windows OS locking backend. No stale PID lockfile is used.

**Not runtime-validated on Windows:** DACL enforcement, power-loss durability, scheduled task registration, actual pi executable/auth behavior, and WinForms/PowerShell rendering. Cross-compilation only proves the Go sources compile. Validate these on a scratch Windows account/store before live use; no backward-compatible or less-private fallback is provided.
