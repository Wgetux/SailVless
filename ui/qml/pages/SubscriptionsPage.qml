import QtQuick 2.6
import Sailfish.Silica 1.0
import "../js/util.js" as Util

Page {
    id: page

    property int updating: 0

    function update(id) {
        updating++
        daemon.call("UpdateSubscription", [id], function (err) {
            updating--
            app.notify(err ? err : qsTr("Subscription updated"))
            app.refreshProfiles()
            app.refreshSubscriptions()
        }, 120000)
    }

    function usage(m) {
        var used = (m.upload || 0) + (m.download || 0)
        return m.total > 0 ? qsTr("%1 of %2").arg(Util.bytes(used)).arg(Util.bytes(m.total))
                           : qsTr("%1 used").arg(Util.bytes(used))
    }

    onStatusChanged: if (status === PageStatus.Active) app.refreshSubscriptions()

    SilicaListView {
        id: list
        anchors.fill: parent
        model: app.subscriptions

        header: PageHeader { title: qsTr("Subscriptions") }

        PullDownMenu {
            busy: page.updating > 0
            MenuItem {
                text: qsTr("Update all")
                visible: list.count > 0
                onClicked: {
                    for (var i = 0; i < app.subscriptions.length; i++)
                        page.update(app.subscriptions[i].id)
                }
            }
            MenuItem {
                text: qsTr("Add subscription")
                onClicked: pageStack.push(Qt.resolvedUrl("AddSubscriptionDialog.qml"))
            }
        }

        delegate: ListItem {
            id: item
            contentHeight: col.height + 2 * Theme.paddingMedium
            onClicked: pageStack.push(Qt.resolvedUrl("SubscriptionPage.qml"), { subId: modelData.id })

            menu: ContextMenu {
                MenuItem {
                    text: qsTr("Update")
                    onClicked: page.update(modelData.id)
                }
                MenuItem {
                    text: qsTr("Delete")
                    onClicked: {
                        var id = modelData.id
                        item.remorseDelete(function () {
                            app.run("DeleteSubscription", [id], function () { app.refreshSubscriptions(); app.refreshProfiles() })
                        })
                    }
                }
            }

            Column {
                id: col
                anchors.verticalCenter: parent.verticalCenter
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x

                Label {
                    width: parent.width
                    truncationMode: TruncationMode.Fade
                    color: item.highlighted ? Theme.highlightColor : Theme.primaryColor
                    text: Util.name(modelData.name)
                }
                Label {
                    width: parent.width
                    font.pixelSize: Theme.fontSizeExtraSmall
                    color: Theme.secondaryColor
                    text: qsTr("%n server(s)", "", modelData.count) + " · " + page.usage(modelData.meta)
                          + (modelData.meta.expire ? " · " + qsTr("until %1").arg(Util.dateOnly(modelData.meta.expire)) : "")
                }
                Label {
                    width: parent.width
                    wrapMode: Text.Wrap
                    font.pixelSize: Theme.fontSizeExtraSmall
                    color: modelData.lastError ? Theme.errorColor : Theme.secondaryColor
                    text: modelData.lastError ? modelData.lastError
                                              : qsTr("Updated %1").arg(Util.date(modelData.lastUpdate))
                }
            }
        }

        ViewPlaceholder {
            enabled: list.count === 0
            text: qsTr("No subscriptions")
            hintText: qsTr("Pull down to add one")
        }

        VerticalScrollDecorator { }
    }
}
