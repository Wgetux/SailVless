import QtQuick 2.6
import Sailfish.Silica 1.0
import "../js/util.js" as Util

Page {
    id: page

    property bool sortByPing: false
    property bool pinging: false

    readonly property int noPing: -3   // not measured yet

    function pingKey(ms) {
        return ms === noPing ? 1e9 : ms < 0 ? 1e8 - ms : ms
    }

    function refill() {
        var list = []
        for (var i = 0; i < app.profiles.length; i++) {
            var p = app.profiles[i]
            list.push({
                id: p.id, name: p.name, selected: p.selected, subId: p.subId || "",
                desc: Util.describe(p) + " · " + p.core,
                ping: p.ping === undefined ? noPing : p.ping,
                section: p.subId ? app.subscriptionName(p.subId) : qsTr("Added manually")
            })
        }
        list.sort(function (a, b) {
            if (a.section !== b.section)
                return a.section < b.section ? -1 : 1
            if (sortByPing && pingKey(a.ping) !== pingKey(b.ping))
                return pingKey(a.ping) - pingKey(b.ping)
            return a.name < b.name ? -1 : a.name > b.name ? 1 : 0
        })
        model.clear()
        for (var j = 0; j < list.length; j++)
            model.append(list[j])
    }

    onSortByPingChanged: refill()
    Connections {
        target: app
        onProfilesChanged: page.refill()
        onSubscriptionsChanged: page.refill()
    }
    Component.onCompleted: refill()

    function pingAll() {
        pinging = true
        daemon.call("PingAll", [], function (err) {
            pinging = false
            if (err)
                app.notify(err)
            app.refreshProfiles()
        }, 60000)
    }

    function connectTo(id) {
        app.status = { state: "connecting" }
        app.run("Connect", [id], app.refreshProfiles, 180000)
    }

    ListModel { id: model }

    SilicaListView {
        id: list
        anchors.fill: parent
        model: model

        header: PageHeader { title: qsTr("Servers") }

        PullDownMenu {
            busy: page.pinging
            MenuItem {
                text: page.sortByPing ? qsTr("Sort by name") : qsTr("Sort by ping")
                onClicked: page.sortByPing = !page.sortByPing
            }
            MenuItem {
                text: qsTr("Connect to the fastest")
                onClicked: {
                    app.status = { state: "connecting" }
                    daemon.call("ConnectBest", [], function (err) {
                        if (err)
                            app.notify(err)
                        app.refreshAll()
                    }, 180000)
                    pageStack.pop()
                }
            }
            MenuItem {
                text: qsTr("Ping all")
                onClicked: page.pingAll()
            }
        }

        section.property: "section"
        section.delegate: SectionHeader { text: section }

        delegate: ListItem {
            id: item
            contentHeight: Theme.itemSizeMedium
            highlighted: down || model.selected

            onClicked: {
                if (app.active)
                    page.connectTo(model.id)
                else
                    app.run("SelectProfile", [model.id], app.refreshProfiles)
                pageStack.pop()
            }

            menu: ContextMenu {
                MenuItem {
                    text: qsTr("Connect")
                    onClicked: page.connectTo(model.id)
                }
                MenuItem {
                    text: qsTr("Delete")
                    visible: model.subId === ""
                    onClicked: {
                        var id = model.id
                        item.remorseDelete(function () { app.run("DeleteProfile", [id], app.refreshProfiles) })
                    }
                }
            }

            Column {
                anchors.verticalCenter: parent.verticalCenter
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x - pingLabel.width - Theme.paddingMedium

                Label {
                    width: parent.width
                    truncationMode: TruncationMode.Fade
                    color: item.highlighted ? Theme.highlightColor : Theme.primaryColor
                    text: Util.name(model.name)
                }
                Label {
                    width: parent.width
                    truncationMode: TruncationMode.Fade
                    font.pixelSize: Theme.fontSizeExtraSmall
                    color: Theme.secondaryColor
                    text: model.desc
                }
            }

            Label {
                id: pingLabel
                anchors { right: parent.right; rightMargin: Theme.horizontalPageMargin; verticalCenter: parent.verticalCenter }
                font.pixelSize: Theme.fontSizeSmall
                color: model.ping === -1 ? Theme.errorColor
                     : model.ping < 0 ? Theme.secondaryColor
                     : model.ping > 300 ? Theme.secondaryHighlightColor : Theme.highlightColor
                text: model.ping === page.noPing ? ""
                    : model.ping === -1 ? qsTr("timeout")
                    : model.ping === -2 ? "UDP"
                    : model.ping + " " + qsTr("ms")
            }
        }

        ViewPlaceholder {
            enabled: list.count === 0
            text: qsTr("No servers")
            hintText: qsTr("Add a subscription or import a link from the main page")
        }

        VerticalScrollDecorator { }
    }
}
