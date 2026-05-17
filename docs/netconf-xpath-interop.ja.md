# NETCONF XPath Interoperability Runbook

Arca の standard NETCONF `:xpath` capability の release evidence を収集するために、この runbook を実行する。目的は、Arca の test helper を共有しない client が default server behavior を安全に扱えることを確認すること。

この runbook が成功し、結果が release sign-off に添付されるまでは、release の standard `:xpath` behavior を維持または変更しない。

## Scope

2 種類以上の independent client family で以下を確認する。

- `ncclient-family` を 1 つ使う。baseline は `ncclient` とする。PyEZ は ncclient stack を使うため、supplementary evidence として記録できる。
- `libnetconf2-family` を 1 つ使う。必須の 2 種類目として Netopeer2 `netopeer2-cli` またはその他の libnetconf2-based client を使う。

- default server `<hello>` が `urn:arca:router:netconf:capability:xpath-filter-subset:1.0` と standard `urn:ietf:params:netconf:capability:xpath:1.0` を advertise する。
- `--netconf-standard-xpath=false` または `-standard-xpath=false` で起動する compatibility-suppressed mode は、Arca-specific subset capability を維持しつつ standard `:xpath` を omit する。
- `get-config` と `get` の XPath filter が node-set result を返す。
- scalar expression、attribute selection、invalid XPath、unsupported path、undeclared prefix、namespace mismatch が deterministic な `rpc-error` を返す。
- expression size、input XML size、selected element count、output size、depth、attribute count、evaluation timeout guardrail を確認する。

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
- default mode では `SERVER_CAPABILITIES` に `urn:ietf:params:netconf:capability:xpath:1.0` が含まれる。
- `node-set` は `ge-0/0/0` を含み、`xe-0/0/0` を含まない。
- `scalar-rejected` と `attribute-rejected` は `invalid-value` の `rpc-error` を返す。

## Optional PyEZ Smoke

PyEZ は Junos-style automation が Arca に接続する想定の smoke test として有用だが、`ncclient-family` に属する。baseline ncclient check の後に supplementary evidence としてのみ実行する。必須の libnetconf2-family check の代替にはならない。

PyEZ を使う場合:

- PyEZ と ncclient package version を記録する。
- その PyEZ release が support する raw RPC path で、同等の node-set RPC と rejected RPC を送る。
- exact script、server capability、RPC payload、reply、exception を添付する。
- evidence は `supplementary ncclient-family / PyEZ` と label する。

## Required libnetconf2-family Check

同等の RPC を以下のいずれかでも実行する。

- Netopeer2 `netopeer2-cli`
- その他の libnetconf2-based client

GitHub Actions の `NETCONF Client Interoperability` workflow には、Ubuntu 24.04 で apt install した `libnetconf2-dev` を使って小さな外部 client を build し、`tools/netconf-interop-server` に raw RPC check を実行する job が含まれる。Ubuntu 24.04 は現時点で `netopeer2` apt package を提供していないため、この CI path は packaged libnetconf2 client library を直接使う。

GitHub Actions 外で必須 evidence を収集する場合は、ncclient の Python dependency と libnetconf2 development package を install してから以下を実行する。

```bash
make netconf-client-evidence
make netconf-evidence-verify
make netconf-standard-xpath-evidence
make netconf-standard-xpath-evidence-verify
```

default target は ncclient と libnetconf2 の evidence を `artifacts/netconf-clients/` に出力し、standard `:xpath` advertisement を必須にする。dedicated standard XPath target は同じ required capability evidence を `artifacts/netconf-clients/standard-xpath/` に出力する。Supplementary PyEZ evidence が必要な場合のみ `make netconf-pyez-evidence` を実行する。

`netconf-console` は、導入されている tool が ncclient-backed ではないことを確認できる場合のみ acceptable とする。ncclient がすでに pass している場合、PyEZ は同じ client family を確認しているため、この必須 check には使えない。

raw RPC payload と response を保存する。raw namespace-declared XPath filter を送れない client の場合は、standard `:xpath` support を sign-off する前に interoperability deviation として記録する。

## Evidence to Attach

standard XPath support を sign-off する前に、以下を添付する。

- GitHub Actions `NETCONF Client Interoperability` の artifact
  `netconf-client-ncclient-evidence`、`netconf-client-libnetconf2-evidence`、
  `netconf-client-ncclient-standard-xpath-evidence`、
  `netconf-client-libnetconf2-standard-xpath-evidence`、
  および schedule / manual run 時の `netconf-client-junos-eznc-evidence`。
  必須の ncclient / libnetconf2 artifact については workflow の
  `verify interop evidence` と `verify standard xpath evidence` job が pass していること。
- Arca commit SHA と package version。
- client family、client name、version。
- server `<hello>` output。
- RPC payload。
- reply XML または exception output。
- PyEZ evidence を収集した場合は、supplementary ncclient-family evidence として label する。
- interoperability deviation の note。
- default mode で standard `:xpath` が advertise され、compatibility suppression は testing 用にのみ残り、startup datastore は advertise されないことの確認。
