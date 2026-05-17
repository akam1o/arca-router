#include <nc_client.h>
#include <libyang/context.h>
#include <libyang/printer_data.h>

#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static const char *cap_xpath = "urn:ietf:params:netconf:capability:xpath:1.0";
static const char *cap_arca_xpath_subset = "urn:arca:router:netconf:capability:xpath-filter-subset:1.0";

static int
accept_hostkey(const char *hostname, ssh_session session, void *priv)
{
    (void)hostname;
    (void)session;
    (void)priv;
    return 0;
}

static void
fail(const char *message)
{
    fprintf(stderr, "libnetconf2 interop failed: %s\n", message);
    exit(1);
}

static char *
evidence_path(const char *relative_path)
{
    const char *dir = getenv("NETCONF_INTEROP_EVIDENCE_DIR");
    size_t dir_len = 0;
    size_t relative_len = 0;
    char *path = NULL;

    if (!dir || !dir[0]) {
        return NULL;
    }

    dir_len = strlen(dir);
    relative_len = strlen(relative_path);
    path = malloc(dir_len + relative_len + 2);
    if (!path) {
        fail("failed to allocate evidence path");
    }
    memcpy(path, dir, dir_len);
    path[dir_len] = '/';
    memcpy(path + dir_len + 1, relative_path, relative_len);
    path[dir_len + relative_len + 1] = '\0';
    return path;
}

static void
write_evidence_file(const char *relative_path, const char *content)
{
    char *path = evidence_path(relative_path);
    FILE *file = NULL;

    if (!path) {
        return;
    }

    file = fopen(path, "w");
    if (!file) {
        free(path);
        fail("failed to open evidence file");
    }
    if (content && fputs(content, file) == EOF) {
        fclose(file);
        free(path);
        fail("failed to write evidence file");
    }
    if (!content || !content[0] || content[strlen(content) - 1] != '\n') {
        if (fputc('\n', file) == EOF) {
            fclose(file);
            free(path);
            fail("failed to finish evidence file");
        }
    }
    if (fclose(file)) {
        free(path);
        fail("failed to close evidence file");
    }
    free(path);
}

static void
write_rpc_evidence(const char *kind, const char *name, const char *content)
{
    char relative_path[256];
    int written = snprintf(relative_path, sizeof(relative_path), "%s/%s.xml", kind, name);

    if (written < 0 || (size_t)written >= sizeof(relative_path)) {
        fail("evidence path is too long");
    }
    write_evidence_file(relative_path, content);
}

static void
print_capabilities(const struct nc_session *session)
{
    const char * const *capabilities = nc_session_get_cpblts(session);
    char *content = NULL;
    size_t content_len = 0;

    puts("SERVER_CAPABILITIES");
    if (!capabilities) {
        return;
    }
    for (size_t i = 0; capabilities[i]; i++) {
        puts(capabilities[i]);
        content_len += strlen(capabilities[i]) + 1;
    }

    content = calloc(1, content_len + 1);
    if (!content) {
        fail("failed to allocate capabilities evidence");
    }
    for (size_t i = 0; capabilities[i]; i++) {
        strcat(content, capabilities[i]);
        strcat(content, "\n");
    }
    write_evidence_file("server_capabilities.txt", content);
    free(content);
}

static char *
print_reply_tree(const struct lyd_node *envp, const struct lyd_node *op)
{
    char *env_xml = NULL;
    char *op_xml = NULL;
    size_t env_len = 0;
    size_t op_len = 0;
    char *combined = NULL;

    if (envp && lyd_print_mem(&env_xml, envp, LYD_XML, 0)) {
        fail("failed to serialize rpc-reply envelope");
    }
    if (op && lyd_print_mem(&op_xml, op, LYD_XML, 0)) {
        free(env_xml);
        fail("failed to serialize rpc-reply operation data");
    }

    if (env_xml) {
        env_len = strlen(env_xml);
    }
    if (op_xml) {
        op_len = strlen(op_xml);
    }

    combined = calloc(1, env_len + op_len + 2);
    if (!combined) {
        free(env_xml);
        free(op_xml);
        fail("failed to allocate reply buffer");
    }
    if (env_xml) {
        memcpy(combined, env_xml, env_len);
        puts(env_xml);
    }
    if (op_xml) {
        combined[env_len] = '\n';
        memcpy(combined + env_len + 1, op_xml, op_len);
        puts(op_xml);
    }

    free(env_xml);
    free(op_xml);
    return combined;
}

static char *
send_rpc(struct nc_session *session, const char *name, const char *xml)
{
    struct nc_rpc *rpc = NULL;
    struct lyd_node *envp = NULL;
    struct lyd_node *op = NULL;
    uint64_t msgid = 0;
    NC_MSG_TYPE msg_type;
    char *reply = NULL;

    printf("\nRPC %s\n", name);
    write_rpc_evidence("rpc", name, xml);
    rpc = nc_rpc_act_generic_xml(xml, NC_PARAMTYPE_CONST);
    if (!rpc) {
        fail("failed to build generic RPC");
    }

    msg_type = nc_send_rpc(session, rpc, 10000, &msgid);
    if (msg_type != NC_MSG_RPC) {
        nc_rpc_free(rpc);
        fail("failed to send RPC");
    }

    msg_type = nc_recv_reply(session, rpc, msgid, 10000, &envp, &op);
    if (msg_type != NC_MSG_REPLY && msg_type != NC_MSG_REPLY_ERR_MSGID) {
        nc_rpc_free(rpc);
        fail("failed to receive RPC reply");
    }

    reply = print_reply_tree(envp, op);
    write_rpc_evidence("reply", name, reply);
    lyd_free_all(envp);
    lyd_free_all(op);
    nc_rpc_free(rpc);
    return reply;
}

static void
assert_contains(const char *haystack, const char *needle, const char *message)
{
    if (!haystack || !strstr(haystack, needle)) {
        fail(message);
    }
}

static void
assert_not_contains(const char *haystack, const char *needle, const char *message)
{
    if (haystack && strstr(haystack, needle)) {
        fail(message);
    }
}

int
main(int argc, char **argv)
{
    const char *host = NULL;
    const char *port_text = NULL;
    const char *username = NULL;
    const char *public_key = NULL;
    const char *private_key = NULL;
    const char *expect_standard_xpath = NULL;
    uint16_t port = 0;
    struct nc_session *session = NULL;
    struct ly_ctx *ctx = NULL;
    char *node_set_reply = NULL;
    char *scalar_reply = NULL;
    char *attribute_reply = NULL;

    if (argc != 6) {
        fprintf(stderr, "usage: %s <host> <port> <username> <public-key> <private-key>\n", argv[0]);
        return 2;
    }
    host = argv[1];
    port_text = argv[2];
    username = argv[3];
    public_key = argv[4];
    private_key = argv[5];
    expect_standard_xpath = getenv("NETCONF_STANDARD_XPATH");
    port = (uint16_t)strtoul(port_text, NULL, 10);
    if (!port) {
        fail("invalid port");
    }

    nc_client_init();
    if (getenv("LIBNETCONF2_SCHEMA_SEARCHPATH") &&
            nc_client_set_schema_searchpath(getenv("LIBNETCONF2_SCHEMA_SEARCHPATH"))) {
        fail("failed to set schema search path");
    }
    nc_client_ssh_set_auth_hostkey_check_clb(accept_hostkey, NULL);
    nc_client_ssh_set_username(username);
    nc_client_ssh_set_auth_pref(NC_SSH_AUTH_PUBLICKEY, 3);
    nc_client_ssh_set_auth_pref(NC_SSH_AUTH_PASSWORD, -1);
    nc_client_ssh_set_auth_pref(NC_SSH_AUTH_INTERACTIVE, -1);
    if (nc_client_ssh_add_keypair(public_key, private_key)) {
        fail("failed to add SSH keypair");
    }

    if (ly_ctx_new(getenv("LIBNETCONF2_SCHEMA_SEARCHPATH"), 0, &ctx)) {
        fail("failed to create libyang context");
    }
    if (!ly_ctx_load_module(ctx, "ietf-netconf-acm", NULL, NULL)) {
        fail("failed to load ietf-netconf-acm schema");
    }
    if (!ly_ctx_load_module(ctx, "ietf-interfaces", NULL, NULL)) {
        fail("failed to load ietf-interfaces schema");
    }

    session = nc_connect_ssh(host, port, ctx);
    if (!session) {
        fail("failed to connect");
    }

    print_capabilities(session);
    if (!nc_session_cpblt(session, cap_arca_xpath_subset)) {
        fail("missing Arca XPath filter subset capability");
    }
    if (expect_standard_xpath && !strcmp(expect_standard_xpath, "1")) {
        if (!nc_session_cpblt(session, cap_xpath)) {
            fail("missing standard XPath capability");
        }
    } else if (nc_session_cpblt(session, cap_xpath)) {
        fail("standard XPath capability was advertised");
    }

    node_set_reply = send_rpc(session, "xpath-node-set",
            "<get-config xmlns=\"urn:ietf:params:xml:ns:netconf:base:1.0\">"
            "<source><running/></source>"
            "<filter type=\"xpath\" xmlns:if=\"urn:ietf:params:xml:ns:yang:ietf-interfaces\" "
            "select=\"/if:interfaces/if:interface[contains(if:name, 'ge-0/0/0')]\"/>"
            "</get-config>");
    assert_contains(node_set_reply, "ge-0/0/0", "XPath node-set reply missing ge-0/0/0");
    assert_contains(node_set_reply, "interop-uplink", "XPath node-set reply missing interop-uplink");
    assert_not_contains(node_set_reply, "xe-0/0/0", "XPath node-set reply included xe-0/0/0");
    assert_not_contains(node_set_reply, "interop-peer", "XPath node-set reply included interop-peer");

    scalar_reply = send_rpc(session, "xpath-scalar-rejected",
            "<get-config xmlns=\"urn:ietf:params:xml:ns:netconf:base:1.0\">"
            "<source><running/></source>"
            "<filter type=\"xpath\" xmlns:if=\"urn:ietf:params:xml:ns:yang:ietf-interfaces\" "
            "select=\"/if:interfaces/if:interface = 'ge-0/0/0'\"/>"
            "</get-config>");
    assert_contains(scalar_reply, "invalid-value", "scalar XPath did not return invalid-value");

    attribute_reply = send_rpc(session, "xpath-attribute-rejected",
            "<get-config xmlns=\"urn:ietf:params:xml:ns:netconf:base:1.0\">"
            "<source><running/></source>"
            "<filter type=\"xpath\" xmlns:if=\"urn:ietf:params:xml:ns:yang:ietf-interfaces\" "
            "select=\"/if:interfaces/if:interface/@name\"/>"
            "</get-config>");
    assert_contains(attribute_reply, "invalid-value", "attribute XPath did not return invalid-value");

    free(node_set_reply);
    free(scalar_reply);
    free(attribute_reply);
    nc_session_free(session, NULL);
    ly_ctx_destroy(ctx);
    nc_client_destroy();
    return 0;
}
