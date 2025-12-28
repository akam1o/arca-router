# arca-router

**Junos 互換の設定構文を備えた高性能ソフトウェアルータ**

arca-router は、VPP（Vector Packet Processing）と FRR（Free Range Routing）を基盤に、Junos 互換の設定構文で運用できるソフトウェアルータです。動的ルーティングプロトコルにも対応します。

**現在のステータス**: v0.3.x - **NETCONF 管理 & セキュリティ**

[English](README.md)

---

## リリース

過去リリースは [`CHANGELOG.md`](CHANGELOG.md) に記載しています。

### v0.3.x - **現行リリース** ✅

- ✅ **NETCONF/SSH サブシステム**: NETCONF（RFC 6241）によるリモート管理
- ✅ **対話型 CLI**: リアルタイム設定、commit/rollback
- ✅ **高度な Policy Options**: ルートフィルタ、ポリシーベースルーティング（prefix-list, policy-statement）
- ✅ **セキュリティ機能**: 認証（パスワード + SSH 鍵）、RBAC（admin/operator/read-only）、レート制限、監査ログ
- ✅ **設定データストア**: candidate/running の二重化、コミット履歴
- ✅ **CI/CD**: GitHub Actions による自動ビルド/テスト/リリース

---

## ロードマップ

### v0.4.x - Advanced VPP Features 🔲

- 🔲 **マルチシャーシ/クラスタリング**
  - Control plane HA (FRR + VRRP)
  - Config sync (etcd)
- 🔲 **MPLS/VPN**
  - MPLS ラベルスイッチング (VPP)
  - L3VPN (FRR + VPP)
- 🔲 **QoS/トラフィックエンジニアリング**
  - VPP QoS ポリシー
  - トラフィックシェーピング
- 🔲 **高度な VPP ポリシー**
  - VPP ACL
  - ポリシーベースルーティング（VPP ネイティブ）
- 🔲 **監視/オブザーバビリティ**
  - Prometheus exporter
  - Grafana dashboard
  - SNMP（任意）
- 🔲 **Web UI**
  - ブラウザでの監視・設定

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

- **Go 1.25+**: ソースからビルドする場合（任意）

---

## クイックスタート（v0.3.x）

✅ **現行リリース（v0.3.x）**: VPP 24.10+ と FRR 8.0+ が必要です

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
arca-cli --version
```

**RHEL 9 / AlmaLinux 9 / Rocky Linux 9**:
```bash
# Install RPM package
sudo dnf install -y ./arca-router-*.rpm

# Verify installation
/usr/sbin/arca-routerd --version
arca-cli --version
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

**NETCONF デーモンを起動**:

```bash
# Start arca-netconfd
sudo systemctl start arca-netconfd

# Enable at boot
sudo systemctl enable arca-netconfd

# Check status
sudo systemctl status arca-netconfd
```

**NETCONF 接続のテスト**:

```bash
# Connect via NETCONF (requires netconf-console or similar client)
netconf-console --host localhost --port 830 --user admin --password YourSecurePassword123
```

### 7. 設定を確認

```bash
# Check daemon logs
sudo journalctl -u arca-routerd -n 50

# View interface status with arca-cli
arca-cli show interfaces

# View routing table
arca-cli show route

# View BGP status
arca-cli show bgp summary

# View OSPF neighbors
arca-cli show ospf neighbor

# Check VPP directly (optional)
sudo vppctl show interface
sudo vppctl show lcp
sudo vppctl show ip fib

# Check FRR directly (optional)
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

# Build binary (with mock VPP flag)
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
make build            # Build binary
make test             # Run unit tests
make integration-test # Run integration tests
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
├── cmd/
│   └── arca-routerd/       # Main daemon
│       ├── main.go         # Entry point
│       ├── apply.go        # Configuration application
│       └── vpp_factory.go  # VPP client factory (mock/real)
├── pkg/
│   ├── config/             # Configuration parser (set syntax)
│   ├── device/             # Hardware abstraction (PCI/sysfs)
│   ├── vpp/                # VPP client interface
│   ├── logger/             # Structured logging
│   └── errors/             # Error handling
├── build/
│   ├── systemd/            # systemd unit files
│   └── package/            # nfpm packaging config and scripts
├── docs/                   # Documentation
├── examples/               # Sample configurations
└── Makefile                # Build automation
```

---

## ドキュメント

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
- **Trademark**: [`TRADEMARK.md`](TRADEMARK.md)

---

## 謝辞

- **VPP**: [FD.io Vector Packet Processing](https://fd.io/)
- **FRR**: [Free Range Routing](https://frrouting.org/)
- **NFPM**: [GoReleaser NFPM](https://nfpm.goreleaser.com/)
