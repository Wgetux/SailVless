#ifndef SAILVLESS_DAEMON_H
#define SAILVLESS_DAEMON_H

#include <QObject>
#include <QJSValue>
#include <QVariantList>

class QDBusPendingCallWatcher;
class QDBusServiceWatcher;

// Thin bridge to sailvlessd (org.sailvless.Daemon on the session bus).
// All structured data travels as JSON strings and is parsed in QML.
class Daemon : public QObject
{
    Q_OBJECT
    Q_PROPERTY(bool available READ available NOTIFY availableChanged)

public:
    explicit Daemon(QObject *parent = nullptr);

    bool available() const { return m_available; }

    // Calls a daemon method asynchronously. callback(error, result):
    // error is "" on success; result is the single string return value,
    // a JSON array for several values, or "" for none.
    Q_INVOKABLE void call(const QString &method, const QVariantList &args = QVariantList(),
                          const QJSValue &callback = QJSValue(), int timeoutMs = 30000);

signals:
    void stateChanged(const QString &statusJson);
    void availableChanged();

private slots:
    void onStateChanged(const QString &statusJson);
    void setAvailable(bool available);

private:
    bool m_available;
    QDBusServiceWatcher *m_watcher;
};

#endif
