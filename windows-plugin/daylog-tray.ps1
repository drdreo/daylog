# Daylog — Windows 11 system tray widget.
#
# The Windows sibling of the Omarchy bar widget and the SwiftBar menu bar
# plugin, and deliberately just as thin (ARCHITECTURE.md §9): it shells out to
# `daylog today --json` and renders the result as a tray icon plus menu. All
# state, folding, and PR joining happen in the CLI — this file is a dumb
# consumer, replaceable in an afternoon. It is plain Windows PowerShell 5.1 +
# WinForms, so it needs nothing beyond what Windows ships.
#
# Run it hidden (install.ps1 sets up a Startup shortcut that does):
#   powershell -NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File daylog-tray.ps1
#
# Settings (environment variables):
#   DAYLOG_PATH          Absolute path to the daylog CLI (empty = search PATH)
#   DAYLOG_REFRESH_SEC   How often to re-run `daylog today --json` (default 60)

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
Add-Type -Namespace Daylog -Name Native -MemberDefinition @'
[DllImport("user32.dll", SetLastError = true)]
public static extern bool DestroyIcon(IntPtr handle);
'@

# One instance is plenty; a second launch (Startup shortcut + manual run) exits.
$script:Mutex = New-Object System.Threading.Mutex($false, 'Local\daylog-tray-widget')
if (-not $script:Mutex.WaitOne(0, $false)) { exit 0 }

# Menu colors, same meaning as the SwiftBar plugin (WinForms menus are always
# light, so these are the light-appearance values). "Urgent" = needs you now.
$script:Dim    = [System.Drawing.Color]::FromArgb(0x6e, 0x6e, 0x73)
$script:Urgent = [System.Drawing.Color]::FromArgb(0xc4, 0x32, 0x1e)

# ------------------------------------------------------------------- helpers

function Find-Daylog {
    if ($env:DAYLOG_PATH) { return $env:DAYLOG_PATH }
    $cmd = Get-Command daylog -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    # The places daylog actually gets installed when PATH doesn't have it yet.
    $candidates = @(
        (Join-Path $env:LOCALAPPDATA 'Programs\daylog\daylog.exe'),
        (Join-Path $env:USERPROFILE 'go\bin\daylog.exe')
    )
    foreach ($p in $candidates) { if (Test-Path $p) { return $p } }
    return ''
}

# Run daylog without flashing a console window (this script runs hidden).
function Invoke-Daylog {
    param([string[]]$ArgList)
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $script:Bin
    $psi.Arguments = ($ArgList | ForEach-Object { '"' + ($_ -replace '"', '\"') + '"' }) -join ' '
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.StandardOutputEncoding = [System.Text.Encoding]::UTF8
    $psi.StandardErrorEncoding = [System.Text.Encoding]::UTF8
    $proc = [System.Diagnostics.Process]::Start($psi)
    $errTask = $proc.StandardError.ReadToEndAsync()
    $out = $proc.StandardOutput.ReadToEnd()
    $proc.WaitForExit()
    if ($proc.ExitCode -ne 0) {
        throw "daylog exited $($proc.ExitCode): $($errTask.Result.Trim())"
    }
    return $out
}

function Truncate([string]$Text, [int]$Max) {
    if ($Text.Length -le $Max) { return $Text }
    return $Text.Substring(0, $Max - 1) + '…'
}

function Get-Clock($Ts) {
    try { return ([System.DateTimeOffset]"$Ts").LocalDateTime.ToString('HH:mm') } catch { return '' }
}

function Get-ShortDateTime($Ts) {
    try { return ([System.DateTimeOffset]"$Ts").LocalDateTime.ToString('MMM d HH:mm') } catch { return '' }
}

# agent:claude → claude, human:cli → cli, poller:gh → gh
function Get-ShortSource($Source) {
    $text = "$Source"
    $sep = $text.IndexOf(':')
    if ($sep -ge 0) { return $text.Substring($sep + 1) }
    return $text
}

# Mirror of the CLI's markdown status text (internal/view/join.go), so the
# menu and the terminal describe a PR in the same words.
function Get-PrStatusLabel($Pr) {
    if (-not $Pr) { return '' }
    if ("$($Pr.state)" -ne 'open') { return "$($Pr.state)" }
    $parts = @()
    if ($Pr.draft -eq $true) { $parts += 'draft' }
    $checks = "$($Pr.checks)"
    if ($checks -and $checks -ne 'none') { $parts += "checks $checks" }
    $review = "$($Pr.review)"
    if ($review -in @('approved', 'review_required', 'changes_requested')) {
        $parts += ($review -replace '_', ' ')
    }
    if ($parts.Count -gt 0) { return $parts -join ' · ' }
    return 'open'
}

function Get-EntryTooltip($E) {
    if (-not $E) { return '' }
    $parts = @((Get-Clock $E.ts) + ' · ' + "$($E.source)" + ' · ' + "$($E.type)")
    if ($E.original_type) { $parts += "was $($E.original_type)" }
    if ($E.refs -and @($E.refs).Count -gt 0) { $parts += (@($E.refs) -join ', ') }
    if ($E.done_note) { $parts += "closed: $($E.done_note)" }
    $parts += "$($E.tldr)"
    return $parts -join ' — '
}

# Checkboxes only on todos, matching the markdown renderer.
function Get-EntryGlyph($E) {
    if ("$($E.type)" -ne 'todo') { return '' }
    if ($E.done -eq $true) { return '☑ ' }
    return '☐ '
}

function Get-EntryText($E, [bool]$WithTime) {
    $text = (Get-EntryGlyph $E) + "$($E.tldr)"
    if ($E.pr) { $text += '  [' + (Get-PrStatusLabel $E.pr) + ']' }
    if ($WithTime) { $text = (Get-Clock $E.ts) + '  ' + $text }
    return $text
}

function Get-HeroMeta($Day) {
    $entries = if ($Day.entries) { @($Day.entries) } else { @() }
    $parts = @("$($Day.date)")
    if ($entries.Count -eq 1) { $parts += '1 entry' } else { $parts += "$($entries.Count) entries" }
    $open = 0
    # open_todos already holds every open todo; needs_triage filters it.
    if ($Day.open_todos) { $open += @($Day.open_todos).Count }
    if ($open -eq 1) { $parts += '1 open todo' } elseif ($open -gt 1) { $parts += "$open open todos" }
    return $parts -join ' · '
}

# ----------------------------------------------------------------- tray icon
#
# The day's note: theme-colored normally, a red badge with a count when
# something needs you (untriaged inbox, failing checks). An orange warning
# triangle means the widget itself couldn't load the day.

function Get-UsesLightTheme {
    try {
        $v = Get-ItemProperty -Path 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize' `
            -Name SystemUsesLightTheme -ErrorAction Stop
        return $v.SystemUsesLightTheme -eq 1
    } catch { return $false }
}

function Set-TrayIcon {
    param([string]$State, [int]$Count)
    $bmp = New-Object System.Drawing.Bitmap 32, 32
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
    $sf = New-Object System.Drawing.StringFormat
    $sf.Alignment = [System.Drawing.StringAlignment]::Center
    $sf.LineAlignment = [System.Drawing.StringAlignment]::Center
    $white = [System.Drawing.Brushes]::White
    $box = New-Object System.Drawing.RectangleF 0, 0, 32, 32
    try {
        if ($State -eq 'error') {
            $orange = New-Object System.Drawing.SolidBrush ([System.Drawing.Color]::FromArgb(0xe6, 0x7e, 0x22))
            $pts = @(
                (New-Object System.Drawing.PointF 16, 2),
                (New-Object System.Drawing.PointF 31, 29),
                (New-Object System.Drawing.PointF 1, 29)
            )
            $g.FillPolygon($orange, $pts)
            $font = New-Object System.Drawing.Font('Segoe UI', 13, [System.Drawing.FontStyle]::Bold)
            $g.DrawString('!', $font, $white, (New-Object System.Drawing.RectangleF 0, 6, 32, 26), $sf)
            $font.Dispose(); $orange.Dispose()
        } elseif ($State -eq 'attention') {
            $red = New-Object System.Drawing.SolidBrush ([System.Drawing.Color]::FromArgb(0xd6, 0x3a, 0x26))
            $g.FillEllipse($red, 1, 1, 30, 30)
            $text = if ($Count -gt 9) { '9+' } else { "$Count" }
            $size = if ($Count -gt 9) { 12 } else { 15 }
            $font = New-Object System.Drawing.Font('Segoe UI', $size, [System.Drawing.FontStyle]::Bold)
            $g.DrawString($text, $font, $white, $box, $sf)
            $font.Dispose(); $red.Dispose()
        } else {
            # A note page with three lines — the tray's note.text.
            $fg = if (Get-UsesLightTheme) { [System.Drawing.Color]::FromArgb(0x3c, 0x3c, 0x40) }
                  else { [System.Drawing.Color]::White }
            $pen = New-Object System.Drawing.Pen $fg, 2.4
            $pen.StartCap = [System.Drawing.Drawing2D.LineCap]::Round
            $pen.EndCap = [System.Drawing.Drawing2D.LineCap]::Round
            $g.DrawRectangle($pen, 9, 3, 14, 26)
            $g.DrawLine($pen, 13, 11, 19, 11)
            $g.DrawLine($pen, 13, 16, 19, 16)
            $g.DrawLine($pen, 13, 21, 17, 21)
            $pen.Dispose()
        }
    } finally {
        $sf.Dispose()
        $g.Dispose()
    }
    $handle = $bmp.GetHicon()
    $script:Notify.Icon = [System.Drawing.Icon]::FromHandle($handle)
    if ($script:IconHandle -ne [IntPtr]::Zero) {
        [void][Daylog.Native]::DestroyIcon($script:IconHandle)
    }
    $script:IconHandle = $handle
    $bmp.Dispose()
}

# --------------------------------------------------------------------- menu
#
# One flat menu mirroring the SwiftBar plugin's sections: header, open todos,
# open todos, today, open PRs, actions. Rows that reference a PR open it on
# click; todo rows carry a submenu with triage verdicts, Mark done + Open PR.

# Shared handlers: per-row data travels in the item's Tag, never in a closure.
$script:OpenUrlHandler = { param($s, $e) if ($s.Tag) { Start-Process $s.Tag } }
$script:MarkDoneHandler = {
    param($s, $e)
    try { [void](Invoke-Daylog @('done', "$($s.Tag)")) }
    catch { $script:Notify.ShowBalloonTip(4000, 'Daylog', "$($_.Exception.Message)", 'Error') }
    Update-Widget
}
# Triage verdicts on an agent proposal: accept adopts it, decline drops it
# from every view. Same single write path as everything else. A click is the
# human ruling, so the identity is stated outright rather than inherited from
# whatever $DAYLOG_SOURCE the tray was launched with.
$script:AcceptHandler = {
    param($s, $e)
    try { [void](Invoke-Daylog @('accept', "$($s.Tag)", '--source', 'human:widget')) }
    catch { $script:Notify.ShowBalloonTip(4000, 'Daylog', "$($_.Exception.Message)", 'Error') }
    Update-Widget
}
$script:DeclineHandler = {
    param($s, $e)
    try { [void](Invoke-Daylog @('decline', "$($s.Tag)", '--source', 'human:widget')) }
    catch { $script:Notify.ShowBalloonTip(4000, 'Daylog', "$($_.Exception.Message)", 'Error') }
    Update-Widget
}
$script:RefreshHandler = { param($s, $e) Update-Widget }
$script:PollHandler = {
    param($s, $e)
    try { [void](Invoke-Daylog @('poll', 'gh')) }
    catch { $script:Notify.ShowBalloonTip(4000, 'Daylog', "$($_.Exception.Message)", 'Error') }
    Update-Widget
}
$script:ExitHandler = {
    param($s, $e)
    $script:Timer.Stop()
    $script:Notify.Visible = $false
    [System.Windows.Forms.Application]::Exit()
}

function Add-Separator($Menu) {
    [void]$Menu.Items.Add((New-Object System.Windows.Forms.ToolStripSeparator))
}

# WinForms treats & as a mnemonic prefix; double it so entry text renders as-is.
function Get-MenuText([string]$Text) { return $Text -replace '&', '&&' }

function Add-Row {
    param($Menu, [string]$Text, [string]$Tooltip = '', $Color = $null,
          [string]$Url = '', [switch]$Header)
    $it = New-Object System.Windows.Forms.ToolStripMenuItem((Get-MenuText $Text))
    if ($Tooltip) { $it.ToolTipText = $Tooltip }
    if ($Color) { $it.ForeColor = $Color }
    if ($Header) { $it.Font = $script:SmallFont }
    if ($Url) { $it.Tag = $Url; $it.Add_Click($script:OpenUrlHandler) }
    [void]$Menu.Items.Add($it)
    return $it
}

# One open-todo row. Untriaged agent proposals are marked in place (rather
# than exiled to a second list) and get the accept/decline verdict pair;
# everything else just gets the usual lifecycle actions.
function Add-TodoRow($Menu, $Entry, [bool]$Untriaged) {
    $pr = $Entry.pr
    $alarming = $pr -and ("$($pr.checks)" -eq 'failing')
    $label = Truncate (Get-EntryText $Entry $false) 70
    if ($Untriaged) { $label = "* $label" }
    $it = New-Object System.Windows.Forms.ToolStripMenuItem((Get-MenuText $label))
    $it.ToolTipText = if ($Untriaged) { 'Awaiting triage — ' + (Get-EntryTooltip $Entry) } else { Get-EntryTooltip $Entry }
    if ($alarming -or $Untriaged) { $it.ForeColor = $script:Urgent }
    if ($Untriaged) {
        $accept = New-Object System.Windows.Forms.ToolStripMenuItem('Accept')
        $accept.Tag = "$($Entry.id)"
        $accept.Add_Click($script:AcceptHandler)
        [void]$it.DropDownItems.Add($accept)
        $decline = New-Object System.Windows.Forms.ToolStripMenuItem('Decline')
        $decline.Tag = "$($Entry.id)"
        $decline.Add_Click($script:DeclineHandler)
        [void]$it.DropDownItems.Add($decline)
        [void]$it.DropDownItems.Add((New-Object System.Windows.Forms.ToolStripSeparator))
    }
    $done = New-Object System.Windows.Forms.ToolStripMenuItem('Mark done')
    $done.Tag = "$($Entry.id)"
    $done.Add_Click($script:MarkDoneHandler)
    [void]$it.DropDownItems.Add($done)
    if ($pr -and $pr.url) {
        $open = New-Object System.Windows.Forms.ToolStripMenuItem(
            (Get-MenuText ("Open $($pr.repo)#$($pr.number) — " + (Get-PrStatusLabel $pr))))
        $open.Tag = "$($pr.url)"
        $open.Add_Click($script:OpenUrlHandler)
        [void]$it.DropDownItems.Add($open)
    }
    [void]$Menu.Items.Add($it)
}

function Build-Menu {
    param($Day, [string]$ErrText)
    $m = $script:Menu
    $old = @($m.Items)
    $m.Items.Clear()
    foreach ($i in $old) { $i.Dispose() }

    $entries   = if ($Day -and $Day.entries)     { @($Day.entries) }     else { @() }
    $openTodos = if ($Day -and $Day.open_todos)  { @($Day.open_todos) }  else { @() }
    $needsTriage = if ($Day -and $Day.needs_triage) { @($Day.needs_triage) } else { @() }
    # needs_triage filters open_todos rather than partitioning it — look up by
    # id when rendering, or the same todo draws twice.
    $untriagedIds = @{}
    foreach ($t in $needsTriage) { $untriagedIds["$($t.id)"] = $true }
    $prs       = if ($Day -and $Day.prs)         { @($Day.prs) }         else { @() }

    $stale = $false
    if ($Day -and $Day.prs_fetched_at) {
        try {
            $fetched = ([System.DateTimeOffset]"$($Day.prs_fetched_at)").LocalDateTime
            $stale = ((Get-Date) - $fetched).TotalHours -gt 2
        } catch {}
    }

    # ---------- header ----------
    $meta = if ($Day) { Get-HeroMeta $Day } else { 'no data yet' }
    (Add-Row $m "Daylog — $meta" -Header).Enabled = $false

    if ($ErrText) {
        Add-Separator $m
        [void](Add-Row $m (Truncate $ErrText 90) -Tooltip $ErrText -Color $script:Urgent)
        [void](Add-Row $m 'Install: https://github.com/drdreo/daylog' -Color $script:Dim `
            -Url 'https://github.com/drdreo/daylog')
    }

    # ---------- open todos: one list, untriaged proposals marked ----------
    if ($openTodos.Count -gt 0) {
        Add-Separator $m
        $heading = if ($needsTriage.Count -gt 0) { "OPEN TODOS ($($needsTriage.Count) awaiting triage)" } else { 'OPEN TODOS' }
        $color = if ($needsTriage.Count -gt 0) { $script:Urgent } else { $null }
        (Add-Row $m $heading -Color $color -Header).Enabled = $false
        foreach ($e in $openTodos) { Add-TodoRow $m $e ($untriagedIds.ContainsKey("$($e.id)")) }
    }

    # ---------- today's entries ----------
    if ($entries.Count -gt 0) {
        Add-Separator $m
        (Add-Row $m 'TODAY' -Header).Enabled = $false
        foreach ($e in $entries) {
            $pr = $e.pr
            $alarming = $pr -and ("$($pr.checks)" -eq 'failing')
            $color = $null
            if ($alarming) { $color = $script:Urgent } elseif ($e.done -eq $true) { $color = $script:Dim }
            $url = if ($pr -and $pr.url) { "$($pr.url)" } else { '' }
            $text = (Truncate (Get-EntryText $e $true) 78) + '  — ' + (Get-ShortSource $e.source)
            [void](Add-Row $m $text -Tooltip (Get-EntryTooltip $e) -Color $color -Url $url)
        }
    } elseif ($Day) {
        Add-Separator $m
        [void](Add-Row $m 'Nothing logged yet today.' -Color $script:Dim)
    }

    # ---------- open PRs (snapshot join) ----------
    if ($prs.Count -gt 0) {
        Add-Separator $m
        $header = if ($stale) { "OPEN PRS (STALE — fetched $(Get-ShortDateTime $Day.prs_fetched_at))" } else { 'OPEN PRS' }
        $hcolor = if ($stale) { $script:Urgent } else { $null }
        (Add-Row $m $header -Color $hcolor -Header).Enabled = $false
        foreach ($p in $prs) {
            $bad = ("$($p.checks)" -eq 'failing') -or ("$($p.review)" -eq 'changes_requested')
            $color = if ($bad) { $script:Urgent } else { $null }
            $text = (Truncate "$($p.repo)#$($p.number)  $($p.title)" 64) + ' — ' + (Get-PrStatusLabel $p)
            [void](Add-Row $m $text -Tooltip "$($p.title) — $($p.url)" -Color $color -Url "$($p.url)")
        }
    }

    # ---------- actions ----------
    Add-Separator $m
    (Add-Row $m 'Refresh').Add_Click($script:RefreshHandler)
    if ($script:Bin) {
        (Add-Row $m 'Poll GitHub').Add_Click($script:PollHandler)
    }
    (Add-Row $m 'Exit').Add_Click($script:ExitHandler)
}

# ------------------------------------------------------------------- refresh

function Update-Widget {
    $script:Bin = Find-Daylog
    $day = $null
    $errText = ''
    if (-not $script:Bin) {
        $errText = 'daylog CLI not found — install it (go install github.com/drdreo/daylog@latest) or set DAYLOG_PATH'
    } else {
        try { $day = Invoke-Daylog @('today', '--json') | ConvertFrom-Json }
        catch { $errText = '`daylog today --json` failed: ' + $_.Exception.Message }
    }

    $needsTriage = if ($day -and $day.needs_triage) { @($day.needs_triage) } else { @() }
    $prs   = if ($day -and $day.prs)         { @($day.prs) }         else { @() }
    $failing = @($prs | Where-Object { "$($_.checks)" -eq 'failing' }).Count
    $attention = $needsTriage.Count + $failing

    if ($errText -and -not $day) { Set-TrayIcon 'error' 0 }
    elseif ($attention -gt 0)    { Set-TrayIcon 'attention' $attention }
    else                         { Set-TrayIcon 'ok' 0 }

    $tip = if ($day) { 'Daylog — ' + (Get-HeroMeta $day) } else { 'Daylog — ' + $errText }
    $script:Notify.Text = Truncate $tip 63

    Build-Menu $day $errText
}

# --------------------------------------------------------------------- wire-up

$script:IconHandle = [IntPtr]::Zero
$script:SmallFont = New-Object System.Drawing.Font('Segoe UI', 8)

$script:Menu = New-Object System.Windows.Forms.ContextMenuStrip
$script:Menu.ShowItemToolTips = $true

$script:Notify = New-Object System.Windows.Forms.NotifyIcon
$script:Notify.ContextMenuStrip = $script:Menu
$script:Notify.Visible = $true

# Left click opens the same menu (ShowContextMenu is non-public but stable).
$script:Notify.Add_MouseUp({
    param($s, $e)
    if ($e.Button -eq [System.Windows.Forms.MouseButtons]::Left) {
        $method = [System.Windows.Forms.NotifyIcon].GetMethod('ShowContextMenu',
            [System.Reflection.BindingFlags]'Instance,NonPublic')
        [void]$method.Invoke($script:Notify, $null)
    }
})

$refreshSec = 60
if ($env:DAYLOG_REFRESH_SEC -match '^\d+$') { $refreshSec = [int]$env:DAYLOG_REFRESH_SEC }
$script:Timer = New-Object System.Windows.Forms.Timer
$script:Timer.Interval = [Math]::Max(5, $refreshSec) * 1000
$script:Timer.Add_Tick({ Update-Widget })
$script:Timer.Start()

Update-Widget

try {
    [System.Windows.Forms.Application]::Run()
} finally {
    $script:Notify.Dispose()
    if ($script:IconHandle -ne [IntPtr]::Zero) { [void][Daylog.Native]::DestroyIcon($script:IconHandle) }
    $script:Mutex.ReleaseMutex()
}
