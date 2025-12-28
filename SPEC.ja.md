# arca-router 設定仕様（v0.3.x）

このドキュメントは arca-router v0.3.x の設定構文とセマンティクスを定義します。

[English](SPEC.md)

## 概要

arca-router は Junos 風の `set` コマンド構文を採用しています。設定は以下の方法で管理します。

1. **NETCONF/SSH**: NETCONF（RFC 6241）によるリモート設定
2. **対話型 CLI**: `arca-cli` によるリアルタイム設定（commit/rollback）
3. **ファイルベース**: 静的設定ファイル（`/etc/arca-router/arca-router.conf`）

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
7. [セキュリティ](#security)
   - [NETCONF サーバ](#netconf-server)
   - [ユーザ管理](#user-management)
   - [レート制限](#rate-limiting)
8. [設定ワークフロー](#configuration-workflow)
9. [例](#examples)

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

<a id="security"></a>
## セキュリティ

<a id="netconf-server"></a>
### NETCONF サーバ

#### NETCONF サーバを有効化

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

**注**: NETCONF サーバは `arca-netconfd` デーモン（`arca-routerd` とは別プロセス）で管理されます。

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

1. `/etc/arca-router/arca-router.conf` を編集
2. デーモン再起動: `sudo systemctl restart arca-routerd`
3. 確認: `sudo journalctl -u arca-routerd -n 50`

### NETCONF 設定

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

1. 設定モードに入る:
   ```bash
   arca-cli
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
   > show configuration changes
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

## 運用コマンド

### show コマンド（arca-cli）

```
# Interface status
arca-cli show interfaces

# Routing table
arca-cli show route

# BGP summary
arca-cli show bgp summary

# BGP neighbors
arca-cli show bgp neighbor <ip>

# OSPF neighbors
arca-cli show ospf neighbor

# Configuration
arca-cli show configuration
```

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
arca-cli
> configure
[edit]
# commit check
```

### デプロイ前チェック

```
# FRR configuration is generated/applied by arca-routerd; verify on the host using vtysh.

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

### VPP 状態確認

```
sudo systemctl status vpp
sudo vppctl show interface
```

### FRR 状態確認

```
sudo systemctl status frr
sudo vtysh -c "show running-config"
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

- [Policy Options Guide](docs/policy-options-guide.md)
- [RBAC Guide](docs/rbac-guide.md)
- [Security Model](docs/security-model.md)
- [VPP Setup Guide](docs/vpp-setup-debian.md)
- [FRR Setup Guide](docs/frr-setup-debian.md)
- [NETCONF Implementation Plan](docs/netconf-implementation-plan.md)

---

## バージョン履歴

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
