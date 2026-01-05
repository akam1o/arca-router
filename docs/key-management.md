# SSH Host Key Management Guide

## Overview

arca-router uses ED25519 SSH host keys for NETCONF server authentication. This document describes key management best practices, rotation procedures, and security considerations.

## Key Location and Permissions

### Default Key Path

```
/var/lib/arca-router/ssh_host_ed25519_key
```

### Required Permissions

**Key File:**
- Permissions: `0600` (owner read/write only)
- Owner: `arca-router` user
- Group: `arca-router` group

**Key Directory:**
- Permissions: `0750` (owner rwx, group rx, no world access)
- Owner: `arca-router` user
- Group: `arca-router` group

### Verification

Check current permissions:
```bash
ls -l /var/lib/arca-router/ssh_host_ed25519_key
```

Expected output:
```
-rw------- 1 arca-router arca-router 464 Dec 28 12:00 /var/lib/arca-router/ssh_host_ed25519_key
```

### Fixing Permissions

If permissions are incorrect:
```bash
# Fix key file permissions
sudo chmod 0600 /var/lib/arca-router/ssh_host_ed25519_key
sudo chown arca-router:arca-router /var/lib/arca-router/ssh_host_ed25519_key

# Fix directory permissions
sudo chmod 0750 /var/lib/arca-router
sudo chown arca-router:arca-router /var/lib/arca-router
```

## Automatic Key Generation

arca-router automatically generates an ED25519 host key on first startup if one doesn't exist:

1. Server starts and checks for key at configured path
2. If key doesn't exist, generates new ED25519 key pair
3. Saves private key to disk with 0600 permissions
4. Logs: "Generated and saved new host key"

This ensures zero-configuration deployment while maintaining security.

## Key Rotation

### When to Rotate Keys

Rotate SSH host keys in these situations:

1. **Security compromise**: Key may have been exposed or stolen
2. **Compliance requirements**: Regular rotation policy (e.g., annually)
3. **Personnel changes**: Admin with key access leaves organization
4. **Cryptographic concerns**: Algorithm weaknesses discovered (unlikely for ED25519)

### Rotation Procedure

#### Option 1: Simple Rotation (Downtime Required)

This method requires stopping the server:

```bash
# 1. Stop the NETCONF server
sudo systemctl stop arca-netconfd

# 2. Backup the old key
sudo cp /var/lib/arca-router/ssh_host_ed25519_key \
       /var/lib/arca-router/ssh_host_ed25519_key.old

# 3. Remove the old key
sudo rm /var/lib/arca-router/ssh_host_ed25519_key

# 4. Start the server (will auto-generate new key)
sudo systemctl start arca-netconfd

# 5. Verify new key was generated
sudo ls -l /var/lib/arca-router/ssh_host_ed25519_key
sudo journalctl -u arca-netconfd | grep "host key"
```

#### Option 2: Pre-Generated Key (No Downtime)

Generate the new key in advance and swap it:

```bash
# 1. Generate new ED25519 key offline
ssh-keygen -t ed25519 -f /tmp/new_host_key -N "" -C "arca-router-$(date +%Y%m%d)"

# 2. Convert to OpenSSH format (if needed)
# ssh-keygen already generates in correct format

# 3. Stop the server
sudo systemctl stop arca-netconfd

# 4. Backup old key
sudo cp /var/lib/arca-router/ssh_host_ed25519_key \
       /var/lib/arca-router/ssh_host_ed25519_key.old

# 5. Install new key
sudo mv /tmp/new_host_key /var/lib/arca-router/ssh_host_ed25519_key
sudo chmod 0600 /var/lib/arca-router/ssh_host_ed25519_key
sudo chown arca-router:arca-router /var/lib/arca-router/ssh_host_ed25519_key

# 6. Start the server
sudo systemctl start arca-netconfd

# 7. Clean up
sudo rm /tmp/new_host_key.pub
```

#### Option 3: Graceful Rotation (Multiple Keys)

**Note**: This requires code changes to support multiple host keys. Currently, arca-router loads a single host key.

For a future implementation:
1. Add new key alongside old key
2. Server loads both keys
3. Clients connect using either key (transition period)
4. After all clients updated, remove old key

### Post-Rotation Tasks

After rotating keys, clients will see a host key mismatch warning:

```
@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@
@    WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!     @
@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@
IT IS POSSIBLE THAT SOMEONE IS DOING SOMETHING NASTY!
```

This is expected behavior. Clients must update their known_hosts:

```bash
# Remove old key fingerprint
ssh-keygen -R <server-hostname>

# Or manually edit ~/.ssh/known_hosts and remove the line for this host

# Connect again to accept new key
ssh user@<server-hostname> -p 830
```

For automated clients (Ansible, scripts):
1. Update known_hosts on control nodes
2. Or use `StrictHostKeyChecking=no` temporarily (not recommended for production)
3. Or use `known_hosts` with wildcards and manage centrally

## Security Considerations

### Key Algorithm Choice

arca-router uses **ED25519** for host keys:

**Advantages:**
- Modern, secure elliptic curve algorithm
- Faster than RSA
- Smaller key size (256-bit vs 2048/4096-bit RSA)
- Resistance to timing attacks
- Recommended by security best practices

**Why not RSA?**
- RSA requires 2048-bit minimum (4096-bit recommended)
- Slower performance
- Larger key files
- More vulnerable to future quantum attacks

### Key Storage Security

**Protect the private key:**
1. Never commit to version control
2. Never share via insecure channels (email, chat)
3. Never copy to untrusted systems
4. Encrypt backups if stored off-server

**Add to .gitignore:**
```
# SSH host keys (private keys)
ssh_host_*_key
*.pem
*.key
!*.pub
```

### Secure Key Deletion

When permanently decommissioning keys, use secure deletion:

```bash
# Overwrite with zeros before deletion
sudo shred -vfz -n 3 /var/lib/arca-router/ssh_host_ed25519_key.old

# Or use arca-router's built-in secure delete function (if implemented)
```

arca-router's `SecurelyRemoveFile()` function:
- Overwrites file with zeros
- Syncs to disk
- Then removes file

This prevents key recovery from disk blocks.

### Monitoring

Monitor for suspicious key-related events:

```bash
# Check for permission changes
sudo journalctl -u arca-netconfd | grep "insecure permissions"

# Check for key access
sudo ausearch -f /var/lib/arca-router/ssh_host_ed25519_key

# Check for unauthorized key modifications
sudo aide --check  # If using AIDE for file integrity monitoring
```

## Environment Variables for Secrets

arca-router supports environment-based configuration for sensitive values:

### Direct Value

```bash
export DB_PASSWORD="my-secret-password"
```

### File-Based (Docker Secrets Compatible)

```bash
echo "my-secret-password" > /run/secrets/db_password
export DB_PASSWORD_FILE="/run/secrets/db_password"
```

The application will:
1. Check `DB_PASSWORD` environment variable first
2. If not set, check `DB_PASSWORD_FILE`
3. Read secret from file path
4. Trim trailing newlines (Docker secrets convention)

This pattern is compatible with Docker Swarm secrets and Kubernetes secrets.

## Compliance and Audit

### Audit Trail

Key operations should be logged:

```bash
# Key generation
[INFO] Generated and saved new host key path=/var/lib/arca-router/ssh_host_ed25519_key

# Key loaded at startup
[INFO] Loaded existing host key path=/var/lib/arca-router/ssh_host_ed25519_key

# Permission warnings
[WARN] Host key has insecure permissions path=/var/lib/arca-router/ssh_host_ed25519_key error=...
```

### Compliance Requirements

Common compliance frameworks and key management:

**PCI DSS:**
- Rotate keys annually
- Protect keys with 0600 permissions
- Log key access

**SOC 2:**
- Document key rotation procedures
- Audit key access
- Encrypt keys at rest (filesystem encryption)

**NIST 800-53:**
- SC-12: Cryptographic Key Establishment and Management
- SC-13: Cryptographic Protection
- SC-17: Public Key Infrastructure Certificates

## Troubleshooting

### Issue: Server Won't Start (Key Permission Error)

**Symptom:**
```
[WARN] Host key has insecure permissions
```

**Solution:**
```bash
sudo chmod 0600 /var/lib/arca-router/ssh_host_ed25519_key
sudo systemctl restart arca-netconfd
```

### Issue: Clients Can't Connect After Key Rotation

**Symptom:**
```
Host key verification failed
```

**Solution:**
Clients must update their known_hosts:
```bash
ssh-keygen -R <server-ip-or-hostname>
```

### Issue: Key File Deleted/Corrupted

**Symptom:**
```
[ERROR] failed to load host key: failed to parse host key
```

**Solution:**
Remove corrupted key and restart (will auto-generate):
```bash
sudo rm /var/lib/arca-router/ssh_host_ed25519_key
sudo systemctl restart arca-netconfd
```

### Issue: Wrong Ownership

**Symptom:**
```
[ERROR] failed to load host key: permission denied
```

**Solution:**
```bash
sudo chown arca-router:arca-router /var/lib/arca-router/ssh_host_ed25519_key
sudo systemctl restart arca-netconfd
```

## Best Practices Summary

1. ✅ Use default ED25519 algorithm (already implemented)
2. ✅ Verify key permissions are 0600
3. ✅ Rotate keys annually or after security events
4. ✅ Backup keys securely before rotation
5. ✅ Update client known_hosts after rotation
6. ✅ Monitor for permission changes
7. ✅ Use environment variables for secrets
8. ✅ Never commit keys to version control
9. ✅ Securely delete old keys after rotation
10. ✅ Document key rotation in change management

## References

- **RFC 8709**: Ed25519 and Ed448 Public Key Algorithms for SSH
- **OpenSSH Manual**: ssh-keygen(1), sshd_config(5)
- **NIST SP 800-57**: Recommendations for Key Management
- **arca-router Security Model**: `/docs/security-model.md`

---

## Quick Reference

### Common Commands

```bash
# Check key permissions
ls -l /var/lib/arca-router/ssh_host_ed25519_key

# Fix permissions
sudo chmod 0600 /var/lib/arca-router/ssh_host_ed25519_key

# Rotate key (with downtime)
sudo systemctl stop arca-netconfd
sudo rm /var/lib/arca-router/ssh_host_ed25519_key
sudo systemctl start arca-netconfd

# View host key fingerprint
ssh-keygen -l -f /var/lib/arca-router/ssh_host_ed25519_key

# Clear client known_hosts entry
ssh-keygen -R <hostname>
```

### Key Facts

- **Algorithm**: ED25519
- **Key Size**: 256-bit
- **File Format**: OpenSSH private key format
- **Required Permissions**: 0600
- **Auto-Generation**: Yes
- **Default Path**: `/var/lib/arca-router/ssh_host_ed25519_key`
