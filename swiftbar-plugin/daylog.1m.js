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
// <xbar.desc>Today's daylog: entries, open todos with agent proposals to triage, and open PRs with live checks/review state.</xbar.desc>
// <xbar.dependencies>daylog</xbar.dependencies>
// <xbar.abouturl>https://github.com/drdreo/daylog</xbar.abouturl>
// <xbar.var>string(DAYLOG_PATH=""): Absolute path to the daylog CLI (empty = search PATH).</xbar.var>
// <xbar.var>string(DAYLOG_ICON="note.text"): SF Symbol for the menu bar icon (SwiftBar only).</xbar.var>
//
// <swiftbar.environment>[DAYLOG_PATH=, DAYLOG_ICON=]</swiftbar.environment>
// <swiftbar.hideRunInTerminal>true</swiftbar.hideRunInTerminal>
// <swiftbar.hideSwiftBar>true</swiftbar.hideSwiftBar>

// SwiftBar launches plugins with a bare PATH, so the shell side re-adds the
// places daylog actually gets installed (install.sh default, homebrew, go).
var EXTRA_PATH = '$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin:$HOME/go/bin'

// Menu colors as light,dark pairs (SwiftBar picks per appearance; xbar uses
// the first). "Urgent" matches the Omarchy widget's meaning: needs you now.
var DIM = '#6e6e73,#98989d'
var URGENT = '#c4321e,#ff6b5e'
// The default label color, stated outright rather than left to SwiftBar:
// a line with no action is drawn disabled-grey, and a submenu parent has no
// action of its own, so an actionable-looking row has to name its color.
var TEXT = '#1d1d1f,#f5f5f7'

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

// A closed todo takes its place in the log when it was closed, so that is
// the clock its row leads with — the filing time is the other half of the
// story, not the headline.
function logClockOf(e) {
  return clockOf(String(e.type) === 'todo' && e.done_ts ? e.done_ts : e.ts)
}

// When a closed todo was originally taken on. Carries the date once the
// todo outlived its filing day, so "filed 09:12" cannot read as this morning.
function filedOf(e) {
  if (String(e.type) !== 'todo' || !e.done_ts) return ''
  var filed = new Date(String(e.ts))
  if (isNaN(filed.getTime())) return ''
  var closed = new Date(String(e.done_ts))
  if (!isNaN(closed.getTime()) && filed.toDateString() !== closed.toDateString())
    return shortDateTime(e.ts)
  return clockOf(e.ts)
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
  var parts = [logClockOf(e) + ' · ' + String(e.source) + ' · ' + String(e.type)]
  if (e.original_type) parts.push('was ' + e.original_type)
  var filed = filedOf(e)
  if (filed) parts.push('filed ' + filed)
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
  // Both moments on the row itself: the leading clock is when the todo was
  // finished, so the line still has to say when it was taken on — a todo
  // carried for three days should say so without a hover.
  var filed = filedOf(e)
  if (filed) text += '  (filed ' + filed + ')'
  if (withTime) text = logClockOf(e) + '  ' + text
  return text
}

// One open-todo row. Untriaged agent proposals are marked in place (rather
// than exiled to a second list) and get the accept/decline verdict pair;
// everything else just gets the usual lifecycle actions.
//
// A todo with nothing to choose between IS its own done button: the action
// rides on the row, so the checkbox behaves like one and the row renders
// enabled. A row that owns a submenu cannot also fire its own action — the
// click opens the submenu instead — so it names its color rather than
// sitting there in disabled grey next to its actionable neighbours.
function todoRow(e, ctx, lines, untriaged) {
  var pr = e.pr || null
  var alarming = pr !== null && String(pr.checks) === 'failing'
  var label = truncate(entryText(e, false), 70)
  if (untriaged) label = '● ' + label
  var tooltip = untriaged ? 'Awaiting triage — ' + entryTooltip(e) : entryTooltip(e)
  var color = alarming || untriaged ? URGENT : TEXT
  var hasSubmenu = untriaged || Boolean(pr && pr.url)

  if (hasSubmenu) {
    lines.push(line(label, { tooltip: tooltip, color: color }))
  } else {
    lines.push(line(label, daylogAction(ctx.bin, ['done', String(e.id)], {
      tooltip: tooltip + ' — click to close', color: color,
    })))
  }
  if (untriaged) {
    // A click is the human ruling, so the identity is stated outright rather
    // than inherited from whatever $DAYLOG_SOURCE the widget was launched with.
    lines.push(line('-- Accept', daylogAction(ctx.bin, ['accept', String(e.id), '--source', 'human:widget'], { sfimage: 'tray.and.arrow.down' })))
    lines.push(line('-- Decline', daylogAction(ctx.bin, ['decline', String(e.id), '--source', 'human:widget'], { sfimage: 'xmark' })))
    lines.push(line('-----'))
  }
  if (hasSubmenu) {
    lines.push(line('-- Mark done', daylogAction(ctx.bin, ['done', String(e.id)], { sfimage: 'checkmark' })))
  }
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
  // open_todos already holds every open todo; needs_triage filters it.
  var open = (day.open_todos || []).length
  if (open > 0) parts.push(open + ' open todo' + (open === 1 ? '' : 's'))
  return parts.join(' · ')
}

function render(day, ctx) {
  var entries = day && day.entries ? day.entries : []
  var openTodos = day && day.open_todos ? day.open_todos : []
  var needsTriage = day && day.needs_triage ? day.needs_triage : []
  var prs = day && day.prs ? day.prs : []

  // needs_triage is a filter over open_todos, not a separate list — look up
  // by id when rendering rather than drawing the same todo twice.
  var untriaged = {}
  for (var t = 0; t < needsTriage.length; t++) untriaged[String(needsTriage[t].id)] = true

  var failingPRs = prs.filter(function (pr) { return String(pr.checks) === 'failing' }).length
  var attention = needsTriage.length + failingPRs

  var fetchedMs = day && day.prs_fetched_at ? new Date(String(day.prs_fetched_at)).getTime() : NaN
  var prsStale = isFinite(fetchedMs) && ctx.nowMs - fetchedMs > 2 * 3600 * 1000

  var lines = []

  // ---------- menu bar title: lights up when something needs you ----------
  // The day's note: calm normally, red with a count when something needs you
  // (untriaged inbox, failing checks). A warning triangle means the widget
  // itself couldn't load the day.
  var iconName = ctx.icon || 'note.text'
  if (ctx.error && day === null) {
    lines.push(line(':exclamationmark.triangle:', { sfcolor: 'orange' }))
  } else if (attention > 0) {
    lines.push(line(':' + iconName + ': ' + attention, { sfcolor: 'red' }))
  } else {
    lines.push(line(':' + iconName + ':'))
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

  // ---------- open todos: one list, untriaged proposals marked ----------
  if (openTodos.length > 0) {
    lines.push('---')
    var heading = 'OPEN TODOS'
    if (needsTriage.length > 0) heading += ' (' + needsTriage.length + ' awaiting triage)'
    lines.push(line(heading, { color: needsTriage.length > 0 ? URGENT : DIM, size: 11 }))
    for (var j = 0; j < openTodos.length; j++) {
      todoRow(openTodos[j], ctx, lines, untriaged[String(openTodos[j].id)] === true)
    }
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
  var ctx = { bin: findDaylog(), icon: envVar('DAYLOG_ICON'), error: '', nowMs: Date.now() }
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
