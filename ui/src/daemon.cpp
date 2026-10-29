#include "daemon.h"

#include <QDBusConnection>
#include <QDBusConnectionInterface>
#include <QDBusMessage>
#include <QDBusPendingCallWatcher>
#include <QDBusPendingReply>
#include <QDBusServiceWatcher>
#include <QJsonArray>
#include <QJsonDocument>

static const char *Service = "org.sailvless.Daemon";
static const char *Path = "/org/sailvless/Daemon";
static const char *Interface = "org.sailvless.Daemon1";

Daemon::Daemon(QObject *parent)
    : QObject(parent)
    , m_available(false)
{
    QDBusConnection bus = QDBusConnection::sessionBus();
    bus.connect(Service, Path, Interface, "StateChanged", this, SLOT(onStateChanged(QString)));

    m_watcher = new QDBusServiceWatcher(Service, bus, QDBusServiceWatcher::WatchForOwnerChange, this);
    connect(m_watcher, &QDBusServiceWatcher::serviceRegistered, this, [this]() { setAvailable(true); });
    connect(m_watcher, &QDBusServiceWatcher::serviceUnregistered, this, [this]() { setAvailable(false); });
    setAvailable(bus.interface()->isServiceRegistered(Service));
    if (!m_available) {
        // D-Bus activation starts sailvlessd.service on demand
        bus.interface()->startService(Service);
    }
}

void Daemon::call(const QString &method, const QVariantList &args, const QJSValue &callback, int timeoutMs)
{
    QDBusMessage msg = QDBusMessage::createMethodCall(Service, Path, Interface, method);
    msg.setArguments(args);
    QDBusPendingCall pending = QDBusConnection::sessionBus().asyncCall(msg, timeoutMs);
    QDBusPendingCallWatcher *w = new QDBusPendingCallWatcher(pending, this);
    connect(w, &QDBusPendingCallWatcher::finished, this, [callback](QDBusPendingCallWatcher *w) {
        w->deleteLater();
        if (!callback.isCallable())
            return;
        QDBusMessage reply = w->reply();
        QString error, result;
        if (reply.type() == QDBusMessage::ErrorMessage) {
            // daemon errors carry a human-readable message; bus errors their own text
            error = reply.errorMessage().isEmpty() ? reply.errorName() : reply.errorMessage();
            if (error.isEmpty())
                error = QStringLiteral("D-Bus error");
        } else {
            const QList<QVariant> out = reply.arguments();
            if (out.size() == 1) {
                result = out.first().toString();
            } else if (out.size() > 1) {
                QJsonArray arr;
                for (const QVariant &v : out)
                    arr.append(QJsonValue::fromVariant(v));
                result = QString::fromUtf8(QJsonDocument(arr).toJson(QJsonDocument::Compact));
            }
        }
        QJSValue cb(callback);
        cb.call(QJSValueList() << QJSValue(error) << QJSValue(result));
    });
}

void Daemon::onStateChanged(const QString &statusJson)
{
    emit stateChanged(statusJson);
}

void Daemon::setAvailable(bool available)
{
    if (m_available == available)
        return;
    m_available = available;
    emit availableChanged();
}
