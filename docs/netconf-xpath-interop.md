# NETCONF XPath Interoperability Runbook

Use this runbook before promoting Arca's implementation-specific XPath support
to the standard NETCONF `:xpath` capability. The goal is to prove that clients
which do not share Arca test helpers can consume the server behavior safely.

Do not enable or advertise
`urn:ietf:params:netconf:capability:xpath:1.0` until this runbook passes and
the results are attached to the release sign-off or v0.11 tracking issue.

## Scope

Validate these outcomes with at least two independent clients:

- The server `<hello>` advertises `urn:arca:router:netconf:capability:xpath-filter-subset:1.0`.
- The server `<hello>` does not advertise standard `:xpath` until the v0.11 gate
  is explicitly closed.
- XPath filters for `get-config` and `get` return node-set results.
- Scalar expressions, attribute selection, invalid XPath, unsupported paths,
  undeclared prefixes, and namespace mismatches return deterministic
  `rpc-error` responses.
- Expression size, input XML size, selected element count, output size, depth,
  attribute count, and evaluation guardrails are exercised.

## Test Server Setup

Use a temporary datastore, host key, and NETCONF user database.

```bash
tmpdir="$(mktemp -d)"

ssh-keygen -t ed25519 -N '' -f "$tmpdir/ssh_host_ed25519_key"

go run ./tools/netconf-userdb \
  -path "$tmpdir/users.db" \
  -username xpath-admin \
  -password xpath-admin-pass \
  -role admin

cat > "$tmpdir/running.conf" <<'EOF'
set system host-name xpath-router
set interfaces ge-0/0/0 description "uplink"
set interfaces ge-0/0/0 unit 0 family inet address 192.0.2.1/24
set interfaces xe-0/0/0 description "peer"
set interfaces xe-0/0/0 unit 0 family inet address 198.51.100.1/24
set routing-options autonomous-system 65000
set routing-options static route 203.0.113.0/24 next-hop 192.0.2.254
EOF

go run ./tools/netconf-interop-server \
  -listen 127.0.0.1:1830 \
  -host-key "$tmpdir/ssh_host_ed25519_key" \
  -user-db "$tmpdir/users.db" \
  -datastore "$tmpdir/config.db" \
  -running-config "$tmpdir/running.conf"
```

Keep the server running while executing the client checks.

## ncclient Checks

Install and run from a separate shell:

```bash
python3 -m venv "$tmpdir/ncclient-venv"
"$tmpdir/ncclient-venv/bin/pip" install ncclient
```

Create `"$tmpdir/ncclient-xpath.py"`:

```python
from ncclient import manager
from ncclient.xml_ import to_ele

HOST = "127.0.0.1"
PORT = 1830
USER = "xpath-admin"
PASSWORD = "xpath-admin-pass"

RPCS = {
    "node-set": """
      <get-config xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
        <source><running/></source>
        <filter type="xpath"
          xmlns:if="urn:ietf:params:xml:ns:yang:ietf-interfaces"
          select="/if:interfaces/if:interface[contains(if:name, 'ge-0/0/0')]"/>
      </get-config>
    """,
    "scalar-rejected": """
      <get-config xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
        <source><running/></source>
        <filter type="xpath"
          xmlns:if="urn:ietf:params:xml:ns:yang:ietf-interfaces"
          select="/if:interfaces/if:interface = 'ge-0/0/0'"/>
      </get-config>
    """,
    "attribute-rejected": """
      <get-config xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
        <source><running/></source>
        <filter type="xpath"
          xmlns:if="urn:ietf:params:xml:ns:yang:ietf-interfaces"
          select="/if:interfaces/if:interface/@name"/>
      </get-config>
    """,
}

with manager.connect(
    host=HOST,
    port=PORT,
    username=USER,
    password=PASSWORD,
    hostkey_verify=False,
    look_for_keys=False,
    allow_agent=False,
    timeout=10,
) as session:
    print("SERVER_CAPABILITIES")
    for capability in session.server_capabilities:
        print(capability)

    for name, rpc in RPCS.items():
        print(f"\nRPC {name}")
        try:
            reply = session.dispatch(to_ele(rpc))
            print(reply.xml)
        except Exception as exc:
            print(type(exc).__name__)
            print(exc)
```

Run it:

```bash
"$tmpdir/ncclient-venv/bin/python" "$tmpdir/ncclient-xpath.py"
```

Expected result:

- `SERVER_CAPABILITIES` includes the Arca XPath filter subset capability.
- `SERVER_CAPABILITIES` does not include
  `urn:ietf:params:netconf:capability:xpath:1.0`.
- `node-set` includes `ge-0/0/0` and does not include `xe-0/0/0`.
- `scalar-rejected` and `attribute-rejected` return `rpc-error` with
  `invalid-value`.

## Second Client Check

Repeat equivalent RPCs with one of:

- `netconf-console`
- Netopeer2 `netopeer2-cli`
- another libnetconf2-based client

Save the raw RPC payloads and responses. If a client cannot send a raw
namespace-declared XPath filter, record that limitation as an interoperability
deviation instead of enabling standard `:xpath`.

## Evidence to Attach

Attach the following before closing the v0.11 standard XPath gate:

- Arca commit SHA and package version.
- Client names and versions.
- Server `<hello>` output.
- RPC payloads.
- Reply XML or exception output.
- Notes for every interoperability deviation.
- Confirmation that standard `:xpath` remains unadvertised until all deviations
  are accepted or fixed.
