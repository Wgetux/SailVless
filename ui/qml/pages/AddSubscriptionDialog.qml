import QtQuick 2.6
import Sailfish.Silica 1.0
import "../js/util.js" as Util

Dialog {
    id: dialog

    canAccept: /^https?:\/\/\S+$/.test(url.text.trim())

    onAccepted: {
        app.notify(qsTr("Downloading subscription…"))
        daemon.call("AddSubscription", [url.text.trim(), name.text.trim()], function (err, result) {
            var r = Util.parse(result, {})
            if (err || r.error)
                app.notify(err || r.error)
            else
                app.notify(qsTr("Subscription added"))
            app.refreshSubscriptions()
            app.refreshProfiles()
        }, 120000)
    }

    Column {
        width: parent.width

        DialogHeader { acceptText: qsTr("Add") }

        TextField {
            id: url
            width: parent.width
            label: qsTr("Subscription URL")
            placeholderText: "https://"
            inputMethodHints: Qt.ImhUrlCharactersOnly | Qt.ImhNoAutoUppercase | Qt.ImhNoPredictiveText
            EnterKey.iconSource: "image://theme/icon-m-enter-next"
            EnterKey.onClicked: name.focus = true
        }

        TextField {
            id: name
            width: parent.width
            label: qsTr("Name (optional)")
            placeholderText: qsTr("Name (optional, taken from the panel)")
        }

        Label {
            x: Theme.horizontalPageMargin
            width: parent.width - 2 * x
            wrapMode: Text.Wrap
            font.pixelSize: Theme.fontSizeExtraSmall
            color: Theme.secondaryHighlightColor
            text: qsTr("The device ID (HWID) is sent if enabled in Settings. Panels with a device limit count this phone as one device.")
        }
    }

    Component.onCompleted: {
        var c = Clipboard.hasText ? Clipboard.text.trim() : ""
        if (/^https?:\/\/\S+$/.test(c))
            url.text = c
    }
}
