# arca-router

[![Build and Test](https://github.com/akam1o/arca-router/actions/workflows/build.yml/badge.svg?branch=main)](https://github.com/akam1o/arca-router/actions/workflows/build.yml)
[![Release](https://github.com/akam1o/arca-router/actions/workflows/release.yml/badge.svg)](https://github.com/akam1o/arca-router/actions/workflows/release.yml)

[English](README.md) | 日本語

**Junos 互換の設定構文を備えた高性能ソフトウェアルータ**

arca-router は、VPP（Vector Packet Processing）と FRR（Free Range Routing）を基盤に、Junos 互換の設定構文で運用できるソフトウェアルータです。動的ルーティングプロトコルにも対応します。

---

## ステータス

arca-router は v0.9.x の NETCONF/YANG and operational safety 実装を完了し、現在は v0.10.x の stabilization and compatibility フェーズです。この README は
現在の統合デーモン経路を説明します。詳細なリリース履歴は
[`CHANGELOG.md`](CHANGELOG.md)、今後の計画は [`ROADMAP.md`](ROADMAP.md) に
集約しています。

現在の主な機能:

- VPP、FRR、NETCONF、gRPC を単一の `arca-routerd` プロセスに統合
- Junos-like な `set` 設定構文とシン `arca` クライアント
- 構造体ファースト設定モデル、差分ベース 2 フェーズコミット、ロールバック
- FRR management candidate datastore 経由の transactional apply
- clustering、VRRP、MPLS、routing instances、QoS、IPv6 parity、BFD、EVPN/VXLAN、streaming telemetry、NMS API 向け v0.6-v0.8 設定・observability 基盤
- Prometheus、health、SNMP、Web UI、Grafana によるオブザーバビリティと認証付き Web 設定ワークフロー
- SQLite または etcd ベースの candidate/running datastore とコミット履歴
- v0.10 互換性 preflight、schema migration guardrail、support matrix 表示、gRPC TLS/mTLS、Web API token auth、admin 限定 audit export
- v0.10 release readiness/sign-off checklist、NETCONF client evidence gate、installed-host security audit

---

## 前提条件

### システム要件

- **OS**: Debian 12（Bookworm）または RHEL 9 / AlmaLinux 9 / Rocky Linux 9
- **CPU**: x86_64（マルチコア推奨、2+ cores）
- **メモリ**: 4GB+ RAM（VPP は hugepages を使用）
- **NIC**: Intel（AVF）または Mellanox（RDMA）互換 NIC

### 必要ソフトウェア

- **VPP 24.10+**: Vector Packet Processing フレームワーク
  - [VPP Setup Guide (Debian)](docs/vpp-setup-debian.md) / [VPP Setup Guide (RHEL9)](docs/vpp-setup-rhel9.md)

- **FRR 8.0+**: 動的ルーティングプロトコルのための Free Range Routing
  - [FRR Setup Guide (Debian)](docs/frr-setup-debian.md) / [FRR Setup Guide (RHEL9)](docs/frr-setup-rhel9.md)
  - `/etc/frr/daemons` で `bgpd`、`ospfd`、`zebra`、`staticd`、`mgmtd`、`vrrpd`、`bfdd` を有効化してください

- **Go 1.25+**: ソースからビルドする場合（任意）

---

## クイックスタート

VPP 24.10+ と、arca-router 標準の FRR daemon set を有効化した FRR 8.0+ が必要です。

### 1. 前提ソフトをインストール

**Debian Bookworm**:
```bash
# Install VPP 24.10
curl -s https://packagecloud.io/install/repositories/fdio/2410/script.deb.sh | sudo bash
sudo apt-get install -y vpp=24.10-release vpp-plugin-core=24.10-release

# Install FRR
sudo apt-get install -y frr frr-pythontools

# See detailed setup guides:
# - docs/vpp-setup-debian.md
# - docs/frr-setup-debian.md
```

> RHEL 注: FD.io は RHEL9 向けに VPP 24.10 の RPM を配布していません。インストール前に [docs/vpp-setup-rhel9.md](docs/vpp-setup-rhel9.md) の手順で VPP をソースからビルドしてください。

**RHEL 9 / AlmaLinux 9 / Rocky Linux 9**:
```bash
# Build VPP 24.10 RPMs from source (see docs/vpp-setup-rhel9.md), then install VPP + FRR
sudo dnf install -y /path/to/vpp-24.10-*.rpm /path/to/vpp-plugin-core-24.10-*.rpm frr frr-pythontools
```

### 2. arca-router をインストール

**Debian Bookworm**:
```bash
# Install DEB package
sudo dpkg -i arca-router_*.deb

# Verify installation
/usr/sbin/arca-routerd --version
arca --version
```

**RHEL 9 / AlmaLinux 9 / Rocky Linux 9**:
```bash
# Install RPM package
sudo dnf install -y ./arca-router-*.rpm

# Verify installation
/usr/sbin/arca-routerd --version
arca --version
```

root 以外の運用ユーザーで `arca` を使う場合は、そのログインユーザーを
`arca-router` グループに追加し、ログインし直してください。

```bash
sudo usermod -aG arca-router $USER
```

### 3. ハードウェアマッピングを設定

例の設定をコピーして編集します。

```bash
# Copy example configs
sudo cp /etc/arca-router/hardware.yaml.example /etc/arca-router/hardware.yaml
sudo cp /etc/arca-router/arca-router.conf.example /etc/arca-router/arca-router.conf
```

`/etc/arca-router/hardware.yaml` を編集:

```yaml
interfaces:
  - name: "ge-0/0/0"
    pci: "0000:03:00.0"
    driver: "avf"
    description: "WAN Uplink"
  - name: "ge-0/0/1"
    pci: "0000:03:00.1"
    driver: "avf"
    description: "LAN Interface"
```

NIC の PCI アドレスを確認:

```bash
lspci | grep Ethernet
```

### 4. インターフェースとルーティングを設定

`/etc/arca-router/arca-router.conf` を編集し、インターフェースとルーティングプロトコルを設定します。

```
# System configuration
set system host-name arca-router-01

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

# OSPF configuration
set protocols ospf area 0.0.0.0 interface ge-0/0/1
set protocols ospf router-id 198.51.100.1

# Static routes
set routing-options static route 0.0.0.0/0 next-hop 198.51.100.2
```

完全な例は [`examples/arca-router.conf`](examples/arca-router.conf) を参照してください。

### 5. arca-router を起動

```bash
# Start the service
sudo systemctl start arca-routerd

# Enable at boot
sudo systemctl enable arca-routerd

# Check status
sudo systemctl status arca-routerd

# View logs
sudo journalctl -u arca-routerd -f
```

### 6. （任意）NETCONF とセキュリティを設定

**NETCONF サーバを有効化**:

`/etc/arca-router/arca-router.conf` を編集し、NETCONF を有効化してユーザを作成します。

```
# Enable NETCONF on port 830
set security netconf ssh port 830

# Create admin user
set security users user admin password YourSecurePassword123
set security users user admin role admin

# Create operator user for automation
set security users user operator password OperatorPass456
set security users user operator role operator

# Rate limiting
set security rate-limit per-ip 10
set security rate-limit per-user 20
```

> NETCONF は `arca-routerd` に統合されています。別プロセスの NETCONF デーモンは不要です。`--netconf-listen` を省略した場合、デーモンは設定された NETCONF ポートで待ち受け、未設定時は `:830` を使用します。

**NETCONF 接続のテスト**:

```bash
# Connect via NETCONF (requires netconf-console or similar client)
netconf-console --host localhost --port 830 --user admin --password YourSecurePassword123
```

### 7. 設定を確認

```bash
# Check daemon logs
sudo journalctl -u arca-routerd -n 50

# arca で running configuration を確認
arca show configuration

# arca-routerd 経由で interface state、counter、QoS profile、queue placement、QoS capability を確認
arca show interfaces
arca show routing-instances
arca show routes
arca show routes protocol bgp
arca show route
arca show route inet6
arca show bgp neighbors
arca show bgp summary
arca show ospf neighbor
arca show ospf3 neighbor
arca show vrrp
arca show bfd status
arca show bfd
arca show bfd counters
arca show evpn
arca show lcp
arca show ha
arca show class-of-service
arca show telemetry paths
arca show telemetry paths live
arca show telemetry paths cardinality per-route
arca show telemetry paths live payload-schema arca.telemetry.routes.v1
arca show telemetry path /system path /interfaces path /overlays/evpn

# Web API が有効な場合は schema-versioned NMS status API を確認
curl -u monitor:ReadOnly789 http://127.0.0.1:8080/api/nms/v1/status
curl -u monitor:ReadOnly789 http://127.0.0.1:8080/api/nms/v1/telemetry/paths
curl -u monitor:ReadOnly789 'http://127.0.0.1:8080/api/nms/v1/telemetry/paths?cardinality=per-route&payload_schema=arca.telemetry.routes.v1'
curl -u monitor:ReadOnly789 'http://127.0.0.1:8080/api/nms/v1/telemetry/paths?path=system,evpn&encoding=json'
curl -u monitor:ReadOnly789 'http://127.0.0.1:8080/api/nms/v1/telemetry/schemas?path=/evpn'
curl -u monitor:ReadOnly789 'http://127.0.0.1:8080/api/nms/v1/telemetry/snapshot?path=/system&path=/interfaces&path=/overlays/evpn&timeout=5s&max_payload_bytes=8388608&max_events=64'
curl -u monitor:ReadOnly789 'http://127.0.0.1:8080/api/nms/v1/telemetry/snapshot?cardinality=per-route&payload_schema=arca.telemetry.routes.v1&max_events=1'

# VPP/FRR を直接確認（任意）
sudo vppctl show interface
sudo vppctl show lcp
sudo vtysh -c 'show running-config'
sudo vtysh -c 'show ip route'
```

---

## 設定リファレンス

設定構文と、対応している `set` 階層（hierarchy）は [`SPEC.ja.md`](SPEC.ja.md) にまとめています（英語版: [`SPEC.md`](SPEC.md)）。

トップレベルのスタンザ:

- `system`
- `interfaces`
- `routing-options`
- `protocols`
- `policy-options`
- `security`

### インターフェース命名規則

- `ge-X/Y/Z`: Gigabit Ethernet（1GbE）
- `xe-X/Y/Z`: 10 Gigabit Ethernet（10GbE）
- `et-X/Y/Z`: 100 Gigabit Ethernet（100GbE）

---

## ソースからビルド

### 前提

- Go 1.25+
- NFPM 2.35.0+（DEB/RPM パッケージング用）

### 手順

```bash
# Clone repository
git clone https://github.com/akam1o/arca-router.git
cd arca-router

# Build binaries
make build

# Run tests
make test

# Build DEB package (nfpm config: build/package/nfpm.yaml)
make deb

# Build RPM package
make rpm

# Packages will be in dist/ directory
ls -lh dist/
```

### Makefile ターゲット

```bash
make help             # Show all available targets
make version          # Display version information
make build            # v0.5.x 統合デーモン + CLI をビルド
make build-cli        # 現行 arca CLI のみビルド
make generate-proto   # typed gRPC bindings を生成
make test             # Run unit tests
make integration-test # Run integration tests
make package-lint     # Validate package metadata and service expectations
make frr-mgmtd-smoke  # Run live FRR mgmtd smoke test
make fmt              # Format code
make vet              # Run go vet
make check            # Run all checks (fmt, vet, test)
make clean            # Clean build artifacts
make install-nfpm     # Install NFPM tool
make deb              # Build DEB package
make deb-test         # Test DEB package metadata
make deb-verify       # Verify DEB package reproducibility
make rpm              # Build RPM package
make rpm-test         # Test RPM package metadata
make rpm-verify       # Verify reproducible build
make packages         # Build both RPM and DEB packages
```

---

## プロジェクト構成

```
arca-router/
├── api/
│   └── v1/
│       └── router.proto        # gRPC API 定義（Config/Session/State/Telemetry）
├── cmd/
│   ├── arca-routerd/           # 統合デーモン
│   │   └── main.go             # 単一プロセス: VPP + FRR + NETCONF + gRPC
│   └── arca/                   # シン gRPC CLI クライアント
│       └── main.go             # Unix ソケット経由で通信
├── internal/                   # v0.6.x-v0.8.x コアパッケージ
│   ├── model/                  # 正準的な設定 & 状態型
│   │   ├── config.go           # RouterConfig（構造体ファーストモデル）
│   │   ├── state.go            # OperationalState
│   │   ├── validate.go         # バリデーションロジック
│   │   └── convert.go          # テキスト設定 ↔ 正準モデル変換
│   ├── engine/                 # 設定エンジン
│   │   ├── engine.go           # 2 フェーズコミット、アトミック適用
│   │   ├── diff.go             # 最小差分計算
│   │   └── plugin.go           # サウスバウンドプラグインインターフェース
│   ├── southbound/
│   │   ├── vpp/plugin.go       # VPP プラグイン（govpp）
│   │   └── frr/plugin.go       # FRR プラグイン（transactional/file apply）
│   ├── northbound/
│   │   └── grpc/               # gRPC サーバ + クライアント
│   │       ├── server.go       # セッション管理、設定操作
│   │       └── client.go       # CLI 用シンクライアント
│   ├── store/                  # 永続化抽象化
│   │   ├── store.go            # ConfigStore インターフェース
│   │   └── sqlite/sqlite.go    # SQLite バックエンド
│   └── auth/auth.go            # 認証/RBAC/監査ラッパー
├── pkg/                        # デーモンと CLI で使う再利用パッケージ
│   ├── config/                 # set コマンドパーサー
│   ├── vpp/                    # VPP クライアントインターフェース
│   ├── frr/                    # FRR 設定ジェネレーター
│   ├── datastore/              # SQLite/etcd データストア
│   ├── netconf/                # NETCONF/SSH サーバ
│   ├── cli/                    # CLI セッション管理
│   ├── auth/                   # パスワード/SSH 鍵認証
│   ├── audit/                  # 監査ログ
│   ├── device/                 # ハードウェア抽象化
│   ├── logger/                 # 構造化ログ
│   └── errors/                 # エラーハンドリング
├── build/
│   ├── systemd/                # systemd ユニットファイル
│   └── package/                # nfpm パッケージング設定
├── docs/                       # ドキュメント
├── examples/                   # サンプル設定と連携例
└── Makefile                    # ビルド自動化
```

---

## ドキュメント

- [Documentation Index](docs/README.md) - ドキュメント一覧
- [Roadmap](ROADMAP.md) - 今後の機能計画
- [VPP Setup Guide for Debian](docs/vpp-setup-debian.md) - Debian 向け VPP インストール
- [VPP Setup Guide for RHEL9](docs/vpp-setup-rhel9.md) - RHEL9 向け VPP インストール
- [FRR Setup Guide for Debian](docs/frr-setup-debian.md) - Debian 向け FRR インストール
- [FRR Setup Guide for RHEL9](docs/frr-setup-rhel9.md) - RHEL9 向け FRR インストール
- [設定仕様（日本語）](SPEC.ja.md) - 設定構文と意味
- [Design Specification](SPEC.md) - アーキテクチャと設計判断（英語）
- [JSON Schema Convention](docs/json-schema-convention.md) - 命名規約
- [Changelog](CHANGELOG.md) - リリース履歴
- [Support Policy](SUPPORT.md) - サポート窓口

---

## コントリビュート

コントリビュート歓迎です。[`CONTRIBUTING.md`](CONTRIBUTING.md) を参照してください。

---

## ライセンス

Apache License 2.0 で提供しています。[`LICENSE`](LICENSE) を参照してください。

---

## サポート

- **Community Support**: GitHub Issues - https://github.com/akam1o/arca-router/issues
- **Support Policy**: [`SUPPORT.md`](SUPPORT.md)
- **Security**: [`SECURITY.md`](SECURITY.md)

---

## 謝辞

- **VPP**: [FD.io Vector Packet Processing](https://fd.io/)
- **FRR**: [Free Range Routing](https://frrouting.org/)
- **NFPM**: [GoReleaser NFPM](https://nfpm.goreleaser.com/)
