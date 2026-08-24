import QtQuick
import QtQuick.Controls
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui

// Daylog bar widget: one icon, one panel. The icon lights up when an agent
// proposal is still awaiting triage or an open PR has failing checks — the
// two "needs me" signals. The panel is the day view: todos, entries, PRs.
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

  // ---------------------------------------------------------------- cursor
  //
  // One flat cursor over every row in visual order — open todos, today's
  // entries, then PRs — addressed by a global index so arrow keys
  // walk the whole panel. Rows compute their own index from these offsets.
  property bool cursorActive: false
  property int cursorIndex: -1

  readonly property int todosOffset: 0
  readonly property int entriesOffset: todosOffset + store.openTodos.length
  readonly property int prsOffset: entriesOffset + store.entries.length
  readonly property int rowCount: prsOffset + store.prs.length

  // A refresh can shrink the lists underneath the cursor.
  onRowCountChanged: if (cursorIndex >= rowCount) cursorIndex = rowCount - 1

  function moveCursor(dy) {
    if (rowCount === 0) return
    if (!cursorActive) {
      cursorActive = true
      cursorIndex = dy > 0 ? 0 : rowCount - 1
      return
    }
    cursorIndex = clamp(cursorIndex + dy, 0, rowCount - 1)
  }

  function selectedRow() {
    var i = cursorIndex
    if (!cursorActive || i < 0 || i >= rowCount) return null
    if (i < entriesOffset) return { kind: "entry", data: store.openTodos[i - todosOffset] }
    if (i < prsOffset) return { kind: "entry", data: store.entries[i - entriesOffset] }
    return { kind: "pr", data: store.prs[i - prsOffset] }
  }

  function selectedUrl() {
    var row = selectedRow()
    if (!row || !row.data) return ""
    if (row.kind === "pr") return String(row.data.url || "")
    return row.data.pr ? String(row.data.pr.url || "") : ""
  }

  function openUrl(url) {
    if (String(url) === "") return
    Quickshell.execDetached(["xdg-open", String(url)])
    root.close()
  }

  // Enter/Space: open the selected PR in the browser (a PR row, or an entry
  // that references one). With no cursor, activate falls back to refresh.
  function activateSelected() {
    if (!cursorActive) {
      store.refresh()
      return
    }
    openUrl(selectedUrl())
  }

  // d: close the selected todo — finishing one of your own, or an agent's
  // proposal you did act on. Only open todos respond; everything else is a
  // record, not an obligation (there is nothing to "do" to it).
  function dismissSelected() {
    var row = selectedRow()
    if (!row || row.kind !== "entry" || !row.data) return
    var e = row.data
    if (String(e.type) !== "todo" || e.done === true) return
    store.markDone(String(e.id))
  }

  // a / x: rule on the selected agent proposal — accept adopts it as yours,
  // decline drops it from every view. Only untriaged rows respond; a todo
  // you already own has nothing left to decide.
  function triageSelected(verdict) {
    var row = selectedRow()
    if (!row || row.kind !== "entry" || !row.data) return
    var e = row.data
    if (!store.needsTriageId(String(e.id))) return
    store.triage(String(e.id), verdict)
  }

  // Keep the cursor's row inside the viewport as it walks.
  function ensureVisible(item) {
    if (!panelFlick || !item) return
    var y = item.mapToItem(column, 0, 0).y
    var pad = Style.space(28)
    if (y < panelFlick.contentY + pad)
      panelFlick.contentY = Math.max(0, y - pad)
    else if (y + item.height > panelFlick.contentY + panelFlick.height - pad)
      panelFlick.contentY = Math.min(Math.max(0, panelFlick.contentHeight - panelFlick.height),
                                     y + item.height - panelFlick.height + pad)
  }

  readonly property bool prFailing: {
    for (var i = 0; i < store.prs.length; i++) {
      if (String(store.prs[i].checks) === "failing") return true
    }
    return false
  }
  readonly property bool needsAttention: store.needsTriage.length > 0 || prFailing

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
    var open = store.openTodos.length
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
    var hints = "↑↓ · ⏎ open · d done · a accept · x decline · r refresh · p poll"
    if (store.updatedMs <= 0) return hints
    return "Updated " + Qt.formatTime(new Date(store.updatedMs), "HH:mm") + " · " + hints
  }

  visible: true
  implicitWidth: button.implicitWidth
  implicitHeight: button.implicitHeight

  onOpenedChanged: if (opened) {
    nowMs = Date.now()
    cursorActive = false
    cursorIndex = -1
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
        if (dy !== 0) root.moveCursor(dy)
      }
      onActivateRequested: root.activateSelected()
      onDeleteRequested: root.dismissSelected()
      onCloseRequested: root.close()
      onTabRequested: function(direction) { root.switchPanel(direction) }
      onTextKey: function(t) {
        if (t === "r" || t === "R") store.refresh()
        else if (t === "p" || t === "P") store.pollNow()
        else if (t === "o" || t === "O") root.activateSelected()
        else if (t === "d" || t === "D") root.dismissSelected()
        else if (t === "a" || t === "A") root.triageSelected("accept")
        else if (t === "x" || t === "X") root.triageSelected("decline")
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

          // ---------- Open todos: one list, untriaged proposals accented ----------
          Column {
            visible: store.openTodos.length > 0
            width: parent.width
            spacing: Style.spacing.md

            PanelSectionHeader {
              width: parent.width
              text: store.needsTriage.length > 0
                    ? "OPEN TODOS (" + store.needsTriage.length + " awaiting triage)"
                    : "OPEN TODOS"
              foreground: store.needsTriage.length > 0 ? root.urgent : root.foreground
              fontFamily: root.fontFamily
            }

            Repeater {
              model: store.openTodos

              EntryRow {
                required property var modelData
                required property int index
                width: parent.width
                entry: modelData
                accent: store.needsTriageId(modelData.id)
                rowIndex: root.todosOffset + index
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
                required property int index
                width: parent.width
                entry: modelData
                showTime: true
                rowIndex: root.entriesOffset + index
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
                required property int index
                width: parent.width
                pr: modelData
                rowIndex: root.prsOffset + index
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
    property int rowIndex: -1

    readonly property var pr: entry && entry.pr ? entry.pr : null
    readonly property bool prAlarming: pr !== null && String(pr.checks) === "failing"
    readonly property bool selected: root.cursorActive && root.cursorIndex === rowIndex
    readonly property string url: pr ? String(pr.url || "") : ""

    onSelectedChanged: if (selected) root.ensureVisible(entryRow)

    implicitHeight: Math.max(entryText.implicitHeight, entrySource.implicitHeight) + Style.spacing.sm

    Rectangle {
      visible: entryRow.selected
      anchors.fill: parent
      radius: Style.cornerRadius
      color: root.alpha(root.foreground, 0.12)
    }

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
      acceptedButtons: Qt.LeftButton
      cursorShape: entryRow.url !== "" ? Qt.PointingHandCursor : Qt.ArrowCursor
      onClicked: {
        root.cursorActive = true
        root.cursorIndex = entryRow.rowIndex
        root.openUrl(entryRow.url)
      }
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
    property int rowIndex: -1

    readonly property bool alarming: pr !== null
      && (String(pr.checks) === "failing" || String(pr.review) === "changes_requested")
    readonly property bool selected: root.cursorActive && root.cursorIndex === rowIndex

    onSelectedChanged: if (selected) root.ensureVisible(prRow)

    implicitHeight: prTitle.implicitHeight + prStatus.implicitHeight + Style.spacing.sm

    Rectangle {
      visible: prRow.selected
      anchors.fill: parent
      radius: Style.cornerRadius
      color: root.alpha(root.foreground, 0.12)
    }

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
      acceptedButtons: Qt.LeftButton
      cursorShape: Qt.PointingHandCursor
      onClicked: {
        root.cursorActive = true
        root.cursorIndex = prRow.rowIndex
        root.openUrl(prRow.pr ? String(prRow.pr.url || "") : "")
      }
    }

    PanelToolTip {
      visible: prHover.containsMouse
      text: prRow.pr ? prRow.pr.title + "\n" + prRow.pr.url : ""
      fontFamily: root.fontFamily
    }
  }
}
