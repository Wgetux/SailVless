.pragma library

// Sailfish fonts have no flag emoji: turn regional-indicator pairs (🇵🇱) into "PL"
function name(s) {
    if (!s)
        return ""
    return s.replace(/\uD83C([\uDDE6-\uDDFF])\uD83C([\uDDE6-\uDDFF])/g, function (m, a, b) {
        return String.fromCharCode(a.charCodeAt(0) - 0xDDE6 + 65) + String.fromCharCode(b.charCodeAt(0) - 0xDDE6 + 65)
    }).replace(/^([A-Z]{2})(?=\S)/, "$1 ")
}

function parse(json, fallback) {
    if (!json)
        return fallback
    try {
        return JSON.parse(json)
    } catch (e) {
        return fallback
    }
}

function bytes(n) {
    if (n === undefined || n === null)
        return "–"
    var units = ["B", "KB", "MB", "GB", "TB"]
    var i = 0
    while (n >= 1024 && i < units.length - 1) {
        n /= 1024
        i++
    }
    return (i === 0 ? n : n.toFixed(n < 10 ? 2 : 1)) + " " + units[i]
}

function rate(n) {
    return bytes(n) + "/s"
}

function pad(n) {
    return n < 10 ? "0" + n : "" + n
}

function duration(sec) {
    if (sec < 0)
        sec = 0
    var h = Math.floor(sec / 3600), m = Math.floor(sec % 3600 / 60), s = Math.floor(sec % 60)
    return (h > 0 ? h + ":" : "") + pad(m) + ":" + pad(s)
}

function date(unix) {
    if (!unix)
        return ""
    return Qt.formatDateTime(new Date(unix * 1000), "dd.MM.yyyy hh:mm")
}

function dateOnly(unix) {
    if (!unix)
        return ""
    return Qt.formatDate(new Date(unix * 1000), "dd.MM.yyyy")
}

// short description of a profile: "vless · xhttp · reality"
function describe(p) {
    if (p.protocol === "xray-json")
        return "Xray JSON"
    if (p.protocol === "singbox-json")
        return "sing-box · " + p.transport.type
    var parts = [p.protocol]
    if (p.transport && p.transport.type && p.transport.type !== "tcp" && p.transport.type !== "quic")
        parts.push(p.transport.type)
    if (p.security && p.security.type && p.security.type !== "none" && p.protocol !== "hysteria2" && p.protocol !== "tuic")
        parts.push(p.security.type)
    return parts.join(" · ")
}
