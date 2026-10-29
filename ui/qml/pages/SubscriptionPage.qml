import QtQuick 2.6
import Sailfish.Silica 1.0
import "../js/util.js" as Util

Page {
    id: page

    property string subId
    readonly property var sub: {
        for (var i = 0; i < app.subscriptions.length; i++)
            if (app.subscriptions[i].id === subId)
                return app.subscriptions[i]
        return null
    }
    readonly property var meta: sub ? sub.meta : ({})

    function patch(obj) {
        app.run("SetSubscription", [subId, JSON.stringify(obj)], app.refreshSubscriptions)
    }

    SilicaFlickable {
        anchors.fill: parent
        contentHeight: column.height + Theme.paddingLarge
        visible: !!page.sub

        Column {
            id: column
            width: parent.width

            PageHeader { title: page.sub ? Util.name(page.sub.name) : "" }

            TextField {
                width: parent.width
                label: qsTr("Name")
                text: page.sub ? page.sub.name : ""
                EnterKey.iconSource: "image://theme/icon-m-enter-accept"
                EnterKey.onClicked: { page.patch({ name: text }); focus = false }
            }

            SectionHeader { text: qsTr("Plan") }

            DetailItem { label: qsTr("Servers"); value: page.sub ? page.sub.count : "" }
            DetailItem { label: qsTr("Downloaded"); value: Util.bytes(page.meta.download) }
            DetailItem { label: qsTr("Uploaded"); value: Util.bytes(page.meta.upload) }
            DetailItem { label: qsTr("Limit"); value: page.meta.total > 0 ? Util.bytes(page.meta.total) : qsTr("unlimited") }
            DetailItem { label: qsTr("Expires"); value: page.meta.expire ? Util.date(page.meta.expire) : qsTr("never") }
            DetailItem { label: qsTr("Format"); value: page.meta.format || "" }
            DetailItem {
                label: qsTr("Device limit (HWID)")
                value: page.meta.hwidLimitReached ? qsTr("limit reached") : page.meta.hwidActive ? qsTr("active") : qsTr("not used")
            }

            Label {
                visible: !!page.meta.announce
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                color: Theme.highlightColor
                text: page.meta.announce || ""
            }

            Button {
                visible: !!page.meta.supportUrl
                anchors.horizontalCenter: parent.horizontalCenter
                text: qsTr("Support")
                onClicked: Qt.openUrlExternally(page.meta.supportUrl)
            }

            SectionHeader { text: qsTr("Updates") }

            TextSwitch {
                text: qsTr("Update automatically")
                description: qsTr("Every %n hour(s)", "", page.sub ? page.sub.effectiveIntervalHours : 24)
                checked: page.sub ? page.sub.autoUpdate : false
                automaticCheck: false
                onClicked: page.patch({ autoUpdate: !checked })
            }

            ComboBox {
                label: qsTr("Interval")
                currentIndex: {
                    var h = page.sub ? page.sub.intervalHours : 0
                    return h === 0 ? 0 : h === 1 ? 1 : h === 6 ? 2 : h === 12 ? 3 : h === 24 ? 4 : 0
                }
                menu: ContextMenu {
                    MenuItem { text: qsTr("As the panel says") }
                    MenuItem { text: qsTr("1 hour") }
                    MenuItem { text: qsTr("6 hours") }
                    MenuItem { text: qsTr("12 hours") }
                    MenuItem { text: qsTr("24 hours") }
                }
                onActivated: page.patch({ intervalHours: [0, 1, 6, 12, 24][index] })
            }

            TextSwitch {
                text: qsTr("Update through the tunnel")
                description: qsTr("Use when the panel is blocked by your provider. Falls back to a direct request.")
                checked: page.sub ? page.sub.viaProxy : false
                automaticCheck: false
                onClicked: page.patch({ viaProxy: !checked })
            }

            DetailItem { label: qsTr("Last update"); value: page.sub ? Util.date(page.sub.lastUpdate) : "" }
            Label {
                visible: page.sub && !!page.sub.lastError
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                font.pixelSize: Theme.fontSizeSmall
                color: Theme.errorColor
                text: page.sub ? page.sub.lastError || "" : ""
            }
        }

        VerticalScrollDecorator { }
    }
}
