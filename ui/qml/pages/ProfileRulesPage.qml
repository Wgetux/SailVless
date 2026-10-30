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

    function load() {
        daemon.call("GetProfileRules", [page.profileId], function (err, r) {
            if (!err)
                page.rules = Util.parse(r, []) || []
        })
    }

    // the daemon validates and reconnects if this server is active
    function save(rules) {
        app.run("SetProfileRules", [page.profileId, JSON.stringify(rules)], page.load)
    }

    function ruleText(r) {
        var kinds = { domain: qsTr("domain"), suffix: qsTr("domain and subdomains"), keyword: qsTr("contains"),
                      ip: "IP", geosite: "geosite", geoip: "geoip" }
        var actions = { proxy: qsTr("proxy"), direct: qsTr("direct"), block: qsTr("block") }
        return kinds[r.kind] + " " + r.value + "  →  " + actions[r.action]
    }

    function removeRule(rule) {
        var rules = page.rules.filter(function (r) {
            return !(r.kind === rule.kind && r.value === rule.value && r.action === rule.action)
        })
        page.save(rules)
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
            MenuItem {
                text: qsTr("Import rules from file")
                onClicked: rulesFile.importRules()
            }
            MenuItem {
                text: qsTr("Export rules to file")
                enabled: page.rules.length > 0
                onClicked: rulesFile.exportRules()
            }
            MenuItem {
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
                    menu: ContextMenu {
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
                            onClicked: {
                                // the delegate may be gone when the remorse fires (the list
                                // was redrawn): keep the page and the rule itself
                                var pg = page, rule = modelData
                                ruleItem.remorseDelete(function () { pg.removeRule(rule) })
                            }
                        }
                    }
                    Label {
                        anchors.verticalCenter: parent.verticalCenter
                        x: Theme.horizontalPageMargin
                        width: parent.width - 2 * x
                        truncationMode: TruncationMode.Fade
                        color: ruleItem.highlighted ? Theme.highlightColor : Theme.primaryColor
                        text: page.ruleText(modelData)
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
