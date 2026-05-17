# NETCONF XPath Interoperability Runbook

Use this runbook before promoting Arca's implementation-specific XPath support
to the standard NETCONF `:xpath` capability. The goal is to prove that clients
which do not share Arca test helpers can consume the server behavior safely.

Do not enable or advertise
`urn:ietf:params:netconf:capability:xpath:1.0` until this runbook passes and
the results are attached to the release sign-off or v0.11 tracking issue.

## Scope

Validate these outcomes with at least two independent client families:

- One `ncclient-family` client. Use `ncclient` as the baseline. PyEZ may be
  recorded as supplementary evidence because it exercises the ncclient stack.
- One `libnetconf2-family` client. Use Netopeer2 `netopeer2-cli` or another
  libnetconf2-based client as the required second family.

- The server `<hello>` advertises `urn:arca:router:netconf:capability:xpath-filter-subset:1.0`.
- The server `<hello>` does not advertise standard `:xpath` until the v0.11 gate
  is explicitly closed.
- XPath filters for `get-config` and `get` return node-set results.
- Scalar expressions, attribute selection, invalid XPath, unsupported paths,
  undeclared prefixes, and namespace mismatches return deterministic
  `rpc-error` responses.
- Expression size, input XML size, selected element count, output size, depth,
  attribute count, and evaluation timeout guardrails are exercised.

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

## Optional PyEZ Smoke

PyEZ is useful when Junos-style automation is expected to reach Arca, but it
belongs to the `ncclient-family`. Run it after the baseline ncclient check only
as supplementary evidence. It does not replace the required libnetconf2-family
check.

If PyEZ is used:

- Record the PyEZ and ncclient package versions.
- Send equivalent node-set and rejected RPCs through the raw RPC path supported
  by that PyEZ release.
- Attach the exact script, server capabilities, RPC payloads, replies, and
  exceptions.
- Label the evidence as `supplementary ncclient-family / PyEZ`.

## Required libnetconf2-family Check

Repeat equivalent RPCs with one of:

- Netopeer2 `netopeer2-cli`
- another libnetconf2-based client

The GitHub Actions `NETCONF Client Interoperability` workflow includes a
Ubuntu 24.04 libnetconf2 job that installs `libnetconf2-dev` with apt, builds a
small external client, and runs raw RPC checks against
`tools/netconf-interop-server`. Ubuntu 24.04 does not currently provide a
`netopeer2` apt package, so this CI path uses the packaged libnetconf2 client
library directly.

To collect the required local evidence outside GitHub Actions, install the
ncclient Python dependencies and libnetconf2 development packages, then run:

```bash
make netconf-client-evidence
make netconf-evidence-verify
```

The target writes ncclient and libnetconf2 evidence under
`artifacts/netconf-clients/`. Run `make netconf-pyez-evidence` only when
supplementary PyEZ evidence is useful.

`netconf-console` is acceptable only when the deployed tool is confirmed not to
be backed by ncclient. PyEZ is not acceptable for this required check when
ncclient has already passed, because both clients exercise the same client
family.

Save the raw RPC payloads and responses. If a client cannot send a raw
namespace-declared XPath filter, record that limitation as an interoperability
deviation instead of enabling standard `:xpath`.

## Evidence to Attach

Attach the following before closing the v0.11 standard XPath gate:

- GitHub Actions `NETCONF Client Interoperability` artifacts named
  `netconf-client-ncclient-evidence`, `netconf-client-libnetconf2-evidence`,
  and, when scheduled or manually run, `netconf-client-junos-eznc-evidence`.
- Arca commit SHA and package version.
- Client family, client names, and versions.
- Server `<hello>` output.
- RPC payloads.
- Reply XML or exception output.
- PyEZ evidence, if collected, labeled as supplementary ncclient-family
  evidence.
- Notes for every interoperability deviation.
- Confirmation that standard `:xpath` remains unadvertised until all deviations
  are accepted or fixed.
