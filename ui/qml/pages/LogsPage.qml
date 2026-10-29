import QtQuick 2.6
import Sailfish.Silica 1.0
import "../js/util.js" as Util

Page {
    id: page

    property string logText: ""
    property string level: "warning"

    function load() {
        daemon.call("Logs", [500], function (err, r) {
            if (!err) {
                var atEnd = flick.atYEnd
                logText = r
                if (atEnd)
                    flick.scrollToBottom()
            }
        })
    }

    Component.onCompleted: {
        load()
        daemon.call("GetSettings", [], function (err, r) {
            if (err)
                return
            page.level = Util.parse(r, {}).logLevel || "warning"
            // the Silica 5.1 ComboBox ignores later changes of a bound currentIndex
            combo.currentIndex = Math.max(0, combo.values.indexOf(page.level))
        })
    }

    Timer {
        interval: 2000
        repeat: true
        running: page.status === PageStatus.Active && Qt.application.active
        onTriggered: page.load()
    }

    SilicaFlickable {
        id: flick
        anchors.fill: parent
        contentHeight: column.height

        PullDownMenu {
            MenuItem {
                text: qsTr("Copy all")
                onClicked: { Clipboard.text = page.logText; app.notify(qsTr("Copied")) }
            }
            MenuItem {
                text: qsTr("Refresh")
                onClicked: page.load()
            }
        }

        Column {
            id: column
            width: parent.width

            PageHeader { title: qsTr("Logs") }

            ComboBox {
                id: combo
                readonly property var values: ["debug", "info", "warning", "error", "none"]
                label: qsTr("Core log level")
                description: qsTr("Applied on the next connect")
                currentIndex: Math.max(0, values.indexOf(page.level))
                menu: ContextMenu {
                    Repeater {
                        model: combo.values
                        MenuItem {
                            text: modelData
                            onClicked: app.run("SetSettings", [JSON.stringify({ logLevel: modelData })])
                        }
                    }
                }
            }

            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.WrapAnywhere
                font.family: "monospace"
                font.pixelSize: Theme.fontSizeTiny
                color: Theme.secondaryColor
                textFormat: Text.PlainText
                text: page.logText || qsTr("No log yet")
            }
        }

        VerticalScrollDecorator { }
    }
}
