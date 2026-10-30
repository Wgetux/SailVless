import QtQuick 2.6
import Sailfish.Silica 1.0
import Sailfish.Pickers 1.0
import "../js/util.js" as Util

Dialog {
    id: dialog

    canAccept: text.text.trim().length > 0

    // a single new profile becomes the selected one: "Connect" then uses it
    function imported(err, result) {
        if (err) {
            app.notify(err)
            return
        }
        var r = Util.parse(result, {})
        var n = r.imported ? r.imported.length : 0
        var bad = r.errors ? r.errors.length : 0
        app.notify(bad > 0 ? qsTr("Imported: %1, failed: %2 (%3)").arg(n).arg(bad).arg(r.errors[0])
                           : qsTr("Imported: %1").arg(n))
        if (n === 1)
            app.run("SelectProfile", [r.imported[0]], app.refreshProfiles)
        else
            app.refreshProfiles()
    }

    onAccepted: daemon.call("ImportLinks", [text.text], imported)

    SilicaFlickable {
        anchors.fill: parent
        contentHeight: column.height

        Column {
            id: column
            width: parent.width

            DialogHeader {
                acceptText: qsTr("Import")
            }

            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                font.pixelSize: Theme.fontSizeSmall
                color: Theme.secondaryHighlightColor
                text: qsTr("One link per line: vless://, vmess://, trojan://, ss://, hysteria2://, hy2://, tuic://, vpn:// (AmneziaVPN, AmneziaWG only). Also accepted: a full Xray or sing-box JSON config, or an AmneziaWG/WireGuard .conf (paste the whole text).")
            }

            TextArea {
                id: text
                width: parent.width
                placeholderText: qsTr("Paste links here")
                label: qsTr("Links or JSON")
                inputMethodHints: Qt.ImhNoAutoUppercase | Qt.ImhNoPredictiveText
            }

            ButtonLayout {
                Button {
                    text: qsTr("Paste from clipboard")
                    enabled: Clipboard.hasText
                    onClicked: text.text = Clipboard.text
                }
                Button {
                    text: qsTr("Choose files")
                    // after picking, return past this dialog straight to where import was opened.
                    // The picker derives acceptDestination from _maskedAcceptDestination and
                    // hands it to its directory pages; setting acceptDestination directly
                    // breaks that and the picker cannot open folders.
                    onClicked: pageStack.push(filePicker, {
                        _maskedAcceptDestination: pageStack.previousPage(dialog),
                        acceptDestinationAction: PageStackAction.Pop
                    })
                }
            }
        }
    }

    // files are read by the daemon itself (one call, several files at once)
    Component {
        id: filePicker
        MultiFilePickerDialog {
            title: qsTr("Config files")
            nameFilters: ["*.conf", "*.txt", "*.json", "*.yaml"]
            onAccepted: {
                var paths = []
                for (var i = 0; i < selectedContent.count; i++)
                    paths.push(selectedContent.get(i).filePath)
                daemon.call("ImportFiles", [JSON.stringify(paths)], dialog.imported)
            }
        }
    }

    Component.onCompleted: {
        // prefill only with something that looks like links or a config
        var c = Clipboard.hasText ? Clipboard.text.trim() : ""
        if (c.indexOf("://") > 0 || c.charAt(0) === "{" || c.charAt(0) === "[" || c.indexOf("[Interface]") >= 0)
            text.text = c
    }
}
