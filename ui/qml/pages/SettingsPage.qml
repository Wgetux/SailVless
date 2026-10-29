import QtQuick 2.6
import Sailfish.Silica 1.0
import "../js/util.js" as Util

Page {
    id: page

    property var settings: ({})
    property var creds: ["", ""]
    property var hwid: ({})
    property bool geoBusy: false

    readonly property bool exposed: settings.listen !== undefined && settings.listen !== "127.0.0.1" && settings.listen !== "::1"

    function load() {
        daemon.call("GetSettings", [], function (err, r) { if (!err) settings = Util.parse(r, {}) })
        daemon.call("GetProxyCredentials", [], function (err, r) { if (!err) creds = Util.parse(r, ["", ""]) })
        daemon.call("GetHWID", [], function (err, r) { if (!err) hwid = Util.parse(r, {}) })
    }

    function patch(obj) {
        app.run("SetSettings", [JSON.stringify(obj)], function () { load(); app.refreshStatus() })
    }

    function proxyUrl(scheme, port) {
        var auth = settings.authEnabled ? encodeURIComponent(creds[0]) + ":" + encodeURIComponent(creds[1]) + "@" : ""
        return scheme + "://" + auth + settings.listen + ":" + port
    }

    Component.onCompleted: load()

    RemorsePopup { id: remorse }

    SilicaFlickable {
        anchors.fill: parent
        contentHeight: column.height + Theme.paddingLarge

        Column {
            id: column
            width: parent.width

            PageHeader { title: qsTr("Settings") }

            SectionHeader { text: qsTr("Local proxy") }

            TextSwitch {
                text: qsTr("Require login and password")
                description: qsTr("Other apps on the phone cannot use the proxy without credentials")
                checked: !!page.settings.authEnabled
                automaticCheck: false
                onClicked: page.patch({ authEnabled: !checked })
            }

            DetailItem { visible: !!page.settings.authEnabled; label: qsTr("Login"); value: page.creds[0] }
            PasswordField {
                visible: !!page.settings.authEnabled
                width: parent.width
                readOnly: true
                label: qsTr("Password")
                text: page.creds[1]
            }

            ButtonLayout {
                Button {
                    visible: !!page.settings.authEnabled
                    text: qsTr("New password")
                    onClicked: app.run("RegenerateCredentials", [], function () { page.load(); app.notify(qsTr("New credentials generated")) })
                }
                Button {
                    text: qsTr("Copy SOCKS5 URL")
                    onClicked: {
                        Clipboard.text = page.proxyUrl("socks5h", page.settings.socksPort)
                        app.notify(qsTr("Copied"))
                    }
                }
            }

            TextField {
                width: parent.width
                label: qsTr("SOCKS5 port")
                text: page.settings.socksPort || ""
                inputMethodHints: Qt.ImhDigitsOnly
                validator: IntValidator { bottom: 1024; top: 65535 }
                EnterKey.iconSource: "image://theme/icon-m-enter-accept"
                EnterKey.onClicked: { page.patch({ socksPort: parseInt(text) }); focus = false }
            }
            TextField {
                width: parent.width
                label: qsTr("HTTP port (0 = off)")
                text: page.settings.httpPort !== undefined ? page.settings.httpPort : ""
                inputMethodHints: Qt.ImhDigitsOnly
                validator: IntValidator { bottom: 0; top: 65535 }
                EnterKey.iconSource: "image://theme/icon-m-enter-accept"
                EnterKey.onClicked: { page.patch({ httpPort: parseInt(text) }); focus = false }
            }
            Button {
                anchors.horizontalCenter: parent.horizontalCenter
                text: qsTr("Random ports")
                onClicked: app.run("RandomizePorts", [], function () { page.load(); app.refreshStatus() })
            }

            TextSwitch {
                text: qsTr("Allow other devices in the network")
                description: page.exposed
                             ? qsTr("WARNING: the proxy is reachable from your Wi-Fi/LAN. Anyone there can use your subscription if authentication is off.")
                             : qsTr("Listens on 127.0.0.1 only (recommended)")
                checked: page.exposed
                automaticCheck: false
                onClicked: {
                    if (checked)
                        page.patch({ listen: "127.0.0.1" })
                    else
                        remorse.execute(qsTr("Opening proxy to the network"), function () { page.patch({ listen: "0.0.0.0" }) })
                }
            }

            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                font.pixelSize: Theme.fontSizeExtraSmall
                color: Theme.secondaryHighlightColor
                text: qsTr("The Sailfish system proxy (Settings → Network) has no login/password fields, so it cannot be used together with authentication. Configure the proxy URL in each app, or use the TUN mode (planned) for system-wide routing.")
            }

            SectionHeader { text: qsTr("Subscriptions") }

            TextField {
                width: parent.width
                label: qsTr("User-Agent")
                text: page.settings.userAgent || ""
                inputMethodHints: Qt.ImhNoAutoUppercase | Qt.ImhNoPredictiveText
                EnterKey.iconSource: "image://theme/icon-m-enter-accept"
                EnterKey.onClicked: { page.patch({ userAgent: text }); focus = false }
            }
            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                font.pixelSize: Theme.fontSizeExtraSmall
                color: Theme.secondaryHighlightColor
                text: qsTr("Panels choose the response format by User-Agent: e.g. \"Happ/3.0.0\" returns Xray JSON with routing rules, the default returns plain links.")
            }

            TextSwitch {
                text: qsTr("Send device ID (HWID)")
                description: qsTr("Required by panels with a device limit (Remnawave)")
                checked: !!page.hwid.enabled
                automaticCheck: false
                onClicked: page.patch({ hwidEnabled: !checked })
            }
            DetailItem { label: "HWID"; value: page.hwid.hwid || "" }
            DetailItem { label: qsTr("Device"); value: page.hwid.device ? page.hwid.device.model + " · " + page.hwid.device.os + " " + page.hwid.device.version : "" }
            TextField {
                width: parent.width
                label: qsTr("Manual HWID (empty = automatic)")
                placeholderText: qsTr("Manual HWID (empty = automatic)")
                text: page.settings.hwidManual || ""
                inputMethodHints: Qt.ImhNoAutoUppercase | Qt.ImhNoPredictiveText
                EnterKey.iconSource: "image://theme/icon-m-enter-accept"
                EnterKey.onClicked: { page.patch({ hwidManual: text.trim() }); focus = false }
            }
            ButtonLayout {
                Button {
                    text: qsTr("Copy HWID")
                    onClicked: { Clipboard.text = page.hwid.hwid; app.notify(qsTr("Copied")) }
                }
                Button {
                    text: qsTr("Reset HWID")
                    onClicked: remorse.execute(qsTr("The panel will see a new device"), function () {
                        app.run("ResetHWID", [], page.load)
                    })
                }
            }

            SectionHeader { text: qsTr("Core") }

            ComboBox {
                id: logLevel
                readonly property var values: ["debug", "info", "warning", "error", "none"]
                label: qsTr("Log level")
                currentIndex: Math.max(0, values.indexOf(page.settings.logLevel || "warning"))
                menu: ContextMenu {
                    Repeater {
                        model: logLevel.values
                        MenuItem {
                            text: modelData
                            onClicked: page.patch({ logLevel: modelData })
                        }
                    }
                }
            }

            Button {
                anchors.horizontalCenter: parent.horizontalCenter
                text: page.geoBusy ? qsTr("Downloading…") : qsTr("Update geo databases")
                enabled: !page.geoBusy
                onClicked: {
                    page.geoBusy = true
                    daemon.call("UpdateGeo", [], function (err) {
                        page.geoBusy = false
                        app.notify(err ? err : qsTr("Geo databases updated"))
                    }, 600000)
                }
            }

            SectionHeader { text: qsTr("About") }
            DetailItem { label: qsTr("Version"); value: appVersion }
        }

        VerticalScrollDecorator { }
    }
}
