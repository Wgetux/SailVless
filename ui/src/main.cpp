#include <QGuiApplication>
#include <QQuickView>
#include <QQmlContext>
#include <QScopedPointer>

#include <sailfishapp.h>

#include "daemon.h"

int main(int argc, char *argv[])
{
    QScopedPointer<QGuiApplication> app(SailfishApp::application(argc, argv));
    app->setApplicationVersion(QStringLiteral(APP_VERSION));

    QScopedPointer<QQuickView> view(SailfishApp::createView());
    Daemon daemon;
    view->rootContext()->setContextProperty(QStringLiteral("daemon"), &daemon);
    view->rootContext()->setContextProperty(QStringLiteral("appVersion"), app->applicationVersion());
    view->setSource(SailfishApp::pathToMainQml());
    view->show();
    return app->exec();
}
