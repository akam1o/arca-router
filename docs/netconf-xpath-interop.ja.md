# NETCONF XPath Interoperability Runbook

Arca の implementation-specific XPath support を standard NETCONF `:xpath`
capability に昇格する前に、この runbook を実行する。目的は、Arca の test helper を共有しない client が server behavior を安全に扱えることを確認すること。

この runbook が成功し、結果が release sign-off または v0.11 tracking issue に添付されるまで、`urn:ietf:params:netconf:capability:xpath:1.0` を enable / advertise しない。

## Scope

2 種類以上の independent client で以下を確認する。

- server `<hello>` が `urn:arca:router:netconf:capability:xpath-filter-subset:1.0` を advertise する。
- v0.11 gate を明示的に close するまで、server `<hello>` は standard `:xpath` を advertise しない。
- `get-config` と `get` の XPath filter が node-set result を返す。
- scalar expression、attribute selection、invalid XPath、unsupported path、undeclared prefix、namespace mismatch が deterministic な `rpc-error` を返す。
- expression size、input XML size、selected element count、output size、depth、attribute count、evaluation guardrail を確認する。

## Test Server Setup

temporary datastore、host key、NETCONF user database を使う。

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

client check の間、server は起動したままにする。

## ncclient Checks

別 shell で install / 実行する。

```bash
python3 -m venv "$tmpdir/ncclient-venv"
"$tmpdir/ncclient-venv/bin/pip" install ncclient
```

`"$tmpdir/ncclient-xpath.py"` を作成する。

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

実行する。

```bash
"$tmpdir/ncclient-venv/bin/python" "$tmpdir/ncclient-xpath.py"
```

期待結果:

- `SERVER_CAPABILITIES` に Arca XPath filter subset capability が含まれる。
- `SERVER_CAPABILITIES` に `urn:ietf:params:netconf:capability:xpath:1.0` が含まれない。
- `node-set` は `ge-0/0/0` を含み、`xe-0/0/0` を含まない。
- `scalar-rejected` と `attribute-rejected` は `invalid-value` の `rpc-error` を返す。

## Second Client Check

同等の RPC を以下のいずれかでも実行する。

- `netconf-console`
- Netopeer2 `netopeer2-cli`
- その他の libnetconf2-based client

raw RPC payload と response を保存する。raw namespace-declared XPath filter を送れない client の場合は、standard `:xpath` を enable せず、interoperability deviation として記録する。

## Evidence to Attach

v0.11 standard XPath gate を close する前に、以下を添付する。

- Arca commit SHA と package version。
- client name と version。
- server `<hello>` output。
- RPC payload。
- reply XML または exception output。
- interoperability deviation の note。
- standard `:xpath` は、すべての deviation が accept または fix されるまで advertise されていないことの確認。
