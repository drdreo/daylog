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
// <xbar.desc>Today's daylog: entries, open todos with agent proposals to triage, and open PRs with live checks/review state. Walk back through earlier days with the ◀/▶ rows.</xbar.desc>
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

// How long a "show me another day" choice sticks. This is a *today* widget:
// a menu bar that still reads Tuesday three hours after you went looking is
// worse than one that forgets, so the view drifts back on its own.
var VIEW_DAY_TTL_SEC = 10 * 60
// The argument that means "stop viewing another day". A word rather than an
// empty string, because a menu line drops empty param values.
var VIEW_DAY_TODAY = 'today'

var WEEKDAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']
var MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

// ---------------------------------------------------------------- rendering
//
// Everything below down to the JXA glue is pure: render(day, ctx) -> lines.
// ctx: { bin, error, nowMs, statePath }. day: parsed `daylog today [DATE]
// --json`, or null.

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
  return MONTHS[t.getMonth()] + ' ' + t.getDate() + ' ' + pad2(t.getHours()) + ':' + pad2(t.getMinutes())
}

// ------------------------------------------------------------ days as days
//
// A day is a calendar day, never an instant: it is shifted and compared by
// its date components at local midnight, so day arithmetic survives the
// clocks changing. `iso` is always the YYYY-MM-DD the CLI speaks.

function isoOf(d) {
  return d.getFullYear() + '-' + pad2(d.getMonth() + 1) + '-' + pad2(d.getDate())
}

function dayOf(iso) {
  var m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(String(iso || ''))
  return m === null ? null : new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]))
}

function shiftDay(iso, delta) {
  var d = dayOf(iso)
  return d === null ? '' : isoOf(new Date(d.getFullYear(), d.getMonth(), d.getDate() + delta))
}

// Whole calendar days from `fromISO` to `toISO` — negative for the past. An
// unparseable date yields 0, which degrades to "treat it as today" in every
// caller rather than to a broken menu.
function dayDelta(fromISO, toISO) {
  var a = dayOf(fromISO), b = dayOf(toISO)
  if (a === null || b === null) return 0
  return Math.round((b.getTime() - a.getTime()) / 86400000)
}

function calendarName(iso) {
  var d = dayOf(iso)
  return d === null ? '' : WEEKDAYS[d.getDay()] + ', ' + MONTHS[d.getMonth()] + ' ' + d.getDate()
}

// The name a human would use: Today, Yesterday, or "Wed, Aug 19".
function dayName(iso, todayISO) {
  var delta = dayDelta(todayISO, iso)
  if (delta === 0) return 'Today'
  if (delta === -1) return 'Yesterday'
  if (delta === 1) return 'Tomorrow'
  var name = calendarName(iso)
  return name === '' ? String(iso || '') : name
}

// The distance spelled out, for days whose name doesn't already say it.
function dayDistance(iso, todayISO) {
  var delta = dayDelta(todayISO, iso)
  if (delta >= -1 && delta <= 1) return ''
  return delta < 0 ? -delta + ' days ago' : 'in ' + delta + ' days'
}

// The day section's heading: the day's name, its date when the name hides it,
// and how far back it is. "TODAY · MON, AUG 24", "WED, AUG 19 · 5 DAYS AGO".
function dayHeading(iso, todayISO) {
  var name = dayName(iso, todayISO)
  var parts = [name]
  var calendar = calendarName(iso)
  if (calendar !== '' && calendar !== name) parts.push(calendar)
  var distance = dayDistance(iso, todayISO)
  if (distance !== '') parts.push(distance)
  return parts.join(' · ').toUpperCase()
}

// The empty state names the day it is empty *about*, so an untouched Tuesday
// can never be misread as a quiet morning. The ◀/▶ rows sit right above it,
// so the way out of an empty day is already on screen.
function emptyDayNote(iso, todayISO) {
  var delta = dayDelta(todayISO, iso)
  if (delta === 0) return 'Nothing logged yet today.'
  if (delta > 0) return 'Nothing logged for ' + dayName(iso, todayISO).toLowerCase() + ' yet.'
  if (delta === -1) return 'Nothing was logged yesterday.'
  return 'Nothing was logged on ' + calendarName(iso) + '.'
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

// ------------------------------------------------------------ day navigation
//
// The day section is a window onto one day, not a hardcoded "today", and the
// ◀/▶ rows slide it. They are rows and not arrow keys on purpose: an open
// macOS menu owns the arrow keys for its own row and submenu navigation, and
// a plugin never sees them. The Omarchy panel is a real focused window, so
// that is where ←/→ do this for real.
//
// Which day is being viewed lives in a file, because SwiftBar re-executes
// this plugin from scratch on every refresh — anything that has to outlive a
// click has to be on disk. The file carries the second it was written so the
// choice can expire (VIEW_DAY_TTL_SEC).

// Point the widget at `iso` (VIEW_DAY_TODAY for today) and re-render. The
// action re-runs *this plugin* with two plain arguments, which run() handles
// before it renders anything — the same discipline as daylogAction: no menu
// line ever carries a shell command. An earlier version wrote the file with
// `/bin/sh -c "printf '%s %s' …"`, and the `%s` in that param was enough to
// stop SwiftBar building the menu at all, taking the whole icon with it.
function viewDayAction(ctx, iso, extra) {
  var params = {
    bash: ctx.self, param1: 'view-day', param2: iso === '' ? VIEW_DAY_TODAY : iso,
    terminal: 'false', refresh: 'true',
  }
  for (var k in extra) params[k] = extra[k]
  return params
}

// Heading plus the rows that walk off this day. Always rendered, even for an
// empty day — a day you cannot navigate away from is a dead end. Forward
// stops at today: there is nothing to log in a day that hasn't happened.
function dayNavLines(day, ctx, lines) {
  var iso = String(day.date || '')
  var todayISO = isoOf(new Date(ctx.nowMs))
  var delta = dayDelta(todayISO, iso)
  var prev = shiftDay(iso, -1)
  var next = shiftDay(iso, 1)

  lines.push(line(dayHeading(iso, todayISO), { color: DIM, size: 11 }))
  // Without a path back to this file there is nothing to click; the heading
  // still names the day rather than leaving dead rows behind.
  if (!ctx.self) return
  if (prev !== '') {
    lines.push(line('◀  ' + dayName(prev, todayISO), viewDayAction(ctx, prev, {
      color: TEXT, sfimage: 'chevron.left', tooltip: 'Show ' + calendarName(prev),
    })))
  }
  if (next !== '' && delta < 0) {
    lines.push(line('▶  ' + dayName(next, todayISO), viewDayAction(ctx, delta === -1 ? '' : next, {
      color: TEXT, sfimage: 'chevron.right', tooltip: 'Show ' + calendarName(next),
    })))
  }
  // Redundant when ▶ already says "Today"; the long way back needs one click.
  if (delta !== 0 && delta !== -1) {
    lines.push(line('↩  Back to today', viewDayAction(ctx, '', {
      color: TEXT, sfimage: 'arrow.uturn.left', tooltip: 'Show today again',
    })))
  }
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

  // ---------- the viewed day's entries ----------
  // Only `entries` is scoped to a day: open todos are obligations that don't
  // expire at midnight and PRs are live state, so walking back through days
  // moves this section alone — and the menu bar badge keeps counting what
  // needs you *now*, whichever day you happen to be reading.
  if (day !== null) {
    lines.push('---')
    dayNavLines(day, ctx, lines)
    if (entries.length > 0) {
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
    } else {
      lines.push(line(emptyDayNote(String(day.date || ''), isoOf(new Date(ctx.nowMs))), { color: DIM }))
    }
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

// This file's own path, so a menu line can re-run it (see viewDayAction).
// SwiftBar states it outright; under xbar it comes out of the interpreter's
// argument list, which is where osascript left it.
function selfPath() {
  var declared = envVar('SWIFTBAR_PLUGIN_PATH')
  if (declared) return declared
  try {
    ObjC.import('Foundation')
    var args = ObjC.deepUnwrap($.NSProcessInfo.processInfo.arguments)
    for (var i = args.length - 1; i >= 0; i--) {
      var path = String(args[i])
      if (!/\.js$/.test(path)) continue
      // A menu action is run from who-knows-where, so the path it carries has
      // to be absolute even when this file was invoked by a relative one.
      if (path.charAt(0) !== '/') {
        path = ObjC.unwrap($.NSFileManager.defaultManager.currentDirectoryPath) + '/' + path
      }
      return ObjC.unwrap($.NSString.alloc.initWithUTF8String(path).stringByStandardizingPath)
    }
  } catch (e) { /* fall through: the nav rows simply aren't drawn */ }
  return ''
}

// $TMPDIR is exactly the right home for this: per-user, and swept by the OS,
// so a day you wandered off to never becomes a permanent setting.
function viewDayPath() {
  var dir = envVar('TMPDIR') || '/tmp'
  if (dir.charAt(dir.length - 1) !== '/') dir += '/'
  return dir + 'daylog-view-day'
}

// The day the ◀/▶ rows last pointed at, or '' for today. An expired, absent,
// or malformed file all read as today — the widget's resting state.
function readViewDay(path, nowMs) {
  var text = ''
  try {
    ObjC.import('Foundation')
    text = ObjC.unwrap($.NSString.stringWithContentsOfFileEncodingError(
      path, $.NSUTF8StringEncoding, null)) || ''
  } catch (e) { return '' }
  var m = /^(\d+)\s+(\d{4}-\d{2}-\d{2})\s*$/.exec(String(text))
  if (m === null) return ''
  if (Math.floor(nowMs / 1000) - Number(m[1]) > VIEW_DAY_TTL_SEC) return ''
  return m[2]
}

// Record the day to view, or forget it. Written here rather than by a shell
// command in the menu line: the file is this file's business, and a menu that
// carries no commands cannot mis-parse one.
function writeViewDay(path, iso, nowMs) {
  ObjC.import('Foundation')
  if (iso === VIEW_DAY_TODAY || !/^\d{4}-\d{2}-\d{2}$/.test(iso)) {
    $.NSFileManager.defaultManager.removeItemAtPathError(path, null)
    return
  }
  var body = Math.floor(nowMs / 1000) + ' ' + iso
  $.NSString.alloc.initWithUTF8String(body)
    .writeToFileAtomicallyEncodingError(path, true, $.NSUTF8StringEncoding, null)
}

function run(argv) {
  var nowMs = Date.now()
  var statePath = viewDayPath()

  // `view-day <YYYY-MM-DD|today>`: what the ◀/▶ rows invoke. It only records
  // the choice — SwiftBar's refresh=true re-runs this file to draw it.
  if (argv && argv.length >= 2 && String(argv[0]) === 'view-day') {
    writeViewDay(statePath, String(argv[1]), nowMs)
    return ''
  }

  var ctx = {
    bin: findDaylog(), icon: envVar('DAYLOG_ICON'), error: '',
    nowMs: nowMs, statePath: statePath, self: selfPath(),
  }
  var day = null
  if (!ctx.bin) {
    ctx.error = 'daylog CLI not found — install it (install.sh) or set DAYLOG_PATH in this plugin’s settings'
  } else {
    // The date came out of the regex in readViewDay, so it is a bare
    // YYYY-MM-DD and safe to hand to the shell as-is.
    var viewDay = readViewDay(ctx.statePath, ctx.nowMs)
    var args = 'today' + (viewDay === '' ? '' : ' ' + viewDay) + ' --json'
    try {
      day = JSON.parse(sh(shellQuote(ctx.bin) + ' ' + args))
    } catch (e) {
      ctx.error = '`daylog ' + args + '` failed: ' + String(e.message || e)
    }
  }
  return render(day, ctx).join('\n')
}
