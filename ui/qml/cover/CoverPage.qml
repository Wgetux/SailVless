import QtQuick 2.6
import Sailfish.Silica 1.0
import "../js/util.js" as Util

CoverBackground {
    id: cover

    property int now: Date.now() / 1000

    Timer {
        interval: 1000
        repeat: true
        running: app.connected && cover.status === Cover.Active
        onTriggered: cover.now = Date.now() / 1000
    }

    Column {
        anchors.centerIn: parent
        width: parent.width - 2 * Theme.paddingLarge
        spacing: Theme.paddingMedium

        Image {
            anchors.horizontalCenter: parent.horizontalCenter
            source: "/usr/share/icons/hicolor/86x86/apps/sailvless.png"
            opacity: app.connected ? 1.0 : 0.4
        }

        Label {
            width: parent.width
            horizontalAlignment: Text.AlignHCenter
            color: app.connected ? Theme.highlightColor : Theme.primaryColor
            font.pixelSize: Theme.fontSizeLarge
            text: {
                switch (app.status.state) {
                case "connected": return qsTr("On")
                case "connecting": return qsTr("Connecting…")
                case "error": return qsTr("Error")
                default: return qsTr("Off")
                }
            }
        }

        Label {
            width: parent.width
            horizontalAlignment: Text.AlignHCenter
            visible: app.connected && !!app.status.since
            font.pixelSize: Theme.fontSizeSmall
            text: Util.duration(cover.now - app.status.since)
        }

        Label {
            width: parent.width
            horizontalAlignment: Text.AlignHCenter
            truncationMode: TruncationMode.Fade
            font.pixelSize: Theme.fontSizeExtraSmall
            color: Theme.secondaryColor
            text: app.status.profileName || (app.selected ? app.selected.name : "")
        }
    }

    CoverActionList {
        CoverAction {
            iconSource: app.active ? "image://theme/icon-cover-pause" : "image://theme/icon-cover-play"
            onTriggered: app.toggle()
        }
    }
}
