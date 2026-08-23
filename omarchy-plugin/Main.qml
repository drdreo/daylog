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
  readonly property var agentInbox: day && day.agent_inbox ? day.agent_inbox : []
  readonly property var prs: day && day.prs ? day.prs : []
  readonly property string date: day && day.date ? String(day.date) : ""
  readonly property string prsFetchedAt: day && day.prs_fetched_at ? String(day.prs_fetched_at) : ""

  function refresh() {
    if (!todayProcess.running) todayProcess.running = true
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
    command: [root.daylogPath, "today", "--json"]

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
}
