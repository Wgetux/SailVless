/*
 * connman-vpnd plugin "sailvless": makes the SailVless tunnel a regular
 * Sailfish VPN connection (status bar icon, Settings -> VPN, top menu toggle).
 *
 * The plugin does not carry or route traffic. The tunnel (interface svtun0)
 * is set up by sailvless-tun-helper; this plugin only mirrors its state:
 *  - connect from the system UI   -> ask the helper (D-Bus RequestConnect),
 *    report READY once svtun0 is up;
 *  - tunnel started from the app  -> the helper calls Connect on our
 *    provider, svtun0 already exists, READY at once;
 *  - disconnect from the system UI -> ask the helper (RequestDisconnect);
 *  - tunnel stopped by the app/helper -> provider goes IDLE.
 * SplitRouting is set so connman never touches the default route or DNS
 * (the tunnel's own policy routing and DNS handling stay in charge).
 */
#define CONNMAN_API_SUBJECT_TO_CHANGE
#include <errno.h>
#include <string.h>
#include <glib.h>
#include <dbus/dbus.h>
#include <connman/plugin.h>
#include <connman/log.h>
#include <connman/inet.h>
#include <connman/dbus.h>
#include <connman/ipaddress.h>
#include <connman/version.h>
#include <connman/vpn/vpn-provider.h>
#include <connman/vpn/plugins/vpn.h>

#define IFNAME      "svtun0"
#define TUN_BUS     "org.sailvless.Tun"
#define TUN_PATH    "/org/sailvless/Tun"
#define TUN_IFACE   "org.sailvless.Tun1"
#define CONNECT_TIMEOUT_S 90

static struct vpn_provider *active;   /* provider we reported or are connecting */
static vpn_provider_connect_cb_t pending_cb;
static void *pending_data;
static guint timeout_id;
static gboolean ready;
static gint64 last_request;            /* monotonic time of the last RequestConnect */

static void call_helper(const char *method)
{
	DBusConnection *conn = connman_dbus_get_connection();
	DBusMessage *msg;

	if (!conn)
		return;
	msg = dbus_message_new_method_call(TUN_BUS, TUN_PATH, TUN_IFACE, method);
	if (!msg)
		goto out;
	dbus_message_set_no_reply(msg, TRUE);
	dbus_message_set_auto_start(msg, TRUE); /* D-Bus activation starts the helper */
	dbus_connection_send(conn, msg, NULL);
	dbus_message_unref(msg);
out:
	dbus_connection_unref(conn);
}

/* Ask connmand to disconnect our VPN service (tunnel stopped from the app):
 * it calls back into vpnd, which runs the regular disconnect path. Async. */
static void disconnect_service(struct vpn_provider *provider)
{
	DBusConnection *conn = connman_dbus_get_connection();
	DBusMessage *msg;
	char *path;

	if (!conn)
		return;
	path = g_strdup_printf("/net/connman/service/vpn_%s", vpn_provider_get_ident(provider));
	msg = dbus_message_new_method_call("net.connman", path, "net.connman.Service", "Disconnect");
	if (msg) {
		dbus_message_set_no_reply(msg, TRUE);
		dbus_connection_send(conn, msg, NULL);
		dbus_message_unref(msg);
	}
	g_free(path);
	dbus_connection_unref(conn);
}

/* A daemon-based VPN goes DISCONNECT -> IDLE when its process exits; we have
 * no process, so finish the transition once vpnd's disconnect has returned. */
static gboolean go_idle(gpointer data)
{
	struct vpn_provider *provider = data;

	vpn_provider_set_state(provider, VPN_PROVIDER_STATE_IDLE);
	vpn_provider_unref(provider);
	return G_SOURCE_REMOVE;
}

static void finish_pending(int err)
{
	if (timeout_id) {
		g_source_remove(timeout_id);
		timeout_id = 0;
	}
	if (pending_cb) {
		vpn_provider_connect_cb_t cb = pending_cb;
		pending_cb = NULL;
		cb(active, pending_data, err);
	}
}

/* Tell connman the tunnel is up on svtun0 (interface must exist). */
static int report_up(struct vpn_provider *provider)
{
	struct connman_ipaddress *ip;
	int err;

	err = vpn_set_ifname(provider, IFNAME);
	if (err < 0)
		return err;

	ip = connman_ipaddress_alloc(AF_INET);
	if (ip) {
		connman_ipaddress_set_ipv4(ip, "172.19.0.1", "255.255.255.252", NULL);
		connman_ipaddress_set_p2p(ip, true);
		vpn_provider_set_ipaddress(provider, ip);
		connman_ipaddress_free(ip);
	}
	vpn_provider_set_boolean(provider, "SplitRouting", true, false);
	vpn_provider_set_supported_ip_networks(provider, true, false);
	vpn_provider_set_state(provider, VPN_PROVIDER_STATE_READY);
	ready = TRUE;
	return 0;
}

static gboolean connect_timeout(gpointer data)
{
	timeout_id = 0;
	connman_warn("sailvless: tunnel did not come up in %d s", CONNECT_TIMEOUT_S);
	if (active) {
		finish_pending(-ETIMEDOUT);
		vpn_provider_indicate_error(active, VPN_PROVIDER_ERROR_CONNECT_FAILED);
	}
	return G_SOURCE_REMOVE;
}

static int sv_connect(struct vpn_provider *provider, struct connman_task *task,
			const char *if_name, vpn_provider_connect_cb_t cb,
			const char *dbus_sender, void *user_data)
{
	if (active && active != provider)
		vpn_provider_unref(active);
	if (active != provider)
		active = vpn_provider_ref(provider);
	ready = FALSE;

	if (connman_inet_ifindex(IFNAME) >= 0) {
		/* the tunnel is already running (started from the app) */
		int err = report_up(provider);
		if (cb)
			cb(provider, user_data, err);
		return err;
	}

	/* started from the system UI: the helper asks the SailVless daemon */
	finish_pending(-ECANCELED); /* a stale attempt, if any */
	pending_cb = cb;
	pending_data = user_data;
	timeout_id = g_timeout_add_seconds(CONNECT_TIMEOUT_S, connect_timeout, NULL);
	/* connmand may retry quickly; one request is enough while it is in flight */
	if (g_get_monotonic_time() - last_request > 3 * G_USEC_PER_SEC) {
		last_request = g_get_monotonic_time();
		call_helper("RequestConnect");
	}
	return -EINPROGRESS;
}

static void sv_disconnect(struct vpn_provider *provider)
{
	finish_pending(-ECANCELED);
	/* if the tunnel is still up the user switched the VPN off in the system UI */
	if (connman_inet_ifindex(IFNAME) >= 0)
		call_helper("RequestDisconnect");
	ready = FALSE;
	vpn_provider_set_state(provider, VPN_PROVIDER_STATE_DISCONNECT);
	g_idle_add(go_idle, vpn_provider_ref(provider));
	if (active == provider) {
		vpn_provider_unref(active);
		active = NULL;
	}
}

static int sv_error_code(struct vpn_provider *provider, int exit_code)
{
	return exit_code ? VPN_PROVIDER_ERROR_CONNECT_FAILED : 0;
}

static bool sv_uses_vpn_agent(struct vpn_provider *provider)
{
	return false; /* nothing to ask the user */
}

/* Follow the helper's StateChanged signal: {"state":"connected"|"stopped"|...} */
static DBusHandlerResult helper_signal(DBusConnection *conn, DBusMessage *msg, void *data)
{
	const char *json = NULL;

	if (!dbus_message_is_signal(msg, TUN_IFACE, "StateChanged"))
		return DBUS_HANDLER_RESULT_NOT_YET_HANDLED;
	if (!dbus_message_get_args(msg, NULL, DBUS_TYPE_STRING, &json, DBUS_TYPE_INVALID) || !json)
		return DBUS_HANDLER_RESULT_NOT_YET_HANDLED;
	if (!active)
		return DBUS_HANDLER_RESULT_NOT_YET_HANDLED;

	if (strstr(json, "\"state\":\"connected\"") && !ready) {
		int err = report_up(active);
		finish_pending(err);
	} else if (strstr(json, "\"state\":\"stopped\"") && pending_cb) {
		/* the start requested from the system UI failed */
		finish_pending(-ECONNABORTED);
		vpn_provider_indicate_error(active, VPN_PROVIDER_ERROR_CONNECT_FAILED);
	} else if (strstr(json, "\"state\":\"stopped\"") && ready) {
		/* stopped from the app: the VPN connection goes down too */
		ready = FALSE;
		disconnect_service(active);
	} else if (strstr(json, "\"state\":\"error\"") && pending_cb) {
		finish_pending(-EIO);
		vpn_provider_indicate_error(active, VPN_PROVIDER_ERROR_CONNECT_FAILED);
	}
	return DBUS_HANDLER_RESULT_NOT_YET_HANDLED;
}

static struct vpn_driver vpn_driver = {
	.flags          = VPN_FLAG_NO_TUN | VPN_FLAG_NO_DAEMON,
	.connect        = sv_connect,
	.disconnect     = sv_disconnect,
	.error_code     = sv_error_code,
	.uses_vpn_agent = sv_uses_vpn_agent,
};

#define MATCH "type='signal',interface='" TUN_IFACE "',member='StateChanged'"

static int sv_init(void)
{
	DBusConnection *conn = connman_dbus_get_connection();

	if (conn) {
		dbus_bus_add_match(conn, MATCH, NULL);
		dbus_connection_add_filter(conn, helper_signal, NULL, NULL);
		dbus_connection_unref(conn);
	}
	return vpn_register("sailvless", &vpn_driver, NULL);
}

static void sv_exit(void)
{
	DBusConnection *conn = connman_dbus_get_connection();

	if (conn) {
		dbus_connection_remove_filter(conn, helper_signal, NULL);
		dbus_bus_remove_match(conn, MATCH, NULL);
		dbus_connection_unref(conn);
	}
	if (timeout_id)
		g_source_remove(timeout_id);
	if (active)
		vpn_provider_unref(active);
	vpn_unregister("sailvless");
}

CONNMAN_PLUGIN_DEFINE(sailvless, "SailVless VPN plugin", CONNMAN_VERSION,
			CONNMAN_PLUGIN_PRIORITY_DEFAULT, sv_init, sv_exit)
