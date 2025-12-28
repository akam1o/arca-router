# Ansible Integration Guide

This document describes how to configure and manage arca-router using Ansible with NETCONF.

## Overview

arca-router supports NETCONF over SSH (RFC 6241/6242), enabling Infrastructure as Code (IaC) workflows with Ansible. This allows you to:

- Automate router configuration deployment
- Maintain configuration version control
- Apply consistent configurations across multiple routers
- Implement configuration rollback strategies

## Compatibility Notes (arca-netconfd)

- NETCONF base:1.1 (chunked framing `#<len>\n` ... `##\n`)
- Candidate datastore only: `target: running` returns `operation-not-supported`
- Commit required: edits are staged in candidate until a commit is issued
- Lock strongly recommended: use `lock: always` or explicit lock/unlock to avoid contention
- SSH auth: password + public key supported; mTLS is deferred to later phases

## Prerequisites

### 1. Ansible Installation

```bash
# Install Ansible with NETCONF support
pip install ansible ansible-pylibssh

# Verify installation
ansible --version
```

### 2. Ansible NETCONF Collection

```bash
# Install ansible.netcommon collection (provides netconf_config, netconf_rpc)
ansible-galaxy collection install ansible.netcommon

# Verify installation
ansible-galaxy collection list | grep netcommon
```

### 3. Python NETCONF Library (ncclient)

```bash
# Install ncclient (required by ansible.netcommon for NETCONF transport)
pip install ncclient

# Verify installation
python3 -c "import ncclient; print(f'ncclient {ncclient.__version__}')"
```

### 4. arca-netconfd Server

Ensure `arca-netconfd` is running on target routers:

```bash
# Start arca-netconfd
sudo systemctl start arca-netconfd

# Verify NETCONF is listening
sudo netstat -tlnp | grep 830
```

### 5. SSH Access

Configure SSH access with password authentication:

```bash
# Test SSH connection
ssh -p 830 -s admin@router1.example.com netconf

# Expected: NETCONF hello message
```

## Quick Start with Repository Examples

Ready-to-run playbooks live in `examples/ansible/`:

```bash
cd examples/ansible

# Connection check (get hostname)
ansible-playbook -i inventory/hosts.ini test_connection.yml

# Configure interfaces / BGP
ansible-playbook -i inventory/hosts.ini configure_interfaces.yml
ansible-playbook -i inventory/hosts.ini configure_bgp.yml
```

These playbooks already follow the required NETCONF contract (candidate-only, explicit commit, lock management). Copy and adjust them for your environment instead of starting from scratch.

## Inventory Configuration

### Basic Inventory (INI format)

Create `inventory/hosts.ini`:

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
# For SSH key authentication:
# ansible_password=                      # omit when using keys
# ansible_ssh_private_key_file=~/.ssh/arca_router_key
```

### YAML Inventory

Create `inventory/hosts.yml`:

```yaml
all:
  children:
    routers:
      hosts:
        router1:
          ansible_host: 192.168.1.10
        router2:
          ansible_host: 192.168.1.11
      vars:
        ansible_connection: ansible.netcommon.netconf
        ansible_network_os: default
        ansible_port: 830
        ansible_user: admin
        ansible_password: admin
        # Security: Use vault for passwords in production
        # ansible_password: "{{ vault_arca_password }}"
```

## Connection Testing

Create `test_connection.yml`:

```yaml
---
- name: Test NETCONF Connection
  hosts: routers
  gather_facts: false

  tasks:
    - name: Get capabilities
      ansible.netcommon.netconf_rpc:
        rpc: get
        content: |
          <filter>
            <system xmlns="urn:arca:router:config:1.0">
              <host-name/>
            </system>
          </filter>
      register: result

    - name: Display result
      ansible.builtin.debug:
        var: result
```

Run:
```bash
ansible-playbook -i inventory/hosts.ini test_connection.yml
```

## Configuration Management

### Interface Configuration

Create `configure_interfaces.yml`:

```yaml
---
- name: Configure Network Interfaces
  hosts: routers
  gather_facts: false

  tasks:
    - name: Lock candidate datastore
      ansible.netcommon.netconf_rpc:
        rpc: lock
        content: |
          <target>
            <candidate/>
          </target>

    - name: Configure interface ge-0/0/0
      ansible.netcommon.netconf_config:
        target: candidate
        content: |
          <config xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
            <interfaces xmlns="urn:ietf:params:xml:ns:yang:ietf-interfaces">
              <interface>
                <name>ge-0/0/0</name>
                <description>Uplink to Core</description>
                <units xmlns="urn:arca:router:config:1.0">
                  <unit>
                    <unit-id>0</unit-id>
                    <family>
                      <inet>
                        <address>
                          <ip>10.0.1.1</ip>
                          <prefix-length>24</prefix-length>
                        </address>
                      </inet>
                    </family>
                  </unit>
                </units>
              </interface>
            </interfaces>
          </config>

    - name: Commit configuration
      ansible.netcommon.netconf_rpc:
        rpc: commit

    - name: Unlock candidate datastore
      ansible.netcommon.netconf_rpc:
        rpc: unlock
        content: |
          <target>
            <candidate/>
          </target>
```

**Tip**: You can also inline locking/committing per task to reduce separate RPCs:

```yaml
- name: Configure interface ge-0/0/0
  ansible.netcommon.netconf_config:
    target: candidate      # required: writable-running unsupported
    lock: always           # recommended to avoid lock races
    commit: true           # required so candidate changes are applied
    content: "<config>...</config>"
```

### BGP Configuration

Create `configure_bgp.yml`:

```yaml
---
- name: Configure BGP
  hosts: routers
  gather_facts: false

  vars:
    local_as: 65001
    router_id: "{{ ansible_host }}"

  tasks:
    - name: Lock candidate datastore
      ansible.netcommon.netconf_rpc:
        rpc: lock
        content: |
          <target>
            <candidate/>
          </target>

    - name: Configure routing options
      ansible.netcommon.netconf_config:
        target: candidate
        content: |
          <config xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
            <routing-options xmlns="urn:arca:router:config:1.0">
              <autonomous-system>{{ local_as }}</autonomous-system>
              <router-id>{{ router_id }}</router-id>
            </routing-options>
          </config>

    - name: Configure BGP group
      ansible.netcommon.netconf_config:
        target: candidate
        content: |
          <config xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
            <protocols xmlns="urn:arca:router:config:1.0">
              <bgp>
                <group>
                  <name>IBGP</name>
                  <type>internal</type>
                  <neighbor>
                    <address>10.0.1.2</address>
                    <peer-as>{{ local_as }}</peer-as>
                    <description>Internal BGP Peer</description>
                    <local-address>{{ router_id }}</local-address>
                  </neighbor>
                </group>
              </bgp>
            </protocols>
          </config>

    - name: Commit configuration
      ansible.netcommon.netconf_rpc:
        rpc: commit

    - name: Unlock candidate datastore
      ansible.netcommon.netconf_rpc:
        rpc: unlock
        content: |
          <target>
            <candidate/>
          </target>
```

## Best Practices

### 1. Use Candidate Datastore Workflow

Always use lock → edit-config → commit → unlock pattern:

```yaml
tasks:
  - name: Lock candidate
    ansible.netcommon.netconf_rpc:
      rpc: lock
      content: |
        <target><candidate/></target>

  - name: Make changes
    ansible.netcommon.netconf_config:
      target: candidate
      content: "{{ config_xml }}"

  - name: Commit
    ansible.netcommon.netconf_rpc:
      rpc: commit

  - name: Unlock
    ansible.netcommon.netconf_rpc:
      rpc: unlock
      content: |
        <target><candidate/></target>
```

### 2. Error Handling with rescue

```yaml
- name: Configure with error handling
  block:
    - name: Lock candidate
      ansible.netcommon.netconf_rpc:
        rpc: lock
        content: |
          <target><candidate/></target>

    - name: Apply configuration
      ansible.netcommon.netconf_config:
        target: candidate
        content: "{{ config_xml }}"

    - name: Commit
      ansible.netcommon.netconf_rpc:
        rpc: commit

  rescue:
    - name: Discard changes on error
      ansible.netcommon.netconf_rpc:
        rpc: discard-changes

    - name: Fail task
      ansible.builtin.fail:
        msg: "Configuration failed, changes discarded"

  always:
    - name: Unlock candidate
      ansible.netcommon.netconf_rpc:
        rpc: unlock
        content: |
          <target><candidate/></target>
      ignore_errors: true
```

### 3. Use Jinja2 Templates

Create `templates/interface.xml.j2`:

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

Use in playbook:

```yaml
- name: Configure interface from template
  ansible.netcommon.netconf_config:
    target: candidate
    content: "{{ lookup('template', 'templates/interface.xml.j2') }}"
  vars:
    interface_name: ge-0/0/0
    interface_description: "Uplink to Core"
    unit_id: 0
    ip_address: 10.0.1.1
    prefix_length: 24
```

### 4. Use Ansible Vault for Credentials

```bash
# Create encrypted password file
ansible-vault create vars/secrets.yml

# Content:
# ---
# vault_arca_password: admin

# Use in playbook
ansible-playbook -i inventory/hosts.ini \
  --extra-vars "@vars/secrets.yml" \
  --ask-vault-pass \
  configure_bgp.yml
```

### 5. Verify Configuration

```yaml
- name: Verify configuration
  ansible.netcommon.netconf_rpc:
    rpc: get-config
    content: |
      <source>
        <running/>
      </source>
      <filter>
        <interfaces xmlns="urn:ietf:params:xml:ns:yang:ietf-interfaces">
          <interface>
            <name>ge-0/0/0</name>
          </interface>
        </interfaces>
      </filter>
  register: running_config

- name: Assert expected configuration
  ansible.builtin.assert:
    that:
      - "'10.0.1.1' in running_config.stdout"
    fail_msg: "Interface configuration not found in running config"
```

## Role Structure

Organize playbooks into roles for reusability:

```
roles/
├── arca_router_base/
│   ├── tasks/
│   │   └── main.yml
│   ├── templates/
│   │   └── system.xml.j2
│   └── vars/
│       └── main.yml
├── arca_router_interfaces/
│   ├── tasks/
│   │   └── main.yml
│   └── templates/
│       └── interface.xml.j2
└── arca_router_bgp/
    ├── tasks/
    │   └── main.yml
    └── templates/
        └── bgp.xml.j2
```

Use roles in playbook:

```yaml
---
- name: Configure arca-router
  hosts: routers
  gather_facts: false

  roles:
    - arca_router_base
    - arca_router_interfaces
    - arca_router_bgp
```

## Troubleshooting

### Connection Issues

**Problem**: `Connection refused`

**Solution**:
```bash
# Check arca-netconfd is running
sudo systemctl status arca-netconfd

# Check port is listening
sudo netstat -tlnp | grep 830
```

**Problem**: `Authentication failed`

**Solution**:
```bash
# Verify credentials in inventory
ansible-inventory -i inventory/hosts.ini --list

# Test SSH manually
ssh -p 830 admin@router1.example.com -s netconf
```

### XML Parsing Errors

**Problem**: `XML syntax error in config`

**Solution**: Validate XML before applying:
```bash
# Use xmllint to validate
echo '<config>...</config>' | xmllint --noout -
```

### NETCONF RPC Errors

**Problem**: `rpc-error: lock-denied`

**Solution**: Release existing lock:
```yaml
- name: Force unlock (emergency only)
  ansible.netcommon.netconf_rpc:
    rpc: unlock
    content: |
      <target><candidate/></target>
```

## Performance Considerations

### 1. Parallel Execution

Use `serial` or `forks` for controlled parallelism:

```yaml
---
- name: Configure routers in batches
  hosts: routers
  gather_facts: false
  serial: 5  # Configure 5 routers at a time

  tasks:
    - name: Apply configuration
      ansible.netcommon.netconf_config:
        target: candidate
        content: "{{ config_xml }}"
```

### 2. Minimize NETCONF Sessions

Batch multiple configurations in a single commit:

```yaml
- name: Configure multiple settings
  ansible.netcommon.netconf_config:
    target: candidate
    content: |
      <config xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
        <system xmlns="urn:arca:router:config:1.0">
          <host-name>router1</host-name>
        </system>
        <interfaces xmlns="urn:ietf:params:xml:ns:yang:ietf-interfaces">
          <!-- Multiple interfaces here -->
        </interfaces>
        <protocols xmlns="urn:arca:router:config:1.0">
          <!-- BGP/OSPF config here -->
        </protocols>
      </config>
```

## Security Recommendations

1. **Use SSH Keys**: Prefer SSH keys (supported now) and disable passwords where possible
2. **Ansible Vault**: Store credentials in encrypted vault files
3. **RBAC**: Use read-only accounts for get-config operations
4. **Audit Logs**: Enable audit logging on arca-netconfd
5. **Network Isolation**: Use management VLANs for NETCONF access

## Phase 3 Limitations

- **No mTLS**: Certificate-based authentication is deferred to a later phase
- **Basic Error Handling**: No automatic recovery; handle failures with Ansible `rescue` + `discard-changes`
- **Manual Rollback**: No automatic rollback on failure; rollback requires explicit NETCONF `discard-changes` or CLI/commit-history rollback

## Next Steps

1. Create Ansible roles for common configurations
2. Set up CI/CD pipeline for configuration testing
3. Implement configuration drift detection
4. Add integration tests with Molecule

## Common Ansible Errors

1. `operation-not-supported` when `target: running` is used (candidate-only server)
2. Config change silently missing because `commit` was omitted (candidate not applied)
3. Lock contention in multi-user runs when `lock`/`unlock` is skipped or not `lock: always`
4. `authentication failed` when credentials/keys do not match what is stored in the arca user DB (passwords and SSH public keys are both accepted)

## References

- [Ansible netconf_config Module](https://docs.ansible.com/ansible/latest/collections/ansible/netcommon/netconf_config_module.html)
- [Ansible netconf_get Module](https://docs.ansible.com/ansible/latest/collections/ansible/netcommon/netconf_get_module.html)
- [RFC 6241: NETCONF Protocol](https://tools.ietf.org/html/rfc6241)
- [arca-router NETCONF Implementation Plan](netconf-implementation-plan.md)
