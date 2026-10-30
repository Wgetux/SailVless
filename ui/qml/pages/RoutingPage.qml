import QtQuick 2.6
import Sailfish.Silica 1.0
import "../js/util.js" as Util

Page {
    id: page

    property var routing: ({ mode: "global", country: "ru", rules: [], customFinal: "proxy" })
    property bool routeImported: false
    readonly property var modes: ["global", "bypass", "custom"]

    function load() {
        daemon.call("GetSettings", [], function (err, r) {
            if (err)
                return
            var s = Util.parse(r, {})
            var rt = s.routing || {}
            rt.rules = rt.rules || []   // Go sends null for an empty list
            page.routing = rt
            page.routeImported = !!s.routeImported
            // Silica 5.1 ComboBox ignores later changes of a bound currentIndex
            modeBox.currentIndex = Math.max(0, page.modes.indexOf(page.routing.mode))
            finalBox.currentIndex = page.routing.customFinal === "direct" ? 1 : 0
        })
    }

    // send the whole routing object: the daemon validates and reconnects
    function save(changes) {
        var r = JSON.parse(JSON.stringify(page.routing))
        for (var k in changes)
            r[k] = changes[k]
        app.run("SetSettings", [JSON.stringify({ routing: r })], page.load)
    }

    function ruleText(r) {
        var kinds = { domain: qsTr("domain"), suffix: qsTr("domain and subdomains"), keyword: qsTr("contains"),
                      ip: "IP", geosite: "geosite", geoip: "geoip" }
        var actions = { proxy: qsTr("proxy"), direct: qsTr("direct"), block: qsTr("block") }
        return kinds[r.kind] + " " + r.value + "  →  " + actions[r.action]
    }

    function removeRule(rule) {
        var rules = page.routing.rules.filter(function (r) {
            return !(r.kind === rule.kind && r.value === rule.value && r.action === rule.action)
        })
        page.save({ rules: rules })
    }

    Component.onCompleted: load()
    // e.g. back from the file picker after an import
    onStatusChanged: if (status === PageStatus.Active) load()

    RulesFile {
        id: rulesFile
        profileId: ""
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
                enabled: page.routing.rules.length > 0
                onClicked: rulesFile.exportRules()
            }
            MenuItem {
                text: qsTr("Add rule")
                onClicked: {
                    var d = pageStack.push(Qt.resolvedUrl("RuleDialog.qml"))
                    d.accepted.connect(function () {
                        var rules = page.routing.rules.slice(0)
                        rules.push({ kind: d.kind, value: d.value, action: d.action })
                        page.save({ rules: rules })
                    })
                }
            }
        }

        Column {
            id: column
            width: parent.width

            PageHeader { title: qsTr("Routing") }

            ComboBox {
                id: modeBox
                label: qsTr("Mode")
                menu: ContextMenu {
                    MenuItem { text: qsTr("Everything through the proxy"); onClicked: page.save({ mode: "global" }) }
                    MenuItem { text: qsTr("Bypass LAN and a country"); onClicked: page.save({ mode: "bypass" }) }
                    MenuItem { text: qsTr("Only my rules"); onClicked: page.save({ mode: "custom" }) }
                }
            }

            TextField {
                visible: page.routing.mode === "bypass"
                width: parent.width
                label: qsTr("Country code (direct)")
                text: page.routing.country || ""
                inputMethodHints: Qt.ImhNoAutoUppercase | Qt.ImhNoPredictiveText
                EnterKey.iconSource: "image://theme/icon-m-enter-accept"
                EnterKey.onClicked: { page.save({ country: text.trim().toLowerCase() }); focus = false }
            }
            Label {
                visible: page.routing.mode === "bypass"
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                font.pixelSize: Theme.fontSizeExtraSmall
                color: Theme.secondaryHighlightColor
                text: qsTr("Sites and IP addresses of this country go direct. For \"ru\": Russian services, .ru/.su/.рф domains and Russian IP ranges.")
            }

            ComboBox {
                id: finalBox
                visible: page.routing.mode === "custom"
                label: qsTr("Everything else")
                menu: ContextMenu {
                    MenuItem { text: qsTr("through the proxy"); onClicked: page.save({ customFinal: "proxy" }) }
                    MenuItem { text: qsTr("direct"); onClicked: page.save({ customFinal: "direct" }) }
                }
            }

            TextSwitch {
                text: qsTr("Block ads")
                description: qsTr("Ad and tracker domains from geosite:category-ads-all")
                checked: !!page.routing.blockAds
                automaticCheck: false
                onClicked: page.save({ blockAds: !checked })
            }

            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                font.pixelSize: Theme.fontSizeExtraSmall
                color: Theme.secondaryColor
                text: qsTr("Local network addresses always go direct.")
            }

            SectionHeader { text: qsTr("My rules (checked first)") }

            Repeater {
                model: page.routing.rules
                ListItem {
                    id: ruleItem
                    width: column.width
                    contentHeight: Theme.itemSizeSmall
                    menu: ContextMenu {
                        MenuItem {
                            text: qsTr("Move up")
                            visible: index > 0
                            onClicked: {
                                var rules = page.routing.rules.slice(0)
                                var r = rules.splice(index, 1)[0]
                                rules.splice(index - 1, 0, r)
                                page.save({ rules: rules })
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
                visible: page.routing.rules.length === 0
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                color: Theme.secondaryHighlightColor
                text: qsTr("No rules. Pull down to add one.")
            }

            SectionHeader { text: qsTr("DNS") }

            TextField {
                width: parent.width
                label: qsTr("DNS through the proxy")
                text: page.routing.dnsRemote || ""
                inputMethodHints: Qt.ImhUrlCharactersOnly | Qt.ImhNoAutoUppercase | Qt.ImhNoPredictiveText
                EnterKey.iconSource: "image://theme/icon-m-enter-accept"
                EnterKey.onClicked: { page.save({ dnsRemote: text.trim() }); focus = false }
            }
            TextField {
                width: parent.width
                label: qsTr("DNS for direct sites")
                text: page.routing.dnsDirect || ""
                inputMethodHints: Qt.ImhUrlCharactersOnly | Qt.ImhNoAutoUppercase | Qt.ImhNoPredictiveText
                EnterKey.iconSource: "image://theme/icon-m-enter-accept"
                EnterKey.onClicked: { page.save({ dnsDirect: text.trim() }); focus = false }
            }
            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                font.pixelSize: Theme.fontSizeExtraSmall
                color: Theme.secondaryHighlightColor
                text: qsTr("Formats: https://1.1.1.1/dns-query (DoH), tcp://8.8.8.8, 8.8.8.8; for direct sites also localhost (system DNS) and https+local://… DNS-over-TLS is not supported by Xray, use DoH instead.")
            }

            SectionHeader { text: qsTr("Imported configs") }

            TextSwitch {
                text: qsTr("Apply these rules to imported configs")
                description: qsTr("Panels that send full Xray JSON (e.g. with the Happ User-Agent) include their own routing. Off: keep the panel's routing.")
                checked: page.routeImported
                automaticCheck: false
                onClicked: app.run("SetSettings", [JSON.stringify({ routeImported: !checked })], page.load)
            }
        }

        VerticalScrollDecorator { }
    }
}
