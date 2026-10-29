import QtQuick 2.6
import Sailfish.Silica 1.0
import "../js/util.js" as Util

Dialog {
    id: dialog

    canAccept: text.text.trim().length > 0

    onAccepted: {
        daemon.call("ImportLinks", [text.text], function (err, result) {
            if (err) {
                app.notify(err)
                return
            }
            var r = Util.parse(result, {})
            var n = r.imported ? r.imported.length : 0
            var bad = r.errors ? r.errors.length : 0
            app.notify(bad > 0 ? qsTr("Imported: %1, failed: %2 (%3)").arg(n).arg(bad).arg(r.errors[0])
                               : qsTr("Imported: %1").arg(n))
            app.refreshProfiles()
        })
    }

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
                text: qsTr("One link per line: vless://, vmess://, trojan://, ss://, hysteria2://, hy2://, tuic://. A full Xray or sing-box JSON config is also accepted.")
            }

            TextArea {
                id: text
                width: parent.width
                placeholderText: qsTr("Paste links here")
                label: qsTr("Links or JSON")
                inputMethodHints: Qt.ImhNoAutoUppercase | Qt.ImhNoPredictiveText
            }

            Button {
                anchors.horizontalCenter: parent.horizontalCenter
                text: qsTr("Paste from clipboard")
                enabled: Clipboard.hasText
                onClicked: text.text = Clipboard.text
            }
        }
    }

    Component.onCompleted: {
        // prefill only with something that looks like links or a config
        var c = Clipboard.hasText ? Clipboard.text.trim() : ""
        if (c.indexOf("://") > 0 || c.charAt(0) === "{" || c.charAt(0) === "[")
            text.text = c
    }
}
