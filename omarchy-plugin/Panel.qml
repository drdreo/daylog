import QtQuick
import QtQuick.Controls
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui

// Daylog bar widget: one icon, one panel. The icon lights up when the agent
// inbox has untriaged todos or an open PR has failing checks — the two
// "needs me" signals. The panel is the day view: entries, todos, inbox, PRs.
Panel {
  id: root
  moduleName: "drdreo.daylog"
  ipcTarget: "drdreo.daylog"
  manageIpc: false

  readonly property color foreground: bar ? bar.foreground : Color.foreground
  readonly property color urgent: bar ? bar.urgent : Color.urgent
  readonly property color dim: Qt.darker(foreground, 1.55)
  readonly property color surface: Color.popups.background
  readonly property string fontFamily: bar ? bar.fontFamily : Style.font.family

  // Staleness countdowns read this instead of Date.now() so an open panel
  // keeps telling the truth (same pattern as the agents panel).
  property double nowMs: Date.now()

  readonly property bool prFailing: {
    for (var i = 0; i < store.prs.length; i++) {
      if (String(store.prs[i].checks) === "failing") return true
    }
    return false
  }
  readonly property bool needsAttention: store.agentInbox.length > 0 || prFailing

  readonly property bool prsStale: {
    if (store.prsFetchedAt === "") return false
    var fetched = new Date(store.prsFetchedAt).getTime()
    return isFinite(fetched) && root.nowMs - fetched > 2 * 3600 * 1000
  }

  function clamp(v, lo, hi) { return Math.max(lo, Math.min(hi, v)) }
  function alpha(c, a) { return Qt.rgba(c.r, c.g, c.b, a) }

  function clockOf(ts) {
    var t = new Date(String(ts || ""))
    return isNaN(t.getTime()) ? "" : Qt.formatTime(t, "HH:mm")
  }

  // agent:claude → claude, human:cli → cli, poller:gh → gh
  function shortSource(source) {
    var text = String(source || "")
    var sep = text.indexOf(":")
    return sep >= 0 ? text.slice(sep + 1) : text
  }

  function heroMeta() {
    if (store.day === null) return "No data yet"
    var parts = [store.date]
    parts.push(store.entries.length + (store.entries.length === 1 ? " entry" : " entries"))
    var open = store.openTodos.length + store.agentInbox.length
    if (open > 0) parts.push(open + " open todo" + (open === 1 ? "" : "s"))
    return parts.join(" · ")
  }

  function entryTooltip(e) {
    if (!e) return ""
    var parts = [clockOf(e.ts) + " · " + String(e.source) + " · " + String(e.type)]
    if (e.original_type) parts.push("was " + e.original_type)
    if (e.refs && e.refs.length > 0) parts.push(e.refs.join(", "))
    if (e.done_note) parts.push("closed: " + e.done_note)
    parts.push(String(e.tldr))
    return parts.join("\n")
  }

  function prsHeaderText() {
    if (!root.prsStale) return "OPEN PRS"
    var fetched = new Date(store.prsFetchedAt)
    return "OPEN PRS (STALE — fetched " + Qt.formatDateTime(fetched, "MMM d HH:mm") + ")"
  }

  function footerText() {
    if (store.polling) return "Polling GitHub…"
    if (store.updatedMs <= 0) return "r refresh · p poll gh"
    return "Updated " + Qt.formatTime(new Date(store.updatedMs), "HH:mm") + " · r refresh · p poll gh"
  }

  visible: true
  implicitWidth: button.implicitWidth
  implicitHeight: button.implicitHeight

  onOpenedChanged: if (opened) {
    nowMs = Date.now()
    if (panelFlick) panelFlick.contentY = 0
    store.refresh()
    Qt.callLater(function() { keyCatcher.forceActiveFocus() })
  }

  Main {
    id: store
    settings: root.settings
  }

  Timer {
    interval: 30000
    running: root.opened
    repeat: true
    onTriggered: root.nowMs = Date.now()
  }

  IpcHandler {
    target: root.ipcTarget
    function open(): void { root.open() }
    function close(): void { root.close() }
    function show(): void { root.open() }
    function hide(): void { root.close() }
    function toggle(): void { root.toggle() }
    function refresh(): string { store.refresh(); return "ok" }
    function poll(): string { store.pollNow(); return "ok" }
  }

  BarIconButton {
    id: button
    anchors.fill: parent
    bar: root.bar
    text: String(root.setting("icon", "󰃭"))
    active: root.needsAttention
    onPressed: function(buttonCode) {
      if (buttonCode === Qt.RightButton) store.pollNow()
      else if (buttonCode === Qt.MiddleButton) store.refresh()
      else root.toggle()
    }
  }

  KeyboardPanel {
    id: panel
    anchorItem: button
    owner: root
    bar: root.bar
    open: root.opened
    focusTarget: keyCatcher
    contentWidth: panel.fittedContentWidth(Style.space(420))
    // A dashboard like the agents panel: the point is reading the whole day
    // without scrolling, so it gets the tall cap.
    contentHeight: panel.fittedContentHeight(column.implicitHeight, Style.space(640))

    PanelKeyCatcher {
      id: keyCatcher
      anchors.fill: parent

      onMoveRequested: function(dx, dy) {
        if (dy !== 0)
          panelFlick.contentY = root.clamp(panelFlick.contentY + dy * Style.space(56), 0,
                                           Math.max(0, panelFlick.contentHeight - panelFlick.height))
      }
      onActivateRequested: store.refresh()
      onCloseRequested: root.close()
      onTabRequested: function(direction) { root.switchPanel(direction) }
      onTextKey: function(t) {
        if (t === "r" || t === "R") store.refresh()
        else if (t === "p" || t === "P") store.pollNow()
      }

      Flickable {
        id: panelFlick
        anchors.fill: parent
        contentWidth: width
        contentHeight: column.implicitHeight
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        flickableDirection: Flickable.VerticalFlick
        interactive: contentHeight > height
        ScrollBar.vertical: ScrollBar { policy: ScrollBar.AsNeeded }

        Column {
          id: column
          width: panelFlick.width
          spacing: Style.space(12)

          PanelHero {
            width: parent.width
            title: "Daylog"
            meta: root.heroMeta()
            foreground: root.foreground
            fontFamily: root.fontFamily
          }

          // ---------- Load failure ----------
          BorderSurface {
            visible: store.error !== ""
            width: parent.width
            implicitHeight: errorText.implicitHeight + Style.spacing.xl * 2
            color: root.alpha(root.urgent, 0.10)
            borderSpec: Border.flat(root.alpha(root.urgent, 0.35), 1)
            radius: Style.cornerRadius

            Text {
              id: errorText
              anchors.left: parent.left
              anchors.right: parent.right
              anchors.verticalCenter: parent.verticalCenter
              anchors.leftMargin: Style.space(12)
              anchors.rightMargin: Style.space(12)
              text: store.error
              color: root.dim
              font.family: root.fontFamily
              font.pixelSize: Style.font.caption
              wrapMode: Text.WordWrap
            }
          }

          // ---------- Agent inbox: proposals awaiting triage ----------
          Column {
            visible: store.agentInbox.length > 0
            width: parent.width
            spacing: Style.spacing.md

            PanelSectionHeader {
              width: parent.width
              text: "AGENT INBOX (" + store.agentInbox.length + " to triage)"
              foreground: root.urgent
              fontFamily: root.fontFamily
            }

            Repeater {
              model: store.agentInbox

              EntryRow {
                required property var modelData
                width: parent.width
                entry: modelData
                accent: true
              }
            }
          }

          // ---------- Open todos ----------
          Column {
            visible: store.openTodos.length > 0
            width: parent.width
            spacing: Style.spacing.md

            PanelSectionHeader {
              width: parent.width
              text: "OPEN TODOS"
              foreground: root.foreground
              fontFamily: root.fontFamily
            }

            Repeater {
              model: store.openTodos

              EntryRow {
                required property var modelData
                width: parent.width
                entry: modelData
              }
            }
          }

          // ---------- Today's entries ----------
          PanelSeparator {
            visible: store.entries.length > 0
            foreground: root.foreground
          }

          Column {
            visible: store.entries.length > 0
            width: parent.width
            spacing: Style.spacing.md

            PanelSectionHeader {
              width: parent.width
              text: "TODAY"
              foreground: root.foreground
              fontFamily: root.fontFamily
            }

            Repeater {
              model: store.entries

              EntryRow {
                required property var modelData
                width: parent.width
                entry: modelData
                showTime: true
              }
            }
          }

          Text {
            visible: store.day !== null && store.entries.length === 0 && store.error === ""
            width: parent.width
            text: "Nothing logged yet today."
            color: root.dim
            font.family: root.fontFamily
            font.pixelSize: Style.font.body
            horizontalAlignment: Text.AlignHCenter
          }

          // ---------- Open PRs (snapshot join) ----------
          PanelSeparator {
            visible: store.prs.length > 0
            foreground: root.foreground
          }

          Column {
            visible: store.prs.length > 0
            width: parent.width
            spacing: Style.spacing.md

            PanelSectionHeader {
              width: parent.width
              text: root.prsHeaderText()
              foreground: root.prsStale ? root.urgent : root.foreground
              fontFamily: root.fontFamily
            }

            Repeater {
              model: store.prs

              PRRow {
                required property var modelData
                width: parent.width
                pr: modelData
              }
            }
          }

          Text {
            width: parent.width
            topPadding: Style.space(2)
            text: root.footerText()
            color: root.dim
            font.family: root.fontFamily
            font.pixelSize: Style.font.caption
            horizontalAlignment: Text.AlignHCenter
            elide: Text.ElideRight
          }
        }
      }
    }
  }

  // One log entry: time (optional), tldr, and the short source name. A done
  // entry is struck through; a PR-referencing entry carries its live status.
  component EntryRow: Item {
    id: entryRow
    property var entry: null
    property bool showTime: false
    property bool accent: false

    readonly property var pr: entry && entry.pr ? entry.pr : null
    readonly property bool prAlarming: pr !== null && String(pr.checks) === "failing"

    implicitHeight: Math.max(entryText.implicitHeight, entrySource.implicitHeight) + Style.spacing.sm

    Text {
      id: entryTime
      visible: entryRow.showTime
      text: entryRow.entry ? root.clockOf(entryRow.entry.ts) : ""
      color: root.dim
      font.family: root.fontFamily
      font.pixelSize: Style.font.caption
      anchors.left: parent.left
      anchors.verticalCenter: parent.verticalCenter
      width: entryRow.showTime ? Style.space(40) : 0
    }

    Text {
      id: entryText
      text: {
        if (!entryRow.entry) return ""
        var text = String(entryRow.entry.tldr)
        if (entryRow.pr) text += "  [" + store.prStatusLabel(entryRow.pr) + "]"
        return text
      }
      color: entryRow.accent ? root.foreground
        : (entryRow.entry && entryRow.entry.done === true ? root.dim : root.foreground)
      font.family: root.fontFamily
      font.pixelSize: Style.font.bodySmall
      font.strikeout: entryRow.entry ? entryRow.entry.done === true : false
      elide: Text.ElideRight
      anchors.left: entryRow.showTime ? entryTime.right : parent.left
      anchors.leftMargin: entryRow.showTime ? Style.space(6) : 0
      anchors.right: entrySource.left
      anchors.rightMargin: Style.spacing.sm
      anchors.verticalCenter: parent.verticalCenter
    }

    Text {
      id: entrySource
      text: entryRow.entry ? root.shortSource(entryRow.entry.source) : ""
      color: entryRow.prAlarming ? root.urgent : root.dim
      font.family: root.fontFamily
      font.pixelSize: Style.font.caption
      anchors.right: parent.right
      anchors.verticalCenter: parent.verticalCenter
    }

    MouseArea {
      id: entryHover
      anchors.fill: parent
      hoverEnabled: true
      acceptedButtons: Qt.NoButton
    }

    PanelToolTip {
      visible: entryHover.containsMouse
      text: root.entryTooltip(entryRow.entry)
      fontFamily: root.fontFamily
    }
  }

  // One open PR: repo#number, elided title, status. Failing checks and
  // requested changes take the urgent color — those are the ones that need you.
  component PRRow: Item {
    id: prRow
    property var pr: null

    readonly property bool alarming: pr !== null
      && (String(pr.checks) === "failing" || String(pr.review) === "changes_requested")

    implicitHeight: prTitle.implicitHeight + prStatus.implicitHeight + Style.spacing.sm

    Text {
      id: prTitle
      text: prRow.pr ? prRow.pr.repo + "#" + prRow.pr.number + "  " + prRow.pr.title : ""
      color: root.foreground
      font.family: root.fontFamily
      font.pixelSize: Style.font.bodySmall
      elide: Text.ElideRight
      anchors.left: parent.left
      anchors.right: parent.right
      anchors.top: parent.top
    }

    Text {
      id: prStatus
      text: prRow.pr ? store.prStatusLabel(prRow.pr) : ""
      color: prRow.alarming ? root.urgent : root.dim
      font.family: root.fontFamily
      font.pixelSize: Style.font.caption
      anchors.left: parent.left
      anchors.top: prTitle.bottom
    }

    MouseArea {
      id: prHover
      anchors.fill: parent
      hoverEnabled: true
      acceptedButtons: Qt.NoButton
    }

    PanelToolTip {
      visible: prHover.containsMouse
      text: prRow.pr ? prRow.pr.title + "\n" + prRow.pr.url : ""
      fontFamily: root.fontFamily
    }
  }
}
