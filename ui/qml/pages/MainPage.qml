import QtQuick 2.6
import Sailfish.Silica 1.0
import "../js/util.js" as Util

Page {
    id: page

    property int now: Date.now() / 1000

    onStatusChanged: if (status === PageStatus.Active) { app.refreshProfiles(); app.refreshSubscriptions() }

    Timer {
        interval: 1000
        repeat: true
        running: app.connected && page.status === PageStatus.Active && Qt.application.active
        onTriggered: page.now = Date.now() / 1000
    }

    SilicaFlickable {
        anchors.fill: parent
        contentHeight: column.height + Theme.paddingLarge

        PullDownMenu {
            MenuItem {
                text: qsTr("Settings")
                onClicked: pageStack.push(Qt.resolvedUrl("SettingsPage.qml"))
            }
            MenuItem {
                text: qsTr("Logs")
                onClicked: pageStack.push(Qt.resolvedUrl("LogsPage.qml"))
            }
            MenuItem {
                text: qsTr("Subscriptions")
                onClicked: pageStack.push(Qt.resolvedUrl("SubscriptionsPage.qml"))
            }
            MenuItem {
                text: qsTr("Import links")
                onClicked: pageStack.push(Qt.resolvedUrl("ImportDialog.qml"))
            }
        }

        Column {
            id: column
            width: parent.width
            spacing: Theme.paddingLarge

            PageHeader { title: "SailVless" }

            InfoLabel {
                visible: !daemon.available
                text: qsTr("Background service is not running")
            }

            // the big button
            Item {
                width: parent.width
                height: Theme.itemSizeHuge * 2

                Rectangle {
                    id: ring
                    anchors.centerIn: parent
                    width: Math.min(parent.width * 0.6, Theme.itemSizeHuge * 2)
                    height: width
                    radius: width / 2
                    color: "transparent"
                    border.width: Theme.paddingSmall
                    border.color: app.connected ? Theme.highlightColor
                                : app.status.state === "error" ? Theme.errorColor
                                : Theme.rgba(Theme.primaryColor, 0.4)

                    Rectangle {
                        anchors.fill: parent
                        anchors.margins: Theme.paddingLarge
                        radius: width / 2
                        color: Theme.rgba(app.connected ? Theme.highlightBackgroundColor : Theme.primaryColor,
                                          mouse.pressed ? 0.45 : (app.connected ? 0.3 : 0.1))
                    }

                    BusyIndicator {
                        anchors.centerIn: parent
                        size: BusyIndicatorSize.Large
                        running: app.status.state === "connecting"
                    }

                    Label {
                        anchors.centerIn: parent
                        width: parent.width - 2 * Theme.paddingLarge * 2
                        horizontalAlignment: Text.AlignHCenter
                        wrapMode: Text.Wrap
                        visible: app.status.state !== "connecting"
                        font.pixelSize: Theme.fontSizeLarge
                        color: app.connected ? Theme.highlightColor : Theme.primaryColor
                        text: app.active ? qsTr("Disconnect") : qsTr("Connect")
                    }

                    MouseArea {
                        id: mouse
                        anchors.fill: parent
                        onClicked: app.toggle()
                    }
                }
            }

            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                horizontalAlignment: Text.AlignHCenter
                font.pixelSize: Theme.fontSizeLarge
                color: Theme.highlightColor
                text: {
                    switch (app.status.state) {
                    case "connected": return qsTr("Connected") + (app.status.since ? " · " + Util.duration(page.now - app.status.since) : "")
                    case "connecting": return qsTr("Connecting…")
                    case "error": return qsTr("Error")
                    default: return qsTr("Disconnected")
                    }
                }
            }

            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                visible: !!app.status.error
                wrapMode: Text.Wrap
                font.pixelSize: Theme.fontSizeExtraSmall
                color: Theme.errorColor
                text: app.status.error || ""
            }

            // traffic
            Row {
                anchors.horizontalCenter: parent.horizontalCenter
                spacing: Theme.paddingLarge * 2
                visible: app.connected && !!app.status.traffic

                Column {
                    Label {
                        anchors.horizontalCenter: parent.horizontalCenter
                        text: "↓ " + Util.rate(app.status.traffic ? app.status.traffic.downRate : 0)
                    }
                    Label {
                        anchors.horizontalCenter: parent.horizontalCenter
                        font.pixelSize: Theme.fontSizeExtraSmall
                        color: Theme.secondaryColor
                        text: Util.bytes(app.status.traffic ? app.status.traffic.down : 0)
                    }
                }
                Column {
                    Label {
                        anchors.horizontalCenter: parent.horizontalCenter
                        text: "↑ " + Util.rate(app.status.traffic ? app.status.traffic.upRate : 0)
                    }
                    Label {
                        anchors.horizontalCenter: parent.horizontalCenter
                        font.pixelSize: Theme.fontSizeExtraSmall
                        color: Theme.secondaryColor
                        text: Util.bytes(app.status.traffic ? app.status.traffic.up : 0)
                    }
                }
            }

            SectionHeader { text: qsTr("Server") }

            BackgroundItem {
                width: parent.width
                height: Theme.itemSizeMedium
                onClicked: pageStack.push(Qt.resolvedUrl("ServersPage.qml"))

                Column {
                    anchors.verticalCenter: parent.verticalCenter
                    x: Theme.horizontalPageMargin
                    width: parent.width - 2 * x - chevron.width

                    Label {
                        width: parent.width
                        truncationMode: TruncationMode.Fade
                        color: highlighted ? Theme.highlightColor : Theme.primaryColor
                        text: app.selected ? app.selected.name : qsTr("No servers yet")
                    }
                    Label {
                        width: parent.width
                        truncationMode: TruncationMode.Fade
                        font.pixelSize: Theme.fontSizeExtraSmall
                        color: Theme.secondaryColor
                        text: app.selected ? Util.describe(app.selected) + (app.selected.subId ? " · " + app.subscriptionName(app.selected.subId) : "")
                                           : qsTr("Pull down to add a subscription or import a link")
                    }
                }
                Image {
                    id: chevron
                    anchors { right: parent.right; rightMargin: Theme.horizontalPageMargin; verticalCenter: parent.verticalCenter }
                    source: "image://theme/icon-m-right"
                }
            }

            SectionHeader { text: qsTr("Local proxy") }

            DetailItem {
                label: "SOCKS5"
                value: app.status.listen + ":" + app.status.socksPort
            }
            DetailItem {
                visible: app.status.httpPort > 0
                label: "HTTP"
                value: app.status.listen + ":" + app.status.httpPort
            }
            DetailItem {
                label: qsTr("Authentication")
                value: app.status.auth ? qsTr("login and password") : qsTr("disabled")
            }
        }
    }
}
