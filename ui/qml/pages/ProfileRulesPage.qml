import QtQuick 2.6
import Sailfish.Silica 1.0
import "../js/util.js" as Util

// One server's own rules: checked before the common ones (Routing page).
Page {
    id: page

    property string profileId
    property string profileName
    property string protocol
    property var rules: []
    property bool selecting: false
    property var marked: ({})      // rule key -> true: selected / waiting to be deleted
    property bool restarting: false // the remorse is restarted, not canceled by the user
    property bool saving: false
    property var pending: null     // rules to save once the running save is done

    function load() {
        if (page.saving)
            return // the running save reloads when done
        daemon.call("GetProfileRules", [page.profileId], function (err, r) {
            if (!err && !page.saving)
                page.rules = Util.parse(r, []) || []
        })
    }

    // shown at once; saves go one at a time (the daemon validates and
    // reconnects if this server is active), the last state wins
    function save(rules) {
        page.rules = rules
        if (page.saving) {
            page.pending = rules
            return
        }
        page.saving = true
        daemon.call("SetProfileRules", [page.profileId, JSON.stringify(rules)], function (err) {
            if (err)
                app.notify(err)
            page.saving = false
            if (page.pending !== null) {
                var next = page.pending
                page.pending = null
                page.save(next)
            } else {
                page.load()
            }
        })
    }

    function key(r) { return r.kind + "|" + r.value + "|" + r.action }

    function markedCount() { return Object.keys(page.marked).length }

    function toggle(r) {
        var m = JSON.parse(JSON.stringify(page.marked))
        if (m[key(r)])
            delete m[key(r)]
        else
            m[key(r)] = true
        page.marked = m
    }

    function selectAll() {
        var m = {}
        for (var i = 0; i < page.rules.length; i++)
            m[key(page.rules[i])] = true
        page.marked = m
    }

    // one remorse and one save for everything marked
    function deleteMarked() {
        var n = markedCount()
        if (n === 0)
            return
        // a new candidate restarts the countdown; that cancel must keep the marks
        page.restarting = true
        if (remorse.visible)
            remorse.cancel()
        remorse.execute(qsTr("Deleting %n rule(s)", "", n), function () {
            var m = page.marked
            page.marked = ({})
            page.selecting = false
            page.save(page.rules.filter(function (r) { return !m[page.key(r)] }))
        })
        page.restarting = false
    }

    function ruleText(r) {
        var kinds = { domain: qsTr("domain"), suffix: qsTr("domain and subdomains"), keyword: qsTr("contains"),
                      ip: "IP", geosite: "geosite", geoip: "geoip" }
        var actions = { proxy: qsTr("proxy"), direct: qsTr("direct"), block: qsTr("block") }
        return kinds[r.kind] + " " + r.value + "  →  " + actions[r.action]
    }

    function removeRule(rule) {
        if (!page.marked[key(rule)])
            toggle(rule)
        deleteMarked()
    }

    RemorsePopup {
        id: remorse
        onCanceled: if (!page.restarting) page.marked = ({})
    }

    Component.onCompleted: load()
    // e.g. back from the file picker after an import
    onStatusChanged: if (status === PageStatus.Active) load()

    RulesFile {
        id: rulesFile
        profileId: page.profileId
        onImported: page.load()
    }

    SilicaFlickable {
        anchors.fill: parent
        contentHeight: column.height + Theme.paddingLarge

        PullDownMenu {
            // selection mode
            MenuItem {
                visible: page.selecting
                text: qsTr("Cancel selection")
                onClicked: { page.selecting = false; page.marked = ({}) }
            }
            MenuItem {
                visible: page.selecting
                text: qsTr("Select all")
                onClicked: page.selectAll()
            }
            MenuItem {
                visible: page.selecting
                text: qsTr("Delete selected (%1)").arg(page.markedCount())
                enabled: page.markedCount() > 0
                onClicked: page.deleteMarked()
            }
            // normal mode
            MenuItem {
                visible: !page.selecting && page.rules.length > 1
                text: qsTr("Select rules")
                onClicked: { page.marked = ({}); page.selecting = true }
            }
            MenuItem {
                visible: !page.selecting
                text: qsTr("Import rules from file")
                onClicked: rulesFile.importRules()
            }
            MenuItem {
                visible: !page.selecting
                text: qsTr("Export rules to file")
                enabled: page.rules.length > 0
                onClicked: rulesFile.exportRules()
            }
            MenuItem {
                visible: !page.selecting
                text: qsTr("Add rule")
                onClicked: {
                    var d = pageStack.push(Qt.resolvedUrl("RuleDialog.qml"))
                    d.accepted.connect(function () {
                        var rules = page.rules.slice(0)
                        rules.push({ kind: d.kind, value: d.value, action: d.action })
                        page.save(rules)
                    })
                }
            }
        }

        Column {
            id: column
            width: parent.width

            PageHeader {
                title: qsTr("Server rules")
                description: Util.name(page.profileName)
            }

            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                font.pixelSize: Theme.fontSizeExtraSmall
                color: Theme.secondaryHighlightColor
                text: qsTr("Only for this server, checked before the common rules (Routing).")
            }
            Item { width: 1; height: Theme.paddingMedium }
            Label {
                visible: page.protocol === "amneziawg"
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                font.pixelSize: Theme.fontSizeExtraSmall
                color: Theme.secondaryHighlightColor
                text: qsTr("Split configs (only some networks through the tunnel): a server may forward only its own list of sites, then \"through the proxy\" cannot add new ones. \"Direct\" and \"block\" always work.")
            }
            Label {
                visible: page.protocol === "xray-json"
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                font.pixelSize: Theme.fontSizeExtraSmall
                color: Theme.secondaryHighlightColor
                text: qsTr("Imported full configs use rules only with \"Apply these rules to imported configs\" on (Routing).")
            }

            SectionHeader { text: qsTr("Rules") }

            Repeater {
                model: page.rules
                ListItem {
                    id: ruleItem
                    width: column.width
                    contentHeight: Theme.itemSizeSmall
                    readonly property bool isMarked: !!page.marked[page.key(modelData)]
                    highlighted: down || isMarked
                    onClicked: if (page.selecting) page.toggle(modelData)
                    menu: page.selecting ? null : ruleMenu
                    Component {
                        id: ruleMenu
                        ContextMenu {
                            MenuItem {
                                text: qsTr("Move up")
                                visible: index > 0
                                onClicked: {
                                    var rules = page.rules.slice(0)
                                    var r = rules.splice(index, 1)[0]
                                    rules.splice(index - 1, 0, r)
                                    page.save(rules)
                                }
                            }
                            MenuItem {
                                text: qsTr("Delete")
                                onClicked: page.removeRule(modelData)
                            }
                        }
                    }
                    Label {
                        anchors.verticalCenter: parent.verticalCenter
                        x: Theme.horizontalPageMargin
                        width: parent.width - 2 * x
                        truncationMode: TruncationMode.Fade
                        color: ruleItem.highlighted ? Theme.highlightColor : Theme.primaryColor
                        text: (page.selecting ? (ruleItem.isMarked ? "☑  " : "☐  ") : "") + page.ruleText(modelData)
                    }
                }
            }
            Label {
                visible: page.rules.length === 0
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                color: Theme.secondaryHighlightColor
                text: qsTr("No rules. Pull down to add one.")
            }
        }

        VerticalScrollDecorator { }
    }
}
