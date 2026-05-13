# arca-router 設定仕様（v0.6.x）

このドキュメントは arca-router の設定構文とセマンティクスを定義します。

[English](SPEC.md)

## 概要

arca-router は Junos 風の `set` コマンド構文を採用しています。設定は以下の方法で管理します。

1. **統合デーモン (`arca-routerd`)**: VPP、FRR、NETCONF、gRPC、Prometheus、Web UI、SNMP を単一プロセスで処理
2. **対話型 CLI (`arca`)**: gRPC シンクライアントによる運用コマンドと candidate/running 設定ワークフロー
3. **NETCONF/SSH**: NETCONF（RFC 6241）によるリモート設定。デーモンに内蔵され、同じ datastore/engine を利用
4. **ファイルブートストラップ**: 設定済み datastore に running 設定がない場合のみ、起動時に `/etc/arca-router/arca-router.conf` を読み込み

### v0.6.x アーキテクチャ

v0.6.x は統合デーモン経路を advanced features 向けに拡張します：

- **構造体ファースト設定モデル**: 設定は Go 構造体（`internal/model.RouterConfig`）で表現。テキストはシリアライズの一形式。
- **SQLite または etcd candidate/running datastore**: single-node では SQLite が標準。clustered deployment では etcd を選択できます。
- **差分ベースエンジン**: 設定エンジン（`internal/engine`）が running/candidate の最小差分を計算し、変更箇所のみ適用。
- **プラグインベース サウスバウンド**: VPP / FRR は `engine.Plugin` 実装として、それぞれ関連する差分のみを受け取る。
- **Transactional FRR apply**: 標準の `--frr-apply-mode=transactional` は FRR management candidate datastore に対して `vtysh` の `mgmt commit check` / `mgmt commit apply` を実行。
- **復旧用 FRR file backend**: `--frr-apply-mode=file` は復旧・互換用途の full-file reload 経路として保持。
- **gRPC 内部 API**: `arca` は Unix ソケット gRPC（`api/v1/router.proto`、デフォルト `/run/arca-router/routerd.sock`）経由でデーモンと通信。
- **2 フェーズコミット**: 全プラグイン検証 → 全プラグイン適用 → 障害時ロールバック。
- **Advanced configuration model**: clustering、MPLS、VRRP、routing instances、class of service、Web UI service settings を構造体ファースト model と diff engine で扱います。
- **Cluster datastore selection**: `arca-routerd` と embedded NETCONF は同じ SQLite または etcd datastore backend を共有します。
- **Observability**: 任意で Prometheus `/metrics`、`/healthz`、read-only Web UI、read-only SNMPv2c、パッケージ同梱 Grafana dashboard を提供。

この仕様で扱うコマンド名は `arca-routerd` と `arca` のみです。廃止済みの command entrypoint はメンテナンス対象外です。

> **互換性メモ**: `set` コマンド構文と NETCONF 設定モデルは維持します。自動移行ツールは v0.6.x の対象外です。

---

## 目次

1. [設定構文](#configuration-syntax)
2. [システム設定](#system-configuration)
3. [インターフェース設定](#interface-configuration)
4. [ルーティングオプション](#routing-options)
5. [プロトコル](#protocols)
   - [BGP](#bgp-configuration)
   - [OSPF](#ospf-configuration)
   - [スタティックルート](#static-routes)
6. [Policy Options](#policy-options)
   - [Prefix Lists](#prefix-lists)
   - [Policy Statements](#policy-statements)
7. [Advanced v0.6 Configuration](#advanced-v06-configuration)
8. [セキュリティ](#security)
   - [NETCONF サーバ](#netconf-server)
   - [ユーザ管理](#user-management)
   - [レート制限](#rate-limiting)
9. [設定ワークフロー](#configuration-workflow)
10. [例](#examples)
11. [実行時オプションと Observability](#runtime-options-and-observability)
12. [運用コマンド](#operational-commands)
13. [設定の妥当性確認](#configuration-validation)
14. [トラブルシューティング](#troubleshooting)
15. [参照](#references)
16. [バージョン履歴](#version-history)

---

<a id="configuration-syntax"></a>
## 設定構文

### 基本形式

すべての設定コマンドは Junos 風の `set` 構文です。

```
set <hierarchy-path> <value>
```

**階層（Hierarchy）の例**:
- System-level: `set system ...`
- Interface-level: `set interfaces ...`
- Routing-level: `set routing-options ...`
- Protocol-level: `set protocols ...`
- Policy-level: `set policy-options ...`
- Security-level: `set security ...`

**コメント**:
```
# This is a comment (line starting with #)
```

**空白**: 複数のスペース/タブは 1 つのスペースとして扱います。

**大文字・小文字**: 設定キーは大文字小文字を区別します。

---

<a id="system-configuration"></a>
## システム設定

### ホスト名

**構文**:
```
set system host-name <hostname>
```

**パラメータ**:
- `<hostname>`: 文字列（英数字 + ハイフン、1-63 文字）

**例**:
```
set system host-name arca-router-01
```

**デフォルト**: `localhost`

---

<a id="interface-configuration"></a>
## インターフェース設定

### インターフェース命名規則

- `ge-X/Y/Z`: Gigabit Ethernet（1 GbE）
- `xe-X/Y/Z`: 10 Gigabit Ethernet（10 GbE）
- `et-X/Y/Z`: 100 Gigabit Ethernet（100 GbE）

各フィールドの意味:
- `X`: FPC（Flexible PIC Concentrator）スロット
- `Y`: PIC（Physical Interface Card）スロット
- `Z`: ポート番号

### 説明（Description）

**構文**:
```
set interfaces <name> description <text>
```

**パラメータ**:
- `<name>`: インターフェース名（例: `ge-0/0/0`）
- `<text>`: 説明文（任意の文字列）

**例**:
```
set interfaces ge-0/0/0 description "WAN Uplink to ISP"
set interfaces ge-0/0/1 description "LAN Interface"
```

### IP アドレス（IPv4）

**構文**:
```
set interfaces <name> unit <unit-number> family inet address <cidr>
```

**パラメータ**:
- `<name>`: インターフェース名
- `<unit-number>`: 論理ユニット番号（0-4095）
- `<cidr>`: CIDR 表記の IPv4 アドレス（例: `192.168.1.1/24`）

**例**:
```
set interfaces ge-0/0/0 unit 0 family inet address 10.0.1.1/24
set interfaces ge-0/0/0 unit 100 family inet address 172.16.1.1/28
```

### IP アドレス（IPv6）

**構文**:
```
set interfaces <name> unit <unit-number> family inet6 address <cidr>
```

**パラメータ**:
- `<name>`: インターフェース名
- `<unit-number>`: 論理ユニット番号（0-4095）
- `<cidr>`: CIDR 表記の IPv6 アドレス（例: `2001:db8::1/64`）

**例**:
```
set interfaces ge-0/0/0 unit 0 family inet6 address 2001:db8:1::1/64
set interfaces ge-0/0/1 unit 0 family inet6 address 2001:db8:2::1/64
```

### ハードウェアマッピング

インターフェースは `/etc/arca-router/hardware.yaml` により物理 NIC にマッピングされます。

```yaml
interfaces:
  - name: "ge-0/0/0"
    pci: "0000:03:00.0"
    driver: "avf"          # Intel AVF driver
    description: "WAN Uplink"
  - name: "ge-0/0/1"
    pci: "0000:03:00.1"
    driver: "avf"
    description: "LAN Interface"
```

**対応ドライバ**:
- `avf`: Intel Adaptive Virtual Function（Intel NIC で推奨）
- `rdma`: Mellanox の RDMA 対応 NIC
- `dpdk`: 汎用 DPDK ドライバ

**PCI アドレスの確認**:
```
lspci | grep Ethernet
```

---

<a id="routing-options"></a>
## ルーティングオプション

### AS 番号（Autonomous System）

**構文**:
```
set routing-options autonomous-system <asn>
```

**パラメータ**:
- `<asn>`: AS 番号（1-4294967295）

**例**:
```
set routing-options autonomous-system 65000
```

**利用**: BGP

### Router ID

**構文**:
```
set routing-options router-id <ip-address>
```

**パラメータ**:
- `<ip-address>`: IPv4 アドレス（ルータ識別子として使用）

**例**:
```
set routing-options router-id 10.0.1.1
```

**利用**: BGP, OSPF

**推奨**: ループバック、または安定したインターフェースの IP を使用してください。

<a id="static-routes"></a>
### スタティックルート

**構文**:
```
set routing-options static route <prefix> next-hop <ip-address> [distance <value>]
```

**パラメータ**:
- `<prefix>`: 宛先プレフィックス（CIDR）
- `<ip-address>`: 次ホップ IP アドレス
- `<value>`: 任意の administrative distance（1-255、デフォルト: 1）

**例**:
```
# Default route
set routing-options static route 0.0.0.0/0 next-hop 10.0.1.254

# Specific route with custom distance
set routing-options static route 192.168.100.0/24 next-hop 192.168.1.254 distance 10
```

---

<a id="protocols"></a>
## プロトコル

<a id="bgp-configuration"></a>
### BGP 設定

#### BGP Group

**構文**:
```
set protocols bgp group <group-name> type <type>
```

**パラメータ**:
- `<group-name>`: グループ識別子（英数字）
- `<type>`: `internal`（IBGP）または `external`（EBGP）

**例**:
```
set protocols bgp group IBGP type internal
set protocols bgp group EBGP type external
```

#### BGP Neighbor

**構文**:
```
set protocols bgp group <group-name> neighbor <ip-address> peer-as <asn>
set protocols bgp group <group-name> neighbor <ip-address> description <text>
set protocols bgp group <group-name> neighbor <ip-address> local-address <ip-address>
```

**パラメータ**:
- `<group-name>`: BGP グループ名
- `<ip-address>`: ネイバー IP アドレス
- `<asn>`: ネイバー AS 番号
- `<text>`: 説明文
- `<local-address>`: BGP セッションの送信元 IP

**例**:
```
set protocols bgp group IBGP neighbor 10.0.1.2 peer-as 65001
set protocols bgp group IBGP neighbor 10.0.1.2 description "Internal BGP Peer"
set protocols bgp group IBGP neighbor 10.0.1.2 local-address 10.0.1.1

set protocols bgp group EBGP neighbor 10.0.2.2 peer-as 65002
set protocols bgp group EBGP neighbor 10.0.2.2 description "External BGP Peer - ISP"
```

#### BGP へのポリシー適用

**構文**:
```
set protocols bgp group <group-name> import <policy-name>
set protocols bgp group <group-name> export <policy-name>
```

**パラメータ**:
- `<policy-name>`: 適用する policy-statement 名

**例**:
```
set protocols bgp group EBGP import DENY-PRIVATE
set protocols bgp group EBGP export ANNOUNCE-CUSTOMER
```

ポリシーの定義は [Policy Options](#policy-options) を参照してください。

<a id="ospf-configuration"></a>
### OSPF 設定

#### OSPF Router ID

**構文**:
```
set protocols ospf router-id <ip-address>
```

**パラメータ**:
- `<ip-address>`: IPv4 アドレス（OSPF の router identifier）

**例**:
```
set protocols ospf router-id 10.0.1.1
```

#### OSPF Area Interface

**構文**:
```
set protocols ospf area <area-id> interface <interface-name>
set protocols ospf area <area-id> interface <interface-name> passive
set protocols ospf area <area-id> interface <interface-name> metric <metric>
set protocols ospf area <area-id> interface <interface-name> priority <priority>
```

**パラメータ**:
- `<area-id>`: OSPF エリア ID（ドット表記: `0.0.0.0`、または整数: `0`）
- `<interface-name>`: インターフェース名（例: `ge-0/0/0`）
- `passive`: OSPF Hello を送信しない（任意）
- `<metric>`: リンクメトリック（1-65535、任意）
- `<priority>`: DR 選出優先度（0-255、任意）

**例**:
```
set protocols ospf area 0.0.0.0 interface ge-0/0/0
set protocols ospf area 0.0.0.0 interface ge-0/0/1 passive
set protocols ospf area 0.0.0.0 interface ge-0/0/1 metric 100
set protocols ospf area 0.0.0.0 interface ge-0/0/1 priority 1
```

### スタティックルート

[Routing Options - Static Routes](#static-routes) を参照してください。

---

<a id="policy-options"></a>
## Policy Options

Policy Options は、ルートフィルタ、ルートの操作、トラフィックフォワーディングを細かく制御するための仕組みです。

<a id="prefix-lists"></a>
### Prefix Lists

policy-statement のマッチ条件で利用する IP プレフィックス集合を定義します。

**構文**:
```
set policy-options prefix-list <name> <prefix>
```

**パラメータ**:
- `<name>`: prefix-list 名
- `<prefix>`: CIDR 表記の IP プレフィックス（IPv4/IPv6）

**例**:
```
# IPv4 prefix-list
set policy-options prefix-list PRIVATE 10.0.0.0/8
set policy-options prefix-list PRIVATE 172.16.0.0/12
set policy-options prefix-list PRIVATE 192.168.0.0/16

# IPv6 prefix-list
set policy-options prefix-list PUBLIC-V6 2001:db8::/32
```

**注**: prefix-list に IPv4/IPv6 が混在している場合、FRR 設定生成時に `<name>`（IPv4）と `<name>-v6`（IPv6）へ分割されます。

<a id="policy-statements"></a>
### Policy Statements

マッチ条件とアクションで構成されるルーティングポリシーを定義します。

#### マッチ条件（from）

**構文**:
```
set policy-options policy-statement <policy-name> term <term-name> from <condition> <value>
```

**対応条件**:
- `prefix-list <name>`: prefix-list に含まれるプレフィックスにマッチ
- `protocol <protocol>`: ルートの出自プロトコルにマッチ（`bgp`, `ospf`, `ospf3`, `static`, `connected`, `direct`, `kernel`, `rip`）
- `neighbor <ip>`: 特定 BGP ネイバーからのルートにマッチ
- `as-path "<regex>"`: AS-path を正規表現でマッチ

**例**:
```
set policy-options policy-statement DENY-PRIVATE term DENY from prefix-list PRIVATE
set policy-options policy-statement FILTER-BGP term MATCH from protocol bgp
set policy-options policy-statement FILTER-AS term MATCH from as-path ".*65001.*"
```

#### アクション（then）

**構文**:
```
set policy-options policy-statement <policy-name> term <term-name> then <action> [value]
```

**対応アクション**:
- `accept`: 受理（permit）
- `reject`: 拒否（deny）
- `local-preference <value>`: BGP local-preference を設定（0-4294967295）
- `community <community>`: BGP community を設定（AS:value 形式）

**例**:
```
set policy-options policy-statement DENY-PRIVATE term DENY then reject
set policy-options policy-statement DENY-PRIVATE term ALLOW then accept

set policy-options policy-statement PREFER-CUSTOMER term CUSTOMER then local-preference 200
set policy-options policy-statement PREFER-CUSTOMER term CUSTOMER then accept

set policy-options policy-statement TAG-TRANSIT term TRANSIT then community 65000:100
set policy-options policy-statement TAG-TRANSIT term TRANSIT then accept
```

#### 完全なポリシー例

```
# Define prefix-list
set policy-options prefix-list CUSTOMER 10.100.0.0/16

# Create policy with match and action
set policy-options policy-statement PREFER-CUSTOMER term CUSTOMER from prefix-list CUSTOMER
set policy-options policy-statement PREFER-CUSTOMER term CUSTOMER then local-preference 200
set policy-options policy-statement PREFER-CUSTOMER term CUSTOMER then accept

# Default term (always include)
set policy-options policy-statement PREFER-CUSTOMER term DEFAULT then accept

# Apply to BGP
set protocols bgp group external import PREFER-CUSTOMER
```

**推奨**: 常にデフォルト term を 1 つ用意し、`accept` もしくは `reject` のアクションを明示してください。

---

<a id="advanced-v06-configuration"></a>
## Advanced v0.6 Configuration

以下の hierarchy は v0.6 の management-plane model です。parser、serializer、validation、clone、conversion、diff、candidate command replacement は実装済みです。FRR VRRP 適用と VPP MPLS interface forwarding は実装済みで、L3VPN plumbing と QoS enforcement の southbound 適用は段階的に実装します。

対応する southbound apply path が実装されるまでは、未対応の routing-instance または class-of-service 設定を active に残す commit は validation で失敗します。未対応 stanza の削除は許可します。VRRP は FRR file backend と標準の transactional FRR backend の両方で適用されます。

MPLS、VRRP、OSPF、routing-instance、class-of-service の interface 参照は `interfaces` 配下に定義された interface を指す必要があります。未知の interface 参照は southbound apply 前の validation で失敗します。

### Prometheus service

```
set system services prometheus enabled true
set system services prometheus listen-address 127.0.0.1
set system services prometheus port 9090
```

`listen-address` は IP address または `localhost` を指定します。port を明示せずに有効化した場合、daemon は `9090` を使用します。

### Web UI service

```
set system services web-ui enabled true
set system services web-ui listen-address 127.0.0.1
set system services web-ui port 8080
```

`listen-address` は IP address または `localhost` を指定します。port を明示せずに有効化した場合、daemon は `8080` を使用します。

### SNMP service

```
set system services snmp enabled true
set system services snmp listen-address 127.0.0.1
set system services snmp port 1161
set system services snmp community public
```

`listen-address` は IP address または `localhost` を指定します。port を明示せずに有効化した場合、daemon は標準 UDP port `161` を使用します。community を明示しない場合は `public` を使用します。

### Multi-chassis and VRRP

```
set chassis cluster enabled true
set chassis cluster node node0 address 192.0.2.10
set chassis cluster node node0 priority 120
set chassis cluster sync etcd endpoint http://127.0.0.1:2379

set protocols vrrp group 10 interface ge-0/0/0
set protocols vrrp group 10 virtual-address 192.0.2.1
set protocols vrrp group 10 priority 110
set protocols vrrp group 10 preempt
```

`chassis cluster` を有効化し、`sync etcd endpoint` を設定する場合、daemon は `--datastore-backend=etcd` で動作している必要があります。また、設定内の sync endpoints は `--etcd-endpoints` と一致している必要があります。不一致の cluster sync 設定を active に残す commit は validation で失敗します。

VRRP group ID は数値で `1` から `255` の範囲です。VRRP priority は設定する場合 `1` から `254` の範囲です。default 動作にする場合は省略します。設定する VRRP interface は `interfaces` 配下に存在する必要があります。

FRR VRRP 設定を適用する前に、arca-routerd は FRR `vrrpd` が前提にする Linux state を準備します。LCP interface 上に arca 管理の macvlan interface（`arv4-<id>-<hash>` または `arv6-<id>-<hash>`）を作成し、RFC VRRP virtual MAC を設定し、virtual address を `/32` または `/128` として付与して up にします。準備した interface 名は `/var/lib/arca-router/vrrp-interfaces.json` に保存されるため、daemon 再起動後も stale な arca 管理 macvlan interface を削除できます。この処理には `CAP_NET_ADMIN` が必要で、packaged systemd unit には含まれています。

### MPLS and Routing Instances

```
set protocols mpls interface ge-0/0/0

set routing-instances BLUE instance-type vrf
set routing-instances BLUE route-distinguisher 65000:100
set routing-instances BLUE vrf-target target:65000:100
set routing-instances BLUE vrf-import BLUE-IN
set routing-instances BLUE vrf-export BLUE-OUT
set routing-instances BLUE interface ge-0/0/1
```

v0.6 では `instance-type vrf` のみ受け付けます。route distinguisher は `<asn>:<number>`、VRF target は `target:<asn>:<number>` 形式です。`vrf-import` と `vrf-export` は設定済みの `policy-options policy-statement` 名を参照し、複数回指定して順序付き policy chain を構成できます。

`protocols mpls interface` は対応する managed VPP interface で MPLS forwarding を有効化します。stanza を削除すると、interface を VPP から削除する前に MPLS forwarding を無効化します。MPLS と routing-instance の interface 参照は設定済み interface に解決できる必要があります。Routing-instance/L3VPN southbound plumbing は引き続き v0.6 safety gate で保護されます。

### Class of Service

```
set class-of-service forwarding-class expedited-forwarding queue 5
set class-of-service traffic-control-profile WAN shaping-rate 1000000000
set class-of-service traffic-control-profile WAN scheduler-map WAN-SCHED
set class-of-service interfaces ge-0/0/0 output-traffic-control-profile WAN
```

Forwarding class queue は `0` から `7` の範囲です。Interface binding は既存の traffic-control profile と設定済み interface を参照する必要があります。

---

<a id="security"></a>
## セキュリティ

<a id="netconf-server"></a>
### NETCONF サーバ

#### NETCONF ポート

**構文**:
```
set security netconf ssh port <port>
```

**パラメータ**:
- `<port>`: TCP ポート番号（1-65535、デフォルト: 830）

**例**:
```
set security netconf ssh port 830
```

**注**: NETCONF サーバは `arca-routerd` に統合されています。`--netconf-listen` を省略した場合、daemon は `security netconf ssh port` の設定ポートで listen します。未設定の場合は `:830` を使用します。`--netconf-listen` は明示的な runtime override として残り、listen address も含めて指定できます。

<a id="user-management"></a>
### ユーザ管理

#### ユーザ作成

**構文**:
```
set security users user <username> password <password>
set security users user <username> role <role>
```

**パラメータ**:
- `<username>`: ユーザ名（英数字、3-32 文字）
- `<password>`: 任意のパスワード（推奨: 8 文字以上）。省略した場合は SSH 公開鍵のみで認証する “鍵のみユーザ” になります。
- `<role>`: ロール（`admin`, `operator`, `read-only`）

**ロール**:
- `admin`: フルアクセス（`kill-session` を含む NETCONF 操作全般）
- `operator`: 設定管理（edit/commit/lock/unlock）
- `read-only`: 参照のみ（get-config/get）

**例**:
```
# Create admin user
set security users user alice password SuperSecret123
set security users user alice role admin

# Create operator user
set security users user bob password Operator456
set security users user bob role operator

# Create read-only user
set security users user monitor password ReadOnly789
set security users user monitor role read-only
```

**推奨**: 強固なパスワードを使用し、最小権限の原則に従ってください。

#### SSH 公開鍵認証

**構文**:
```
set security users user <username> ssh-key "<public-key>"
```

**パラメータ**:
- `<public-key>`: OpenSSH 形式の SSH 公開鍵

**例**:
```
set security users user alice ssh-key "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQ... alice@workstation"
```

**注**: 自動化用途では、パスワード認証よりも公開鍵認証を推奨します。

<a id="rate-limiting"></a>
### レート制限

**構文**:
```
set security rate-limit per-ip <limit>
set security rate-limit per-user <limit>
```

**パラメータ**:
- `<limit>`: 1 秒あたりの最大リクエスト数（1-1000）

**例**:
```
set security rate-limit per-ip 10
set security rate-limit per-user 20
```

**デフォルト**:
- Per-IP: 10 requests/second
- Per-user: 20 requests/second

---

<a id="configuration-workflow"></a>
## 設定ワークフロー

### ファイルベース設定

`/etc/arca-router/arca-router.conf` はブートストラップ用の設定ソースです。`arca-routerd` は起動時にまず設定済み datastore から current running configuration を読み込みます。running 設定が存在しない場合のみ、設定ファイルを parse して engine 経由で適用し、datastore に保存します。

1. 初回起動前、または datastore を意図的に初期化した後に `/etc/arca-router/arca-router.conf` を編集
2. デーモン起動/再起動: `sudo systemctl restart arca-routerd`
3. 確認: `sudo journalctl -u arca-routerd -n 50`

datastore 初期化後の通常の設定変更は `arca` または NETCONF を使用します。

clustered deployment では etcd datastore backend を使用します。

```bash
arca-routerd \
  --datastore-backend=etcd \
  --etcd-endpoints=https://etcd1:2379,https://etcd2:2379,https://etcd3:2379 \
  --etcd-prefix=/arca-router/
```

`chassis cluster sync etcd endpoint` を設定している場合、その endpoints は daemon の `--etcd-endpoints` と一致している必要があります。一致しない場合は、startup または commit validation で失敗し、設定は受け入れられません。

### NETCONF 設定

NETCONF の編集は `arca` と同じ candidate/running datastore と engine を使用します。

1. NETCONF クライアントで接続:
   ```bash
   netconf-console --host 192.168.1.1 --port 830 --user alice --password xxx
   ```

2. candidate 設定を編集:
   ```xml
   <rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
     <edit-config>
       <target><candidate/></target>
       <config>
         <system xmlns="urn:arca:router:config:1.0">
           <host-name>new-hostname</host-name>
         </system>
       </config>
     </edit-config>
   </rpc>
   ```

3. validate と commit:
   ```xml
   <rpc message-id="102"><validate><source><candidate/></source></validate></rpc>
   <rpc message-id="103"><commit/></rpc>
   ```

### 対話型 CLI 設定

`arca` は Unix ソケット gRPC API 経由で `arca-routerd` と通信します。デフォルトソケットは `/run/arca-router/routerd.sock` です。デーモン側で `--grpc-socket` を変更した場合は `arca -socket <path>` を使用します。

1. 設定モードに入る:
   ```bash
   arca
   > configure
   [edit]
   ```

2. 変更を投入:
   ```bash
   # set system host-name router-new
   # set interfaces ge-0/0/0 unit 0 family inet address 10.0.2.1/24
   ```

3. validate と commit:
   ```bash
   # commit check
   # commit
   # exit
   ```

4. 変更を確認:
   ```bash
   # show | compare
   ```

設定モードで利用できる主なコマンド:

```
set <config>              設定を追加または変更
delete <config>           prefix に一致する設定を削除
show                      candidate 設定を表示
show | compare            candidate と running の差分を表示
commit                    candidate 設定を commit
commit check              commit せずに検証
commit and-quit           commit 後に設定モードを終了
commit comment <msg>      commit message を指定
rollback <N>              N 個前の commit に rollback
discard-changes           candidate 変更を破棄
show history [N]          commit history を表示
edit <path>               hierarchy path に移動
up                        hierarchy を 1 階層上に移動
top                       hierarchy の top に戻る
```

### ロールバック

**NETCONF**:
```xml
<rpc message-id="104"><discard-changes/></rpc>
```

**対話型 CLI**:
```
[edit]
# rollback 1
# commit
```

`rollback 0` は `discard-changes` と同じです。`rollback <N>` は履歴上の対象 commit を復元する新しい commit を作成します。

**ファイルベース**:
```
# Restore from backup
sudo cp /etc/arca-router/arca-router.conf.backup /etc/arca-router/arca-router.conf
sudo systemctl restart arca-routerd
```

---

<a id="examples"></a>
## 例

### 例 1: BGP を使った基本ルータ

```
# System configuration
set system host-name edge-router-01

# Interface configuration
set interfaces ge-0/0/0 description "WAN Uplink"
set interfaces ge-0/0/0 unit 0 family inet address 198.51.100.1/30
set interfaces ge-0/0/1 description "LAN Interface"
set interfaces ge-0/0/1 unit 0 family inet address 192.168.1.1/24

# Routing options
set routing-options autonomous-system 65000
set routing-options router-id 198.51.100.1

# BGP configuration
set protocols bgp group external type external
set protocols bgp group external neighbor 198.51.100.2 peer-as 65001
set protocols bgp group external neighbor 198.51.100.2 description "ISP Router"

# Static default route
set routing-options static route 0.0.0.0/0 next-hop 198.51.100.2
```

### 例 2: OSPF とポリシーを使うルータ

```
# System configuration
set system host-name core-router-01

# Interface configuration
set interfaces ge-0/0/0 description "Core Link"
set interfaces ge-0/0/0 unit 0 family inet address 10.0.1.1/24
set interfaces ge-0/0/1 description "LAN Interface"
set interfaces ge-0/0/1 unit 0 family inet address 192.168.1.1/24

# Routing options
set routing-options router-id 10.0.1.1

# OSPF configuration
set protocols ospf router-id 10.0.1.1
set protocols ospf area 0.0.0.0 interface ge-0/0/0
set protocols ospf area 0.0.0.0 interface ge-0/0/1 passive

# Policy: Deny private prefixes
set policy-options prefix-list PRIVATE 10.0.0.0/8
set policy-options prefix-list PRIVATE 172.16.0.0/12
set policy-options prefix-list PRIVATE 192.168.0.0/16

set policy-options policy-statement DENY-PRIVATE term DENY from prefix-list PRIVATE
set policy-options policy-statement DENY-PRIVATE term DENY then reject

set policy-options policy-statement DENY-PRIVATE term ALLOW then accept
```

### 例 3: 複数プロトコル + セキュリティ設定

```
# System configuration
set system host-name mpls-pe-router-01

# Interface configuration
set interfaces ge-0/0/0 description "WAN Uplink"
set interfaces ge-0/0/0 unit 0 family inet address 198.51.100.1/30
set interfaces ge-0/0/0 unit 0 family inet6 address 2001:db8:1::1/64

set interfaces ge-0/0/1 description "LAN Interface"
set interfaces ge-0/0/1 unit 0 family inet address 192.168.1.1/24
set interfaces ge-0/0/1 unit 0 family inet6 address 2001:db8:2::1/64

# Routing options
set routing-options autonomous-system 65000
set routing-options router-id 198.51.100.1

# BGP configuration (IPv4 and IPv6)
set protocols bgp group external type external
set protocols bgp group external neighbor 198.51.100.2 peer-as 65001
set protocols bgp group external neighbor 198.51.100.2 description "ISP Router - IPv4"
set protocols bgp group external neighbor 2001:db8:1::2 peer-as 65001
set protocols bgp group external neighbor 2001:db8:1::2 description "ISP Router - IPv6"

# OSPF configuration
set protocols ospf router-id 198.51.100.1
set protocols ospf area 0.0.0.0 interface ge-0/0/1 passive

# Security configuration
set security netconf ssh port 830

set security users user admin password AdminPass123
set security users user admin role admin

set security users user operator password OpPass456
set security users user operator role operator

set security rate-limit per-ip 10
set security rate-limit per-user 20
```

---

<a id="runtime-options-and-observability"></a>
## 実行時オプションと Observability

### arca-routerd 実行時オプション

パッケージ版のサービスは `/usr/sbin/arca-routerd` を実行します。ソースビルドでは `build/bin/arca-routerd` が生成されます。

主なオプション:

```
--config <path>            bootstrap 設定ファイル（デフォルト: /etc/arca-router/arca-router.conf）
--hardware <path>          hardware mapping file（デフォルト: /etc/arca-router/hardware.yaml）
--datastore <path>         SQLite datastore（デフォルト: /var/lib/arca-router/config.db）
--datastore-backend <mode> configuration datastore backend: sqlite または etcd（デフォルト: sqlite）
--etcd-endpoints <list>    --datastore-backend=etcd 用の comma-separated etcd endpoints
--etcd-prefix <prefix>     etcd key prefix（デフォルト: /arca-router/）
--etcd-timeout <duration>  etcd connection / operation timeout（デフォルト: 5s）
--etcd-username <value>    etcd username
--etcd-password <value>    etcd password
--etcd-cert <path>         etcd TLS client certificate
--etcd-key <path>          etcd TLS client key
--etcd-ca <path>           etcd TLS CA certificate
--grpc-socket <path>       内部 gRPC Unix socket（デフォルト: /run/arca-router/routerd.sock）
--netconf-listen <addr>    NETCONF/SSH listen address。security netconf ssh port より優先（デフォルト: :830）
--host-key <path>          NETCONF SSH host key path
--user-db <path>           NETCONF user database path
--frr-apply-mode <mode>    FRR backend: transactional または file（デフォルト: transactional）
--metrics-listen <addr>    Prometheus listen address。system services prometheus config より優先
--web-listen <addr>        Web UI listen address。system services web-ui config より優先
--snmp-listen <addr>       SNMPv2c UDP listen address。空の場合は無効
--snmp-community <value>   SNMPv2c read-only community。system services snmp config より優先（デフォルト: public）
--mock-vpp                 test 用の mock VPP client を使用
```

### FRR apply backend

標準 backend は `transactional` です。FRR 側で `/etc/frr/daemons` の `mgmtd=yes` と、`arca-router` service user からの `vtysh` access（通常は `frrvty` group）が必要です。

arca-router 標準の FRR daemon set は `bgpd`、`ospfd`、`zebra`、`staticd`、`mgmtd`、`vrrpd` です。transactional backend は FRR の interface tree 配下にある `frr-vrrpd` YANG model を使って VRRP を適用します。`file` backend は full FRR config を書き出し、`frr-reload.py` で適用します。復旧・互換用途として保持しており、利用する場合は service user が `/etc/frr/frr.conf` に書き込むための追加権限が必要です。

### Prometheus と health

metrics endpoint は次のように起動します。

```bash
arca-routerd --metrics-listen=:9090
```

running configuration からも有効化できます。

```
set system services prometheus enabled true
set system services prometheus listen-address 127.0.0.1
set system services prometheus port 9090
```

Endpoints:

- `GET /metrics`
- `GET /healthz`

metrics endpoint は daemon uptime、running config version、NETCONF counters に加えて、cluster enabled state、node count、etcd sync configuration、datastore alignment の cluster sync gauge を出力します。

パッケージ版では Grafana dashboard を次の場所へインストールします。

```
/usr/share/arca-router/grafana/arca-routerd-dashboard.json
```

### Web UI

read-only Web UI は次のように起動します。

```bash
arca-routerd --web-listen=127.0.0.1:8080
```

設定からも有効化できます。

```
set system services web-ui enabled true
set system services web-ui listen-address 127.0.0.1
set system services web-ui port 8080
```

Endpoints:

- `GET /`
- `GET /api/config`
- `GET /api/config/history`
- `GET /api/status`
- `POST /api/config/validate`
- `POST /api/config/commit`

`/api/status` は build metadata、uptime、running config version、datastore backend、cluster sync state、NETCONF counters を返します。
`/api/config` は running configuration を set-command text と running config version として返します。dashboard でも同じ running configuration を browser editor に表示します。
`/api/config/history` は recent configuration commits を返し、dashboard の commit history panel で使用します。

running configuration に password 付きの `security users` が存在する場合、Web UI は HTTP Basic authentication を要求します。built-in の `read-only`、`operator`、`admin` role は read-only dashboard と API endpoints へのアクセスを許可されます。
configuration write には `operator` または `admin` が必要です。dashboard editor は `/api/config/validate` と `/api/config/commit` を呼び出します。`/api/config/validate` は `{ "config_text": "set ..." }` を受け取り、validation status と diff text を返します。`/api/config/commit` は `{ "config_text": "set ...", "message": "..." }` を受け取り、CLI と同じ internal gRPC candidate workflow で commit します。

### SNMP

read-only SNMPv2c endpoint は次のように起動します。

```bash
arca-routerd --snmp-listen=:1161 --snmp-community=public
```

running configuration からも有効化できます。

```
set system services snmp enabled true
set system services snmp listen-address 127.0.0.1
set system services snmp port 1161
set system services snmp community public
```

パッケージ版の systemd unit は `CAP_NET_BIND_SERVICE` を付与しているため、設定すれば標準 UDP port 161 も利用できます。

```bash
arca-routerd --snmp-listen=:161 --snmp-community=<read-only-community>
```

SNMP は監視用途のみを想定しています。信頼できないネットワークには公開しないでください。

---

<a id="operational-commands"></a>
## 運用コマンド

### show コマンド（arca）

```
# Interface status
arca show interfaces
arca show interfaces ge-0/0/0

# Routing table
arca show route
arca show route protocol bgp

# BGP summary
arca show bgp summary

# BGP neighbors
arca show bgp neighbor <ip>

# OSPF neighbors
arca show ospf neighbor

# Configuration
arca show configuration
```

対話型の設定モードでは、`show history [N]` で commit history も表示できます。

### VPP 直接操作

```
# Interface status
sudo vppctl show interface

# Linux Control Plane (LCP) status
sudo vppctl show lcp

# IP forwarding table
sudo vppctl show ip fib

# IPv6 forwarding table
sudo vppctl show ip6 fib
```

### FRR 直接操作

```
# Enter FRR CLI
sudo vtysh

# Show running config
show running-config

# Show IP routes
show ip route

# Show BGP summary
show ip bgp summary

# Show BGP neighbors
show ip bgp neighbors

# Show OSPF neighbors
show ip ospf neighbor
```

---

## 設定の妥当性確認

### 構文検証

```
# Interactive candidate validation
arca
> configure
[edit]
# commit check
```

### デプロイ前チェック

```
# package metadata と service expectation を検証
make package-lint

# FRR mgmtd が有効なホストで transactional apply smoke test を実行
make frr-mgmtd-smoke

# FRR configuration is generated/applied by arca-routerd; verify on the host using vtysh

# Check BGP session status
sudo vtysh -c "show ip bgp summary"

# Check OSPF neighbors
sudo vtysh -c "show ip ospf neighbor"
```

---

## トラブルシューティング

### デーモン状態確認

```
sudo systemctl status arca-routerd
sudo journalctl -u arca-routerd -n 50
```

### Datastore と socket の確認

```
# running/candidate datastore
sudo ls -l /var/lib/arca-router/config.db

# arca が利用する内部 gRPC socket
sudo ls -l /run/arca-router/routerd.sock
```

### VPP 状態確認

```
sudo systemctl status vpp
sudo vppctl show interface
```

### FRR 状態確認

```
sudo systemctl status frr
grep '^mgmtd=yes' /etc/frr/daemons
sudo vtysh -c "show running-config"
```

### Observability endpoint の確認

```
# --metrics-listen または system services prometheus 有効時の Prometheus / health
curl http://127.0.0.1:9090/healthz
curl http://127.0.0.1:9090/metrics

# --web-listen または system services web-ui 有効時の Web UI
curl http://127.0.0.1:8080/api/status
curl http://127.0.0.1:8080/api/config

# --snmp-listen または system services snmp 有効時の SNMP
snmpget -v 2c -c public 127.0.0.1:1161 1.3.6.1.3.9950.1.3.0
```

### インターフェースマッピング確認

```
# Check hardware.yaml mappings
cat /etc/arca-router/hardware.yaml

# Verify PCI addresses
lspci | grep Ethernet

# Check VPP interface binding
sudo vppctl show interface addr
```

---

## 参照

- [Roadmap](ROADMAP.md)
- [Changelog](CHANGELOG.md)
- [Observability](docs/observability.md)
- [Datastore Design](docs/datastore-design.md)
- [Configuration Precedence Rules](docs/config-precedence.md)
- [Policy Options Guide](docs/policy-options-guide.md)
- [RBAC Guide](docs/rbac-guide.md)
- [Security Model](docs/security-model.md)
- [VPP Setup Guide](docs/vpp-setup-debian.md)
- [FRR Setup Guide](docs/frr-setup-debian.md)

---

## バージョン履歴

- **v0.6.x**: Advanced feature foundations
  - clustering、MPLS、VRRP、routing instances、class of service、Web UI の management-plane config model
  - clustered candidate/running configuration 向け etcd datastore backend selection
  - read-only Web UI dashboard、JSON status endpoint、running config endpoint
  - v0.6 config diff と candidate replacement coverage

- **v0.5.x**: Production hardening
  - 現行コマンド名は `arca-routerd` と `arca`
  - daemon/CLI 間の generated gRPC API
  - SQLite-backed candidate/running datastore と commit history
  - FRR management candidate datastore を使う transactional apply
  - Prometheus、health、SNMP、Grafana observability
  - 廃止済み command entrypoint を削除

- **v0.4.x**: 統合アーキテクチャ
  - VPP、FRR、NETCONF、gRPC を単一 daemon に統合
  - 構造体ファースト設定モデル
  - 差分ベース apply engine と plugin southbound
  - gRPC シンクライアント CLI

- **v0.3.1** (2025-12-28): 仕様策定（完了）
  - NETCONF/SSH サブシステム
  - 対話型 CLI
  - Policy Options（prefix-list, policy-statement）
  - セキュリティ機能（RBAC、レート制限、監査ログ）
  - 設定ワークフロー（commit/rollback）

- **v0.2.x**: VPP と FRR の統合
  - 実 VPP 統合
  - FRR によるルーティング（BGP, OSPF）
  - LCP（Linux Control Plane）

- **v0.1.x**: mock VPP の MVP
  - 設定パーサ
  - systemd 統合
  - RPM/DEB パッケージング
