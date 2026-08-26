import QtQuick
import Quickshell.Io

// The data side of the Daylog widget. The daylog CLI is the only interface
// (ARCHITECTURE.md §9): `daylog today --json` is the consumer contract and
// `daylog poll gh` refreshes the PR snapshot. This file shells out, parses,
// and exposes the folded day; the widget stays a dumb consumer.
Item {
  id: root
  visible: false

  property var settings: ({})

  function setting(name, fallback) {
    var value = settings ? settings[name] : undefined
    return value === undefined || value === null ? fallback : value
  }

  readonly property string daylogPath: String(setting("daylogPath", "daylog"))
  readonly property int refreshIntervalSec: Math.max(10, Number(setting("refreshIntervalSec", 60)))

  // The parsed `daylog today --json` object; null until the first good read.
  property var day: null
  property string error: ""
  property double updatedMs: 0
  property bool polling: false

  readonly property var entries: day && day.entries ? day.entries : []
  readonly property var openTodos: day && day.open_todos ? day.open_todos : []
  // needs_triage filters open_todos rather than partitioning it: these are
  // the agent-filed proposals still awaiting a verdict. Render open_todos
  // and consult this by id, or the same todo draws twice.
  readonly property var needsTriage: day && day.needs_triage ? day.needs_triage : []
  readonly property var prs: day && day.prs ? day.prs : []
  readonly property string date: day && day.date ? String(day.date) : ""
  readonly property string prsFetchedAt: day && day.prs_fetched_at ? String(day.prs_fetched_at) : ""

  // ------------------------------------------------------------ which day
  //
  // The panel is a window onto one day, not a hardcoded today. viewDate is
  // the *request* ("" = today, and it keeps meaning today across midnight);
  // shownDate is the day actually loaded, which is what every label reads,
  // so a heading can never describe a day other than the entries beneath it.
  property string viewDate: ""
  // Restamped on every refresh rather than bound to a `new Date()` QML would
  // never re-evaluate, so a panel left open rolls over at midnight.
  property string todayDate: Qt.formatDate(new Date(), "yyyy-MM-dd")
  property bool pendingRefresh: false

  readonly property string shownDate: date !== "" ? date : (viewDate !== "" ? viewDate : todayDate)
  // Negative for the past; 0 is today, which is what hides the ▶ row.
  readonly property int shownDelta: dayDelta(todayDate, shownDate)

  // Walk to another day. Forward stops at today: a day that hasn't happened
  // has nothing to log. The step is taken from the requested day, not the
  // loaded one, so holding ← keeps moving while a fold is still in flight.
  function stepDay(delta) {
    var next = shiftDay(viewDate !== "" ? viewDate : todayDate, delta)
    if (next === "" || dayDelta(todayDate, next) > 0) return
    viewDate = next === todayDate ? "" : next
    refresh()
  }

  function resetDay() {
    if (viewDate === "") return
    viewDate = ""
    refresh()
  }

  // ------------------------------------------------------------ days as days
  //
  // A day is a calendar day, never an instant: it is shifted and compared by
  // its date components at local midnight, so day arithmetic survives the
  // clocks changing. `iso` is always the YYYY-MM-DD the CLI speaks.

  function dayOf(iso) {
    var m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(String(iso || ""))
    return m === null ? null : new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]))
  }

  function shiftDay(iso, delta) {
    var d = dayOf(iso)
    if (d === null) return ""
    return Qt.formatDate(new Date(d.getFullYear(), d.getMonth(), d.getDate() + delta), "yyyy-MM-dd")
  }

  // Whole calendar days from fromISO to toISO — negative for the past. An
  // unparseable date yields 0, which degrades to "treat it as today" in every
  // caller rather than to a broken panel.
  function dayDelta(fromISO, toISO) {
    var a = dayOf(fromISO), b = dayOf(toISO)
    if (a === null || b === null) return 0
    return Math.round((b.getTime() - a.getTime()) / 86400000)
  }

  function calendarName(iso) {
    var d = dayOf(iso)
    return d === null ? "" : Qt.formatDate(d, "ddd, MMM d")
  }

  // The name a human would use: Today, Yesterday, or "Wed, Aug 19".
  function dayName(iso) {
    var delta = dayDelta(todayDate, iso)
    if (delta === 0) return "Today"
    if (delta === -1) return "Yesterday"
    if (delta === 1) return "Tomorrow"
    var name = calendarName(iso)
    return name === "" ? String(iso || "") : name
  }

  // The distance spelled out, for days whose name doesn't already say it.
  function dayDistance(iso) {
    var delta = dayDelta(todayDate, iso)
    if (delta >= -1 && delta <= 1) return ""
    return delta < 0 ? -delta + " days ago" : "in " + delta + " days"
  }

  // The day section's heading: the day's name, its date when the name hides
  // it, and how far back it is. "TODAY · MON, AUG 24", "WED, AUG 19 · 5 DAYS AGO".
  function dayHeading(iso) {
    var parts = [dayName(iso)]
    var calendar = calendarName(iso)
    if (calendar !== "" && calendar !== parts[0]) parts.push(calendar)
    var distance = dayDistance(iso)
    if (distance !== "") parts.push(distance)
    return parts.join(" · ").toUpperCase()
  }

  // The empty state names the day it is empty *about*, so an untouched
  // Tuesday can never be misread as a quiet morning.
  function emptyDayNote(iso) {
    var delta = dayDelta(todayDate, iso)
    if (delta === 0) return "Nothing logged yet today."
    if (delta > 0) return "Nothing logged for " + dayName(iso).toLowerCase() + " yet."
    if (delta === -1) return "Nothing was logged yesterday."
    return "Nothing was logged on " + calendarName(iso) + "."
  }

  // A day request in flight is not dropped, just queued: holding ← must keep
  // walking rather than stall on whichever fold happened to be running.
  function refresh() {
    todayDate = Qt.formatDate(new Date(), "yyyy-MM-dd")
    if (todayProcess.running) {
      pendingRefresh = true
      return
    }
    todayProcess.command = viewDate === ""
      ? [daylogPath, "today", "--json"]
      : [daylogPath, "today", viewDate, "--json"]
    todayProcess.running = true
  }

  // One PR poll cycle through the same CLI the systemd timer uses; the
  // refresh in onExited picks up whatever transitions the poll logged.
  function pollNow() {
    if (pollProcess.running) return
    polling = true
    pollProcess.running = true
  }

  function applyToday(output) {
    var text = String(output || "")
    if (text.trim() === "") {
      // A missing or failed binary produces no stdout. Keep showing the last
      // good day if we have one; only a widget that never loaded says why.
      if (day === null)
        error = "No output from `" + daylogPath + " today --json` — is the daylog CLI installed and on PATH?"
      return
    }
    try {
      day = JSON.parse(text)
      error = ""
      updatedMs = Date.now()
    } catch (e) {
      if (day === null) error = "Could not parse daylog output: " + e
    }
  }

  // Mirror of the CLI's markdown status text, so the panel and the terminal
  // describe a PR in the same words.
  function prStatusLabel(pr) {
    if (!pr) return ""
    if (String(pr.state) !== "open") return String(pr.state)
    var parts = []
    if (pr.draft === true) parts.push("draft")
    if (String(pr.checks || "none") !== "none") parts.push("checks " + pr.checks)
    var review = String(pr.review || "none")
    if (review === "approved" || review === "review_required" || review === "changes_requested")
      parts.push(review.replace(/_/g, " "))
    return parts.length > 0 ? parts.join(" · ") : "open"
  }

  Timer {
    interval: root.refreshIntervalSec * 1000
    running: true
    repeat: true
    triggeredOnStart: true
    onTriggered: root.refresh()
  }

  Process {
    id: todayProcess
    running: false
    // No declarative command: refresh() builds it, because it carries the day
    // being requested and must not change under a running process.
    onExited: if (root.pendingRefresh) {
      root.pendingRefresh = false
      Qt.callLater(root.refresh)
    }

    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: root.applyToday(text)
    }
    stderr: StdioCollector {
      waitForEnd: true
      onStreamFinished: if (text.trim() !== "") console.warn("daylog", text.trim())
    }
  }

  Process {
    id: pollProcess
    running: false
    command: [root.daylogPath, "poll", "gh"]
    onExited: {
      root.polling = false
      root.refresh()
    }

    stderr: StdioCollector {
      waitForEnd: true
      onStreamFinished: if (text.trim() !== "") console.warn("daylog poll", text.trim())
    }
  }

  // Close a todo through the same single write path everything else uses.
  // The full ULID is passed, so the CLI's prefix match is exact — the widget
  // never resolves fuzzily on the user's behalf.
  function markDone(id) {
    if (doneProcess.running || String(id) === "") return
    doneProcess.command = [daylogPath, "done", String(id)]
    doneProcess.running = true
  }

  Process {
    id: doneProcess
    running: false
    onExited: root.refresh()

    stderr: StdioCollector {
      waitForEnd: true
      onStreamFinished: if (text.trim() !== "") console.warn("daylog done", text.trim())
    }
  }

  // Triage an agent-filed proposal: accept adopts it as yours, decline drops
  // it from every view. Same single write path, same exact-ULID discipline.
  function triage(id, verdict) {
    if (triageProcess.running || String(id) === "") return
    if (verdict !== "accept" && verdict !== "decline") return
    // A click is the human ruling, so the identity is stated outright rather
    // than inherited from whatever $DAYLOG_SOURCE the widget was launched with.
    triageProcess.command = [daylogPath, verdict, String(id), "--source", "human:widget"]
    triageProcess.running = true
  }

  Process {
    id: triageProcess
    running: false
    onExited: root.refresh()

    stderr: StdioCollector {
      waitForEnd: true
      onStreamFinished: if (text.trim() !== "") console.warn("daylog triage", text.trim())
    }
  }

  // True when the todo with this id is an untriaged agent proposal.
  function needsTriageId(id) {
    for (var i = 0; i < needsTriage.length; i++) {
      if (String(needsTriage[i].id) === String(id)) return true
    }
    return false
  }
}
