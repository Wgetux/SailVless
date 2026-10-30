import QtQuick 2.6
import Sailfish.Silica 1.0
import Sailfish.Pickers 1.0
import "../js/util.js" as Util

// Export/import of a rule set as a file (~/Documents/SailVless), e.g. to
// restore it after reinstalling. profileId "" = the common rules.
Item {
    id: rulesFile

    property string profileId
    signal imported()

    function exportRules() {
        daemon.call("ExportRules", [rulesFile.profileId], function (err, path) {
            app.notify(err ? err : qsTr("Saved to %1").arg(path.replace(/^\/home\/[^\/]+\//, "~/")))
        })
    }

    function importRules() {
        pageStack.push(picker)
    }

    Component {
        id: picker
        FilePickerPage {
            title: qsTr("Rules file")
            nameFilters: ["*.json"]
            onSelectedContentPropertiesChanged: {
                daemon.call("ImportRules", [rulesFile.profileId, selectedContentProperties.filePath], function (err, result) {
                    if (err) {
                        app.notify(err)
                        return
                    }
                    var r = Util.parse(result, {})
                    app.notify(qsTr("Rules added: %1, already there: %2").arg(r.added).arg(r.skipped))
                    rulesFile.imported()
                })
            }
        }
    }
}
