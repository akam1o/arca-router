# arca-router セキュリティモデル

**バージョン**: 2.0.0
**更新日**: 2025-12-28
**ステータス**: Phase 3 実装完了

---

## 概要

本ドキュメントは、arca-routerの実行権限、Linux Capabilities、ファイル権限、セキュリティ境界を定義する。VPP/FRR統合に必要な最小権限を確保しつつ、セキュリティリスクを最小化することを目標とする。

**Phase 3追加機能**:
- NETCONF/SSH認証 (password + SSH public key)
- RBAC (Role-Based Access Control): admin/operator/read-only
- Rate limiting (IP-based / User-based)
- Audit logging (authentication, authorization, configuration changes)
- Secrets management (password hashing with argon2id, SSH key permissions)

---

## 1. 実行権限モデル

### 1.1 arca-routerd の実行ユーザー

**推奨方式: 専用ユーザー + Linux Capabilities**

| 項目 | 設定値 | 理由 |
|------|--------|------|
| **実行ユーザー** | `arca-router` (専用ユーザー) | root実行を避け、権限を制限 |
| **プライマリグループ** | `arca-router` | 設定ファイル所有権管理 |
| **セカンダリグループ** | `vpp`, `frr`, `frrvty` | VPP/FRRリソースへのアクセス |
| **ホームディレクトリ** | `/var/lib/arca-router` | 状態ファイル・キャッシュ保存先 |

#### 専用ユーザー作成（RPM postinstallスクリプトで実施）

```bash
# Create arca-router user if not exists
if ! id arca-router &>/dev/null; then
    useradd --system --no-create-home \
        --home-dir /var/lib/arca-router \
        --shell /sbin/nologin \
        --comment "arca-router service user" \
        arca-router
fi

# Add arca-router to VPP/FRR groups
usermod -aG vpp arca-router
usermod -aG frr arca-router
usermod -aG frrvty arca-router
```

---

### 1.2 root実行が必要な操作

以下の操作はroot権限（またはCapabilities）が必要:

| 操作 | 必要な権限 | 理由 |
|------|-----------|------|
| VPP API socket接続 (`/run/vpp/api.sock`) | `vpp`グループ所属 | VPP APIアクセス |
| FRR設定ファイル書き込み (`/etc/frr/frr.conf`) | `frr`グループ所属 | FRR設定管理 |
| LCP (Linux Control Plane) 操作 | `CAP_NET_ADMIN` | netlink/TAP操作 |
| VPP FIB操作（経路同期） | `CAP_NET_ADMIN` | Kernel routing table操作 |

---

### 1.3 Linux Capabilities設計

**採用方式: systemd AmbientCapabilities**

最小限のCapabilitiesを付与し、root権限を回避する。

#### 必要なCapabilities

| Capability | 用途 | 必須/推奨 |
|-----------|------|----------|
| `CAP_NET_ADMIN` | netlink操作、LCP、VPP FIB同期 | **必須** |
| `CAP_NET_RAW` | Raw socket操作（将来のBFD/LLDP用） | 推奨 (Phase 3以降) |

#### systemd unit fileへの反映

[build/systemd/arca-routerd.service](../build/systemd/arca-routerd.service:1):

```ini
[Service]
User=arca-router
Group=arca-router
SupplementaryGroups=vpp frr frrvty

# Linux Capabilities
AmbientCapabilities=CAP_NET_ADMIN
CapabilityBoundingSet=CAP_NET_ADMIN

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/arca-router /run/arca-router /var/log/arca-router /etc/frr

# VPP/FRR integration
RuntimeDirectory=arca-router
StateDirectory=arca-router
LogsDirectory=arca-router
```

---

## 2. ファイル権限

### 2.1 設定ファイル

| ファイルパス | 所有者:グループ | 権限 | 理由 |
|-------------|----------------|------|------|
| `/etc/arca-router/arca-router.conf` | `root:arca-router` | `0640` | 設定には機密情報を含む可能性 |
| `/etc/arca-router/hardware.yaml` | `root:arca-router` | `0640` | PCIアドレスは機密情報ではないが統一 |
| `/etc/frr/frr.conf` | `root:frr` | `0660` | arca-routerdが書き込み可能（frrグループ所属） |

#### 設定ファイル作成時の権限設定

```go
// pkg/config/writer.go
func writeConfigFile(path string, content []byte) error {
    // Atomic write: tmp → rename
    tmpPath := path + ".tmp"

    // Write with restrictive permissions
    if err := os.WriteFile(tmpPath, content, 0640); err != nil {
        return err
    }

    // Set ownership (root:arca-router)
    if err := os.Chown(tmpPath, 0, getArcaRouterGID()); err != nil {
        return err
    }

    // Atomic rename
    return os.Rename(tmpPath, path)
}
```

---

### 2.2 VPP/FRR リソース権限

| リソース | 所有者:グループ | 権限 | アクセス方法 |
|---------|----------------|------|-------------|
| `/run/vpp/api.sock` | `root:vpp` | `0660` | `vpp`グループ所属で接続 |
| `/etc/frr/frr.conf` | `root:frr` | `0660` | `frr`グループ所属で読み書き（arca-routerdが直接書き込み） |
| `/run/frr/` | `frr:frr` | `0775` | FRR socket通信 |

#### VPP API socket権限の確認

VPP起動時に`/run/vpp/api.sock`が正しい権限で作成されるかを確認:

```bash
# VPP startup.conf設定
unix {
  api-segment {
    gid vpp
  }
}
```

RPM postinstallで権限を確認:

```bash
# Verify VPP socket permissions
if [ -e /run/vpp/api.sock ]; then
    SOCK_GROUP=$(stat -c %G /run/vpp/api.sock)
    if [ "$SOCK_GROUP" != "vpp" ]; then
        echo "Warning: /run/vpp/api.sock group is $SOCK_GROUP (expected: vpp)"
        echo "Update /etc/vpp/startup.conf: unix { api-segment { gid vpp } }"
    fi
fi
```

---

### 2.3 ランタイムディレクトリ

| ディレクトリ | 所有者:グループ | 権限 | 用途 |
|-------------|----------------|------|------|
| `/var/lib/arca-router` | `arca-router:arca-router` | `0750` | 状態ファイル、キャッシュ |
| `/run/arca-router` | `arca-router:arca-router` | `0750` | PIDファイル、ソケット |
| `/var/log/arca-router` | `arca-router:arca-router` | `0750` | ログファイル |

systemd `RuntimeDirectory`/`StateDirectory`により自動作成される。

---

## 3. セキュリティ境界

### 3.1 信頼境界

```
┌─────────────────────────────────────────────┐
│  管理者 (SSH/Console)                        │
│  - 設定ファイル編集                           │
│  - systemctl restart arca-routerd           │
└─────────────────────────────────────────────┘
                    ↓ (root権限)
┌─────────────────────────────────────────────┐
│  arca-routerd (arca-router user)            │
│  - 設定パース                                │
│  - VPP/FRR設定生成                           │
│  - 設定適用                                  │
└─────────────────────────────────────────────┘
          ↓ (vpp group)      ↓ (frr group)
┌──────────────────┐   ┌─────────────────────┐
│  VPP (root)      │   │  FRR (frr user)     │
│  - Dataplane     │   │  - Routing daemon   │
│  - LCP           │   │  - BGP/OSPF         │
└──────────────────┘   └─────────────────────┘
```

#### 信頼モデル

| レイヤー | 信頼レベル | 入力検証 |
|---------|-----------|---------|
| 管理者 | **信頼** | 最小限（構文エラー検出のみ） |
| arca-routerd | **準信頼** | 厳密な検証（型、範囲、依存関係） |
| VPP/FRR | **信頼** | arca-routerdの出力を信頼 |

**設計原則**:
- 管理者入力は信頼するが、設定ミスを防ぐため構文エラーは検出
- arca-routerdは設定の一貫性を保証（不正な設定を拒否）
- VPP/FRRへの入力は既に検証済みと仮定（追加検証なし）

---

### 3.2 攻撃面とリスク

| 攻撃面 | リスク | 緩和策 |
|-------|--------|--------|
| 設定ファイル改ざん | 不正なルーティング設定 | root権限のみ書き込み可能、設定検証 |
| VPP API socket不正アクセス | VPP設定改ざん | vppグループのみアクセス、ソケット権限 |
| FRR設定ファイル改ざん | ルーティング乗っ取り | frrグループのみアクセス、atomic write |
| arca-routerdプロセス乗っ取り | システム権限昇格 | Capabilities制限、systemd sandboxing |
| LCP操作悪用 | Kernel network stack操作 | CAP_NET_ADMIN最小化、入力検証 |

#### 追加セキュリティ対策（Phase 3以降）

- **設定ファイル暗号化**: 機密情報（BGP password等）の暗号化
- **Audit log**: 設定変更のログ記録
- **RBAC**: 複数管理者向けの役割ベースアクセス制御
- **SELinux policy**: RHEL 9でのSELinuxポリシー定義

---

## 4. VPP/FRR操作権限

### 4.1 VPP操作

| 操作 | 必要な権限 | 実装方法 |
|------|-----------|---------|
| VPP API接続 | `vpp`グループ | `/run/vpp/api.sock`への接続 |
| インターフェース作成 (AVF/RDMA) | VPP API | `avf_create`/`rdma_create_v2` |
| LCP作成 | VPP API + `CAP_NET_ADMIN` | `lcp_itf_pair_add_del` + netlink |
| IPアドレス設定 | VPP API | `sw_interface_add_del_address` |
| VPP FIB操作 | VPP API | `ip_route_add_del` |

**権限確認フロー**:

```go
// pkg/vpp/govpp_client.go
func (c *govppClient) Connect(ctx context.Context) error {
    // Check if /run/vpp/api.sock exists
    if _, err := os.Stat(c.socketPath); err != nil {
        return fmt.Errorf("VPP socket not found: %w (ensure VPP is running)", err)
    }

    // Check if socket is accessible (vpp group)
    if err := checkSocketAccess(c.socketPath); err != nil {
        return fmt.Errorf("VPP socket permission denied: %w "+
            "(ensure user is in vpp group)", err)
    }

    // Connect to VPP
    // ...
}
```

---

### 4.2 FRR操作

| 操作 | 必要な権限 | 実装方法 |
|------|-----------|---------|
| FRR設定ファイル書き込み | `frr`グループ | `/etc/frr/frr.conf`への直接書き込み（0660） |
| FRR設定適用 | `frrvty`グループ | `frr-reload.py` または `vtysh -f` |
| FRR状態確認（arca-cli用） | `frrvty`グループ | `vtysh -c 'show running-config'` |

**注**: arca-routerdは`frr.conf`を直接書き込み、`frr-reload.py`で適用する方式を採用。`frrvty`グループはarca-cli（vtysh経由）とFRR設定適用時に必要。

**権限確認フロー**:

```go
// pkg/frr/reloader.go
func (r *Reloader) ApplyConfig(cfg *Config) error {
    // Check if /etc/frr/frr.conf is writable
    if err := checkFileWritable("/etc/frr/frr.conf"); err != nil {
        return fmt.Errorf("FRR config not writable: %w "+
            "(ensure user is in frr group)", err)
    }

    // Atomic write (tmp → rename)
    // ...

    // Apply via frr-reload.py (requires frrvty group)
    cmd := exec.Command("/usr/lib/frr/frr-reload.py", "--reload", "/etc/frr/frr.conf")
    // ...
}
```

---

## 5. 実装チェックリスト

### Phase 2実装時の確認事項

- [x] systemd unit fileに`User=arca-router`を設定
- [x] `AmbientCapabilities=CAP_NET_ADMIN`を設定
- [x] RPM postinstallで`arca-router`ユーザー作成
- [x] RPM postinstallでグループ追加（`vpp`, `frr`, `frrvty`）
- [ ] VPP socket権限確認ロジック実装（`pkg/vpp/govpp_client.go`）
- [ ] FRR設定ファイル権限設定（`pkg/frr/reloader.go`）
- [ ] 設定ファイル権限設定（`pkg/config/writer.go`）
- [ ] 統合テストで権限エラーハンドリング検証

---

## 6. トラブルシューティング

### 6.1 VPP接続エラー

**症状**: `VPP socket permission denied`

**原因**:
- ユーザーが`vpp`グループに所属していない
- `/run/vpp/api.sock`の権限が正しくない

**解決方法**:

```bash
# Check user groups
id arca-router

# Add user to vpp group if missing
sudo usermod -aG vpp arca-router

# Check VPP socket permissions
ls -l /run/vpp/api.sock

# Update VPP startup.conf if needed
sudo vi /etc/vpp/startup.conf
# Add: unix { api-segment { gid vpp } }

# Restart VPP
sudo systemctl restart vpp
```

---

### 6.2 FRR設定適用エラー

**症状**: `FRR config not writable`

**原因**:
- ユーザーが`frr`グループに所属していない
- `/etc/frr/frr.conf`の権限が正しくない

**解決方法**:

```bash
# Add user to frr group
sudo usermod -aG frr arca-router
sudo usermod -aG frrvty arca-router

# Fix FRR config permissions (group write required for arca-routerd)
sudo chown root:frr /etc/frr/frr.conf
sudo chmod 0660 /etc/frr/frr.conf

# Restart arca-routerd
sudo systemctl restart arca-routerd
```

---

### 6.3 LCP操作エラー

**症状**: `operation not permitted` when creating LCP

**原因**:
- `CAP_NET_ADMIN` Capabilityが不足

**解決方法**:

```bash
# Check capabilities
sudo systemctl show arca-routerd | grep Capabilities

# Update systemd unit file
sudo vi /etc/systemd/system/arca-routerd.service
# Add: AmbientCapabilities=CAP_NET_ADMIN

# Reload systemd and restart
sudo systemctl daemon-reload
sudo systemctl restart arca-routerd
```

---

## 7. セキュリティ監査

### Phase 2完了時の監査項目

- [ ] 全ファイルの所有者・権限を確認
- [ ] systemd unit fileのCapabilities設定を確認
- [ ] VPP/FRR socket権限を確認
- [ ] arca-routerdがroot権限で実行されていないことを確認
- [ ] 不要なCapabilitiesが付与されていないことを確認
- [ ] SELinux/AppArmorポリシーの確認（Phase 3）

### 監査スクリプト例

```bash
#!/bin/bash
# scripts/security-audit.sh

echo "=== arca-router Security Audit ==="

# Check user
echo "User: $(systemctl show -p User arca-routerd | cut -d= -f2)"

# Check capabilities
echo "Capabilities: $(systemctl show -p AmbientCapabilities arca-routerd | cut -d= -f2)"

# Check file permissions
echo "Config: $(ls -l /etc/arca-router/arca-router.conf)"
echo "FRR: $(ls -l /etc/frr/frr.conf)"
echo "VPP socket: $(ls -l /run/vpp/api.sock)"

# Check groups
echo "Groups: $(id arca-router)"
```

---

## 5. Phase 3 セキュリティ機能

### 5.1 NETCONF認証

**認証方式**:

1. **Password認証** (Argon2id)
   - Argon2idによる強固なパスワードハッシュ
   - Time cost: 1, Memory: 64MB, Threads: 4, Salt: 16 bytes
   - ハッシュはSQLiteデータベースに保存

2. **SSH公開鍵認証**
   - OpenSSH形式の公開鍵をサポート
   - RSA, ECDSA, Ed25519対応
   - 公開鍵はSQLiteデータベースに保存
   - 鍵ファイル権限: `0600` (owner read/write only)

**NETCONF Server**:
- TCP port 830 (デフォルト)
- SSH subsystem経由でNETCONFプロトコル提供
- RFC 6241準拠

**実装場所**:
- `pkg/netconf/ssh_server.go` - SSH認証処理
- `pkg/netconf/user_db.go` - ユーザー管理
- `cmd/arca-netconfd/main.go` - NETCONFデーモン

---

### 5.2 RBAC (Role-Based Access Control)

**ロール定義**:

| Role | Permissions | Use Case |
|------|------------|----------|
| `admin` | All NETCONF operations including `kill-session` | System administrators |
| `operator` | Configuration management (edit, commit, lock, unlock) | Network operators, CI/CD |
| `read-only` | View-only access (get-config, get) | Monitoring, auditing |

**Permission Matrix**:

| Operation | read-only | operator | admin |
|-----------|-----------|----------|-------|
| get-config | ✅ | ✅ | ✅ |
| get | ✅ | ✅ | ✅ |
| lock | ❌ | ✅ | ✅ |
| unlock | ❌ | ✅ | ✅ |
| edit-config | ❌ | ✅ | ✅ |
| commit | ❌ | ✅ | ✅ |
| kill-session | ❌ | ❌ | ✅ |

**Access Denied Error** (RFC 6241準拠):
```xml
<rpc-error>
  <error-type>protocol</error-type>
  <error-tag>access-denied</error-tag>
  <error-app-tag>rbac-deny</error-app-tag>
  <error-message>read-only role cannot perform this operation</error-message>
</rpc-error>
```

**実装場所**:
- `pkg/netconf/rbac.go` - RBAC enforcement
- `pkg/netconf/server.go` - RPC request handling

**詳細**: [RBAC Guide](rbac-guide.md)

---

### 5.3 Rate Limiting

**制限種別**:

1. **Per-IP Rate Limiting**
   - デフォルト: 10 requests/second
   - 設定: `set security rate-limit per-ip <limit>`
   - 目的: DDoS攻撃緩和

2. **Per-User Rate Limiting**
   - デフォルト: 20 requests/second
   - 設定: `set security rate-limit per-user <limit>`
   - 目的: ユーザー別の帯域制御

**実装方式**:
- Token bucket algorithm
- In-memory state (per SSH session)
- 超過時は一時的にブロック (429 Too Many Requests相当)

**実装場所**:
- `pkg/netconf/rate_limiter.go`
- `pkg/netconf/ssh_server.go`

---

### 5.4 Audit Logging

**ログ対象イベント**:

1. **認証イベント**
   - 認証成功/失敗
   - ユーザー名、ソースIP、認証方式

2. **認可イベント**
   - RBAC拒否
   - 操作、ロール、ユーザー名

3. **設定変更イベント**
   - commit操作
   - 変更内容のdiff
   - ユーザー名、タイムスタンプ

**ログ保存先**:
- **In-process logger**: slog (構造化ログ)
- **Audit datastore**: SQLite (永続化) ※Phase 3で配線実装予定

**ログフォーマット**:
```
[AUDIT] event=auth_success user=alice ip=192.168.1.100 method=password
[AUDIT] event=auth_failure user=bob ip=192.168.1.101 method=password reason=invalid_credentials
[RBAC] Access denied: user=monitor role=read-only operation=commit
[CONFIG] Commit by alice: +set system host-name new-router
```

**実装場所**:
- `pkg/netconf/audit_logger.go`
- `pkg/netconf/ssh_server.go` (認証・認可イベント)
- `pkg/netconf/server.go` (設定変更イベント)

---

### 5.5 Secrets Management

**パスワード管理**:
- **ハッシュ**: Argon2id (OWASP推奨)
- **ソルト**: 16 bytes random
- **ストレージ**: SQLite (`users` table, `password_hash` column)
- **平文保存禁止**: パスワードは常にハッシュ化

**SSH鍵管理**:
- **鍵形式**: OpenSSH authorized_keys形式
- **ストレージ**: SQLite (`user_keys` table)
- **権限**: 鍵ファイル (存在する場合) は `0600`
- **検証**: SSH公開鍵署名検証

**環境変数経由の機密情報**:
```bash
# systemd unit fileで環境変数を設定
[Service]
Environment="NETCONF_ADMIN_PASSWORD_FILE=/run/secrets/admin-password"
```

**実装場所**:
- `pkg/netconf/user_db.go` - パスワード・鍵管理
- `pkg/netconf/ssh_server.go` - 認証処理

**セキュリティベストプラクティス**:
- 平文パスワードをログに出力しない
- 機密情報を環境変数や設定ファイルに直接書かない (可能な限り)
- 定期的なパスワードローテーション (ユーザー責任)

---

### 5.6 セキュリティ監査

**監査スクリプト例**:

```bash
#!/bin/bash
# scripts/netconf-security-audit.sh

echo "=== NETCONF Security Audit ==="

# Check NETCONF service status
echo "NETCONF Service: $(systemctl is-active arca-netconfd)"

# Check user database permissions
echo "User DB: $(ls -l /var/lib/arca-router/netconf.db)"

# List users and roles
sqlite3 /var/lib/arca-router/netconf.db "SELECT username, role FROM users;"

# Check audit logs
echo "Recent authentication events:"
journalctl -u arca-netconfd | grep "auth_success\|auth_failure" | tail -10

# Check RBAC denials
echo "Recent RBAC denials:"
journalctl -u arca-netconfd | grep "Access denied" | tail -10
```

---

### 5.7 セキュリティリスクと緩和策

| Risk | Impact | Mitigation |
|------|--------|------------|
| Brute-force password attack | Unauthorized access | Rate limiting (10 req/s per IP), Argon2id (slow hashing) |
| SSH key theft | Unauthorized access | Key file permissions (0600), regular key rotation |
| RBAC bypass | Privilege escalation | Server-side enforcement, comprehensive unit tests |
| Audit log tampering | Evidence destruction | Database permissions (root only), immutable logging |
| NETCONF DoS attack | Service unavailability | Rate limiting, connection limits, timeout |
| Privilege escalation via arca-netconfd | System compromise | Capabilities restriction, systemd sandboxing |

**追加対策 (Phase 4以降)**:
- Multi-factor authentication (MFA)
- Certificate-based authentication
- Audit log forwarding to external SIEM
- Intrusion detection system (IDS) integration

---

## 参考文献

- [systemd Capabilities](https://www.freedesktop.org/software/systemd/man/systemd.exec.html#Capabilities)
- [Linux Capabilities(7)](https://man7.org/linux/man-pages/man7/capabilities.7.html)
- [VPP API Security](https://s3-docs.fd.io/vpp/24.10/gettingstarted/developers/vapi.html)
- [FRR User Guide - Security](https://docs.frrouting.org/en/latest/setup.html#security)
- [RFC 6241 - NETCONF Protocol](https://datatracker.ietf.org/doc/html/rfc6241)
- [OWASP Password Storage Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html)
- [Argon2 Specification](https://github.com/P-H-C/phc-winner-argon2)
- [RBAC Guide](rbac-guide.md)
- [Policy Options Guide](policy-options-guide.md)
