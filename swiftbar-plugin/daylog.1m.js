#!/usr/bin/osascript -l JavaScript
// Daylog — macOS menu bar widget for SwiftBar (xbar-compatible output).
//
// The macOS sibling of the Omarchy bar widget, and deliberately just as thin
// (ARCHITECTURE.md §9): it shells out to `daylog today --json` and renders the
// result as menu lines. All state, folding, and PR joining happen in the CLI —
// this file is a dumb consumer, replaceable in an afternoon. It is plain JXA
// (JavaScript for Automation), so it needs nothing beyond what macOS ships.
//
// <xbar.title>Daylog</xbar.title>
// <xbar.version>v1.0</xbar.version>
// <xbar.author>drdreo</xbar.author>
// <xbar.author.github>drdreo</xbar.author.github>
// <xbar.desc>Today's daylog: entries, open todos, the agent inbox, and open PRs with live checks/review state.</xbar.desc>
// <xbar.dependencies>daylog</xbar.dependencies>
// <xbar.abouturl>https://github.com/drdreo/daylog</xbar.abouturl>
// <xbar.var>string(DAYLOG_PATH=""): Absolute path to the daylog CLI (empty = search PATH).</xbar.var>
//
// <swiftbar.environment>[DAYLOG_PATH=]</swiftbar.environment>
// <swiftbar.hideRunInTerminal>true</swiftbar.hideRunInTerminal>
// <swiftbar.hideSwiftBar>true</swiftbar.hideSwiftBar>

// SwiftBar launches plugins with a bare PATH, so the shell side re-adds the
// places daylog actually gets installed (install.sh default, homebrew, go).
var EXTRA_PATH = '$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin:$HOME/go/bin'

// Menu colors as light,dark pairs (SwiftBar picks per appearance; xbar uses
// the first). "Urgent" matches the Omarchy widget's meaning: needs you now.
var DIM = '#6e6e73,#98989d'
var URGENT = '#c4321e,#ff6b5e'

// ---------------------------------------------------------------- rendering
//
// Everything below down to the JXA glue is pure: render(day, ctx) -> lines.
// ctx: { bin, error, nowMs }. day: parsed `daylog today --json`, or null.

// The text part of a menu line must not contain the param separator, and
// param values are double-quoted, so both strip what would break parsing.
function sanitize(text) {
  return String(text == null ? '' : text).replace(/\|/g, '¦').replace(/\s+/g, ' ').trim()
}

function quoteParam(value) {
  return '"' + String(value == null ? '' : value)
    .replace(/"/g, "'").replace(/\|/g, '¦').replace(/\s+/g, ' ') + '"'
}

function truncate(text, max) {
  text = String(text)
  return text.length <= max ? text : text.slice(0, max - 1) + '…'
}

// One menu line in xbar syntax: "text | key=value key=value".
function line(text, params) {
  var parts = [sanitize(text)]
  var keys = params ? Object.keys(params) : []
  if (keys.length > 0) {
    parts.push('|')
    for (var i = 0; i < keys.length; i++) {
      var v = params[keys[i]]
      if (v === undefined || v === null || v === '') continue
      parts.push(keys[i] + '=' + (/[\s"]/.test(String(v)) ? quoteParam(v) : String(v)))
    }
  }
  return parts.join(' ')
}

// The action params for "run daylog <args> and re-render": param1..N carry
// the arguments, so nothing is ever interpolated into a shell string.
function daylogAction(bin, args, extra) {
  var params = { bash: bin, terminal: 'false', refresh: 'true' }
  for (var i = 0; i < args.length; i++) params['param' + (i + 1)] = args[i]
  for (var k in extra) params[k] = extra[k]
  return params
}

function pad2(n) { return (n < 10 ? '0' : '') + n }

function clockOf(ts) {
  var t = new Date(String(ts || ''))
  return isNaN(t.getTime()) ? '' : pad2(t.getHours()) + ':' + pad2(t.getMinutes())
}

function shortDateTime(ts) {
  var t = new Date(String(ts || ''))
  if (isNaN(t.getTime())) return ''
  var months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']
  return months[t.getMonth()] + ' ' + t.getDate() + ' ' + pad2(t.getHours()) + ':' + pad2(t.getMinutes())
}

// agent:claude → claude, human:cli → cli, poller:gh → gh
function shortSource(source) {
  var text = String(source || '')
  var sep = text.indexOf(':')
  return sep >= 0 ? text.slice(sep + 1) : text
}

// Mirror of the CLI's markdown status text (internal/view/join.go), so the
// menu and the terminal describe a PR in the same words.
function prStatusLabel(pr) {
  if (!pr) return ''
  if (String(pr.state) !== 'open') return String(pr.state)
  var parts = []
  if (pr.draft === true) parts.push('draft')
  if (String(pr.checks || 'none') !== 'none') parts.push('checks ' + pr.checks)
  var review = String(pr.review || 'none')
  if (review === 'approved' || review === 'review_required' || review === 'changes_requested')
    parts.push(review.replace(/_/g, ' '))
  return parts.length > 0 ? parts.join(' · ') : 'open'
}

function entryTooltip(e) {
  if (!e) return ''
  var parts = [clockOf(e.ts) + ' · ' + String(e.source) + ' · ' + String(e.type)]
  if (e.original_type) parts.push('was ' + e.original_type)
  if (e.refs && e.refs.length > 0) parts.push(e.refs.join(', '))
  if (e.done_note) parts.push('closed: ' + e.done_note)
  parts.push(String(e.tldr))
  return parts.join(' — ')
}

// Checkboxes only on todos, matching the markdown renderer.
function entryGlyph(e) {
  if (String(e.type) !== 'todo') return ''
  return e.done === true ? '☑ ' : '☐ '
}

function entryText(e, withTime) {
  var text = entryGlyph(e) + String(e.tldr)
  if (e.pr) text += '  [' + prStatusLabel(e.pr) + ']'
  if (withTime) text = clockOf(e.ts) + '  ' + text
  return text
}

// A row for an inbox/open-todo entry: the submenu carries the two actions the
// Omarchy widget binds to keys — mark done, and open the referenced PR.
function todoRow(e, ctx, lines) {
  var pr = e.pr || null
  var alarming = pr !== null && String(pr.checks) === 'failing'
  lines.push(line(truncate(entryText(e, false), 70), {
    tooltip: entryTooltip(e),
    color: alarming ? URGENT : undefined,
  }))
  lines.push(line('-- ✓ Mark done', daylogAction(ctx.bin, ['done', String(e.id)], { sfimage: 'checkmark' })))
  if (pr && pr.url) {
    lines.push(line('-- Open ' + pr.repo + '#' + pr.number + ' — ' + prStatusLabel(pr), {
      href: String(pr.url), sfimage: 'arrow.up.right.square',
    }))
  }
}

function heroMeta(day) {
  var parts = [String(day.date || '')]
  var entries = day.entries || []
  parts.push(entries.length + (entries.length === 1 ? ' entry' : ' entries'))
  var open = (day.open_todos || []).length + (day.agent_inbox || []).length
  if (open > 0) parts.push(open + ' open todo' + (open === 1 ? '' : 's'))
  return parts.join(' · ')
}

function render(day, ctx) {
  var entries = day && day.entries ? day.entries : []
  var openTodos = day && day.open_todos ? day.open_todos : []
  var agentInbox = day && day.agent_inbox ? day.agent_inbox : []
  var prs = day && day.prs ? day.prs : []

  var failingPRs = prs.filter(function (pr) { return String(pr.checks) === 'failing' }).length
  var attention = agentInbox.length + failingPRs

  var fetchedMs = day && day.prs_fetched_at ? new Date(String(day.prs_fetched_at)).getTime() : NaN
  var prsStale = isFinite(fetchedMs) && ctx.nowMs - fetchedMs > 2 * 3600 * 1000

  var lines = []

  // ---------- menu bar title: lights up when something needs you ----------
  if (ctx.error && day === null) {
    lines.push(line(':calendar.badge.exclamationmark:', { sfcolor: 'orange' }))
  } else if (attention > 0) {
    lines.push(line(':calendar.badge.exclamationmark: ' + attention, { sfcolor: 'red' }))
  } else {
    lines.push(line(':calendar:'))
  }
  lines.push('---')

  // ---------- header ----------
  lines.push(line('Daylog — ' + (day ? heroMeta(day) : 'no data yet'), { size: 12 }))

  if (ctx.error) {
    lines.push('---')
    lines.push(line(truncate(ctx.error, 90), { color: URGENT, tooltip: ctx.error }))
    lines.push(line('Install: https://github.com/drdreo/daylog', {
      href: 'https://github.com/drdreo/daylog', color: DIM, size: 11,
    }))
  }

  // ---------- agent inbox: proposals awaiting triage ----------
  if (agentInbox.length > 0) {
    lines.push('---')
    lines.push(line('AGENT INBOX (' + agentInbox.length + ' to triage)', { color: URGENT, size: 11 }))
    for (var i = 0; i < agentInbox.length; i++) todoRow(agentInbox[i], ctx, lines)
  }

  // ---------- open todos ----------
  if (openTodos.length > 0) {
    lines.push('---')
    lines.push(line('OPEN TODOS', { color: DIM, size: 11 }))
    for (var j = 0; j < openTodos.length; j++) todoRow(openTodos[j], ctx, lines)
  }

  // ---------- today's entries ----------
  if (entries.length > 0) {
    lines.push('---')
    lines.push(line('TODAY', { color: DIM, size: 11 }))
    for (var k = 0; k < entries.length; k++) {
      var e = entries[k]
      var pr = e.pr || null
      var alarming = pr !== null && String(pr.checks) === 'failing'
      var done = e.done === true
      lines.push(line(truncate(entryText(e, true), 78) + '  — ' + shortSource(e.source), {
        tooltip: entryTooltip(e),
        color: alarming ? URGENT : (done ? DIM : undefined),
        href: pr && pr.url ? String(pr.url) : undefined,
      }))
    }
  } else if (day !== null) {
    lines.push('---')
    lines.push(line('Nothing logged yet today.', { color: DIM }))
  }

  // ---------- open PRs (snapshot join) ----------
  if (prs.length > 0) {
    lines.push('---')
    var header = prsStale
      ? 'OPEN PRS (STALE — fetched ' + shortDateTime(day.prs_fetched_at) + ')'
      : 'OPEN PRS'
    lines.push(line(header, { color: prsStale ? URGENT : DIM, size: 11 }))
    for (var m = 0; m < prs.length; m++) {
      var p = prs[m]
      var bad = String(p.checks) === 'failing' || String(p.review) === 'changes_requested'
      lines.push(line(truncate(p.repo + '#' + p.number + '  ' + p.title, 64) + ' — ' + prStatusLabel(p), {
        href: String(p.url || ''),
        color: bad ? URGENT : undefined,
        tooltip: p.title + ' — ' + p.url,
      }))
    }
  }

  // ---------- actions ----------
  lines.push('---')
  lines.push(line('Refresh', { refresh: 'true', sfimage: 'arrow.clockwise' }))
  if (ctx.bin) {
    lines.push(line('Poll GitHub', daylogAction(ctx.bin, ['poll', 'gh'], { sfimage: 'arrow.triangle.2.circlepath' })))
  }
  return lines
}

// ---------------------------------------------------------------- JXA glue

function jxaApp() {
  if (!jxaApp._app) {
    var a = Application.currentApplication()
    a.includeStandardAdditions = true
    jxaApp._app = a
  }
  return jxaApp._app
}

function sh(cmd) {
  return jxaApp().doShellScript('export PATH="' + EXTRA_PATH + ':$PATH"; ' + cmd)
}

function envVar(name) {
  ObjC.import('stdlib')
  try { return ObjC.unwrap($.getenv(name)) || '' } catch (e) { return '' }
}

function shellQuote(s) {
  return "'" + String(s).replace(/'/g, "'\\''") + "'"
}

function findDaylog() {
  var override = envVar('DAYLOG_PATH')
  if (override) return override
  try { return String(sh('command -v daylog')).trim() } catch (e) { return '' }
}

function run() {
  var ctx = { bin: findDaylog(), error: '', nowMs: Date.now() }
  var day = null
  if (!ctx.bin) {
    ctx.error = 'daylog CLI not found — install it (install.sh) or set DAYLOG_PATH in this plugin’s settings'
  } else {
    try {
      day = JSON.parse(sh(shellQuote(ctx.bin) + ' today --json'))
    } catch (e) {
      ctx.error = '`daylog today --json` failed: ' + String(e.message || e)
    }
  }
  return render(day, ctx).join('\n')
}
