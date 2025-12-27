# arca-router Ansible Examples

This directory contains Ansible playbook examples for configuring and managing arca-router via NETCONF.

## Directory Structure

```
examples/ansible/
├── README.md                      # This file
├── inventory/
│   ├── hosts.ini                  # INI format inventory
│   └── hosts.yml                  # YAML format inventory
├── test_connection.yml            # Test NETCONF connection
├── configure_interfaces.yml       # Configure network interfaces
└── configure_bgp.yml             # Configure BGP routing
```

## Prerequisites

### 1. Install Ansible

```bash
# macOS
brew install ansible

# Ubuntu/Debian
sudo apt install ansible python3-pip
pip3 install ansible

# RHEL/CentOS
sudo yum install ansible python3-pip
pip3 install ansible
```

### 2. Install NETCONF Collection

```bash
# Install ansible.netcommon collection
ansible-galaxy collection install ansible.netcommon

# Verify installation
ansible-galaxy collection list | grep netcommon
```

Expected output:
```
ansible.netcommon    5.0.0
```

### 3. Install ncclient (Python NETCONF library)

```bash
pip3 install ncclient
```

### 4. Start arca-netconfd

Ensure the NETCONF server is running on target routers:

```bash
# Start arca-netconfd
sudo ./build/bin/arca-netconfd --listen 127.0.0.1:830

# Or use systemd (if configured)
sudo systemctl start arca-netconfd

# Verify it's running
sudo netstat -tlnp | grep 830
```

## Quick Start

### 1. Configure Inventory

Edit `inventory/hosts.ini` or `inventory/hosts.yml` to match your environment:

**INI format** (`inventory/hosts.ini`):
```ini
[routers]
router1 ansible_host=192.168.1.10
router2 ansible_host=192.168.1.11

[routers:vars]
ansible_connection=ansible.netcommon.netconf
ansible_network_os=default
ansible_port=830
ansible_user=admin
ansible_password=admin
```

**YAML format** (`inventory/hosts.yml`):
```yaml
all:
  children:
    routers:
      hosts:
        router1:
          ansible_host: 192.168.1.10
      vars:
        ansible_connection: ansible.netcommon.netconf
        ansible_port: 830
        ansible_user: admin
        ansible_password: admin
```

### 2. Test Connection

Run the connection test playbook:

```bash
cd examples/ansible
ansible-playbook -i inventory/hosts.ini test_connection.yml
```

Expected output:
```
TASK [Display connection result] *********************************************
ok: [router1] => {
    "msg": "Connection: SUCCESS\nHost: 192.168.1.10:830\nUser: admin\n"
}

PLAY RECAP ******************************************************************
router1                    : ok=2    changed=0    unreachable=0    failed=0
```

### 3. Configure Interfaces

Run the interface configuration playbook:

```bash
ansible-playbook -i inventory/hosts.ini configure_interfaces.yml
```

This will:
- Lock the candidate datastore
- Configure ge-0/0/0 (10.0.1.1/24) and ge-0/0/1 (192.168.1.1/24)
- Commit the configuration
- Unlock the candidate datastore
- Verify the configuration in running datastore

### 4. Configure BGP

Run the BGP configuration playbook:

```bash
ansible-playbook -i inventory/hosts.ini configure_bgp.yml
```

This will:
- Configure AS number and router-id
- Configure IBGP and EBGP groups
- Configure BGP neighbors
- Commit the configuration
- Verify BGP configuration

## Playbook Details

### test_connection.yml

**Purpose**: Test NETCONF connectivity and verify arca-netconfd is accessible.

**Usage**:
```bash
ansible-playbook -i inventory/hosts.ini test_connection.yml
```

**What it does**:
- Connects to NETCONF server
- Retrieves system hostname via get-config
- Displays connection status and troubleshooting hints

### configure_interfaces.yml

**Purpose**: Configure network interfaces with IP addresses.

**Usage**:
```bash
ansible-playbook -i inventory/hosts.ini configure_interfaces.yml
```

**Customization**: Edit the `interfaces` variable in the playbook:
```yaml
vars:
  interfaces:
    - name: ge-0/0/0
      description: "Uplink to Core"
      unit_id: 0
      ip_address: 10.0.1.1
      prefix_length: 24
```

**What it does**:
1. Locks candidate datastore
2. Configures each interface with description and IP address
3. Commits changes to running config
4. Unlocks candidate datastore
5. Verifies configuration (rescue block handles errors)

### configure_bgp.yml

**Purpose**: Configure BGP routing with multiple groups and neighbors.

**Usage**:
```bash
ansible-playbook -i inventory/hosts.ini configure_bgp.yml
```

**Customization**: Edit BGP variables in the playbook:
```yaml
vars:
  local_as: 65001
  router_id: 10.0.1.1
  bgp_groups:
    - name: IBGP
      type: internal
      neighbors:
        - address: 10.0.1.2
          peer_as: 65001
          description: "Internal BGP Peer"
```

**What it does**:
1. Locks candidate datastore
2. Configures routing-options (AS number, router-id)
3. Configures BGP groups (IBGP, EBGP)
4. Configures BGP neighbors for each group
5. Commits changes
6. Unlocks candidate datastore
7. Verifies BGP configuration

## Common Tasks

### Dry Run (Check Mode)

Run playbook without making changes:

```bash
ansible-playbook -i inventory/hosts.ini configure_interfaces.yml --check
```

**Note**: NETCONF modules may not fully support check mode in all cases.

### Verbose Output

Show detailed NETCONF XML messages:

```bash
ansible-playbook -i inventory/hosts.ini test_connection.yml -vvv
```

### Target Specific Host

Run playbook on single host:

```bash
ansible-playbook -i inventory/hosts.ini configure_interfaces.yml --limit router1
```

### List Hosts

Show hosts in inventory:

```bash
ansible-inventory -i inventory/hosts.ini --list
```

## Troubleshooting

### Connection Refused

**Error**:
```
fatal: [router1]: FAILED! => {"msg": "Connection refused"}
```

**Solution**:
1. Verify arca-netconfd is running:
   ```bash
   sudo systemctl status arca-netconfd
   # or
   ps aux | grep arca-netconfd
   ```

2. Check port is listening:
   ```bash
   sudo netstat -tlnp | grep 830
   ```

3. Test SSH manually:
   ```bash
   ssh -p 830 admin@192.168.1.10 -s netconf
   ```

### Authentication Failed

**Error**:
```
fatal: [router1]: FAILED! => {"msg": "Authentication failed"}
```

**Solution**:
1. Verify credentials in inventory file
2. Test SSH manually with credentials:
   ```bash
   ssh -p 830 admin@192.168.1.10 -s netconf
   ```

### Lock Denied

**Error**:
```
<rpc-error>
  <error-tag>lock-denied</error-tag>
</rpc-error>
```

**Solution**: Another session has the lock. Release it:
```bash
# Run emergency unlock playbook
ansible-playbook -i inventory/hosts.ini emergency_unlock.yml
```

Or manually:
```bash
ssh -p 830 admin@192.168.1.10 -s netconf
# Send unlock RPC
```

### Module Not Found

**Error**:
```
ERROR! couldn't resolve module/action 'ansible.netcommon.netconf_config'
```

**Solution**: Install the netcommon collection:
```bash
ansible-galaxy collection install ansible.netcommon
```

## Best Practices

### 1. Use Version Control

Store playbooks and inventory in Git:
```bash
git init
git add inventory/ *.yml
git commit -m "Initial Ansible configuration"
```

### 2. Use Ansible Vault for Passwords

Encrypt sensitive credentials:
```bash
# Create encrypted file
ansible-vault create vars/secrets.yml

# Content:
vault_arca_password: admin

# Use in inventory
ansible_password: "{{ vault_arca_password }}"

# Run with vault
ansible-playbook -i inventory/hosts.yml playbook.yml --ask-vault-pass
```

### 3. Test Before Production

Always test playbooks on a lab router first:
```bash
ansible-playbook -i inventory/lab.ini configure_bgp.yml
```

### 4. Use Idempotency

Design playbooks to be idempotent (safe to run multiple times):
- Use `netconf_config` with merge operations
- Verify state before making changes
- Use `changed_when` to accurately report changes

### 5. Error Handling

Always use block/rescue/always for critical operations:
```yaml
- name: Configure with error handling
  block:
    - name: Lock candidate
      ansible.netcommon.netconf_rpc:
        rpc: lock
        content: |
          <target><candidate/></target>

    - name: Make changes
      ansible.netcommon.netconf_config:
        target: candidate
        content: "{{ config }}"

    - name: Commit
      ansible.netcommon.netconf_rpc:
        rpc: commit

  rescue:
    - name: Discard on error
      ansible.netcommon.netconf_rpc:
        rpc: discard-changes

  always:
    - name: Unlock
      ansible.netcommon.netconf_rpc:
        rpc: unlock
        content: |
          <target><candidate/></target>
      ignore_errors: true
```

## Advanced Usage

### Using Jinja2 Templates

Store XML templates separately:

**templates/interface.xml.j2**:
```xml
<config xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <interfaces xmlns="urn:ietf:params:xml:ns:yang:ietf-interfaces">
    <interface>
      <name>{{ interface_name }}</name>
      <description>{{ interface_description }}</description>
      <units xmlns="urn:arca:router:config:1.0">
        <unit>
          <unit-id>{{ unit_id }}</unit-id>
          <family>
            <inet>
              <address>
                <ip>{{ ip_address }}</ip>
                <prefix-length>{{ prefix_length }}</prefix-length>
              </address>
            </inet>
          </family>
        </unit>
      </units>
    </interface>
  </interfaces>
</config>
```

**Use in playbook**:
```yaml
- name: Configure interface from template
  ansible.netcommon.netconf_config:
    target: candidate
    content: "{{ lookup('template', 'templates/interface.xml.j2') }}"
```

### Dynamic Inventory

Use dynamic inventory for cloud environments:

**inventory.py** (executable):
```python
#!/usr/bin/env python3
import json

inventory = {
    "routers": {
        "hosts": ["router1", "router2"],
        "vars": {
            "ansible_connection": "ansible.netcommon.netconf",
            "ansible_port": 830
        }
    }
}

print(json.dumps(inventory))
```

Usage:
```bash
chmod +x inventory.py
ansible-playbook -i inventory.py playbook.yml
```

## Next Steps

1. **Create Ansible Roles**: Organize playbooks into reusable roles
2. **CI/CD Integration**: Automate playbook execution with Jenkins/GitLab CI
3. **Molecule Testing**: Test playbooks with Molecule framework
4. **AWX/Tower**: Deploy with Ansible AWX for web UI and scheduling

## References

- [Ansible NETCONF Guide](https://docs.ansible.com/ansible/latest/network/user_guide/platform_netconf_enabled.html)
- [ansible.netcommon.netconf_config](https://docs.ansible.com/ansible/latest/collections/ansible/netcommon/netconf_config_module.html)
- [ansible.netcommon.netconf_get](https://docs.ansible.com/ansible/latest/collections/ansible/netcommon/netconf_get_module.html)
- [ansible.netcommon.netconf_rpc](https://docs.ansible.com/ansible/latest/collections/ansible/netcommon/netconf_rpc_module.html)
- [arca-router Ansible Integration Guide](../../docs/ansible-integration.md)
- [RFC 6241: NETCONF Protocol](https://tools.ietf.org/html/rfc6241)
