import QtQuick 2.6
import Sailfish.Silica 1.0

Dialog {
    id: dialog

    readonly property var kinds: ["suffix", "domain", "keyword", "ip", "geosite", "geoip"]
    readonly property var actions: ["proxy", "direct", "block"]
    property string kind: "suffix"
    property string action: "direct"
    property bool editing: false
    property alias value: valueField.text

    canAccept: valueField.text.trim().length > 0 && valueField.acceptableInput

    Column {
        width: parent.width

        DialogHeader { acceptText: dialog.editing ? qsTr("Save") : qsTr("Add") }

        ComboBox {
            label: qsTr("Match")
            currentIndex: dialog.kinds.indexOf(dialog.kind)
            menu: ContextMenu {
                MenuItem { text: qsTr("domain and subdomains"); onClicked: dialog.kind = "suffix" }
                MenuItem { text: qsTr("exact domain"); onClicked: dialog.kind = "domain" }
                MenuItem { text: qsTr("domain contains"); onClicked: dialog.kind = "keyword" }
                MenuItem { text: qsTr("IP or subnet"); onClicked: dialog.kind = "ip" }
                MenuItem { text: qsTr("geosite category"); onClicked: dialog.kind = "geosite" }
                MenuItem { text: qsTr("geoip country/list"); onClicked: dialog.kind = "geoip" }
            }
        }

        TextField {
            id: valueField
            width: parent.width
            focus: true
            label: dialog.kind === "ip" ? qsTr("e.g. 203.0.113.0/24")
                 : dialog.kind === "geosite" ? qsTr("e.g. telegram, category-ru, youtube")
                 : dialog.kind === "geoip" ? qsTr("e.g. ru, telegram")
                 : qsTr("e.g. example.com")
            placeholderText: label
            inputMethodHints: Qt.ImhNoAutoUppercase | Qt.ImhNoPredictiveText | Qt.ImhUrlCharactersOnly
            validator: RegExpValidator { regExp: /^\s*[A-Za-z0-9*._:\/!@-]+\s*$/ }
        }

        ComboBox {
            label: qsTr("Action")
            currentIndex: dialog.actions.indexOf(dialog.action)
            menu: ContextMenu {
                MenuItem { text: qsTr("through the proxy"); onClicked: dialog.action = "proxy" }
                MenuItem { text: qsTr("direct"); onClicked: dialog.action = "direct" }
                MenuItem { text: qsTr("block"); onClicked: dialog.action = "block" }
            }
        }
    }
}
