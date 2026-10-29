import QtQuick 2.6
import Sailfish.Silica 1.0
import "pages"
import "js/util.js" as Util

ApplicationWindow {
    id: app

    property var status: ({ state: "stopped" })
    property var profiles: []
    property var subscriptions: []
    property bool busy: false

    readonly property bool connected: status.state === "connected"
    readonly property bool active: status.state === "connected" || status.state === "connecting"
    readonly property var selected: {
        for (var i = 0; i < profiles.length; i++)
            if (profiles[i].selected)
                return profiles[i]
        return null
    }

    function notify(text) {
        notice.text = text
        notice.show()
    }

    // run a daemon call; shows the error as a notice, calls onOk(result) on success
    function run(method, args, onOk, timeoutMs) {
        daemon.call(method, args || [], function (err, result) {
            if (err) {
                notify(err)
                return
            }
            if (onOk)
                onOk(result)
        }, timeoutMs || 30000)
    }

    function refreshStatus() {
        daemon.call("Status", [], function (err, result) {
            if (!err)
                status = Util.parse(result, status)
        })
    }

    function refreshProfiles() {
        daemon.call("ListProfiles", [], function (err, result) {
            if (!err)
                profiles = Util.parse(result, []) || []
        })
    }

    function refreshSubscriptions() {
        daemon.call("ListSubscriptions", [], function (err, result) {
            if (!err)
                subscriptions = Util.parse(result, []) || []
        })
    }

    function refreshAll() {
        refreshStatus()
        refreshProfiles()
        refreshSubscriptions()
    }

    function toggle() {
        if (active) {
            run("Disconnect")
        } else if (!selected) {
            notify(qsTr("Add a server first"))
        } else {
            status = { state: "connecting" }
            run("Connect", [""], null, 180000)
        }
    }

    function subscriptionName(id) {
        for (var i = 0; i < subscriptions.length; i++)
            if (subscriptions[i].id === id)
                return subscriptions[i].name
        return ""
    }

    initialPage: Component { MainPage { } }
    cover: Qt.resolvedUrl("cover/CoverPage.qml")
    allowedOrientations: defaultAllowedOrientations

    Notice {
        id: notice
        duration: 4000
    }

    Connections {
        target: daemon
        onStateChanged: app.status = Util.parse(statusJson, app.status)
        onAvailableChanged: if (daemon.available) app.refreshAll()
    }

    // traffic counters are only polled while somebody looks at them
    Timer {
        interval: 1000
        repeat: true
        running: app.connected && Qt.application.active
        onTriggered: app.refreshStatus()
    }

    Component.onCompleted: refreshAll()
}
