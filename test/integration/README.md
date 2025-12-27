# NETCONF Integration Tests

This directory contains integration tests for the arca-router NETCONF implementation.

## Requirements

### 1. Python and ncclient

```bash
# Install ncclient
pip install ncclient

# Or use virtual environment (recommended)
python3 -m venv venv
source venv/bin/activate
pip install ncclient
```

### 2. Running arca-netconfd

The NETCONF server (`arca-netconfd`) must be running before executing tests.

```bash
# Build arca-netconfd
cd /path/to/arca-router
go build -o build/bin/arca-netconfd ./cmd/arca-netconfd

# Run with default settings (port 830)
sudo ./build/bin/arca-netconfd --listen 127.0.0.1:830
```

**Note**: Port 830 requires root privileges. For testing without root:

```bash
# Run on high port
./build/bin/arca-netconfd --listen 127.0.0.1:8830

# Set environment variable for tests
export NETCONF_PORT=8830
```

### 3. User Database Setup

Create a test user (if user authentication is enabled):

```bash
# Add admin user to users.db
# TODO: Implement user management CLI
```

## Running Tests

### Basic Usage

```bash
# Run all tests
python3 test/integration/netconf_test.py
```

### Environment Variables

Configure connection parameters:

```bash
# Custom host/port
export NETCONF_HOST=192.168.1.100
export NETCONF_PORT=8830

# Custom credentials
export NETCONF_USER=testuser
export NETCONF_PASSWORD=testpass

# Run tests
python3 test/integration/netconf_test.py
```

## Test Cases

The test suite covers:

1. **Connection and Hello**: Capability negotiation, base:1.0/1.1 support
2. **Get Config**: Retrieve running and candidate configurations
3. **Lock/Unlock**: Datastore locking mechanism
4. **Edit Config**: Modify candidate configuration
5. **Commit**: Promote candidate to running
6. **Close Session**: Graceful session termination

## Expected Output

```
============================================================
NETCONF Integration Tests
Target: localhost:830
User: admin
============================================================
ncclient version: 0.6.15

============================================================
TEST: Connection and Hello Exchange
============================================================
Connected to localhost:830
Session ID: 1
✓ PASS: Server has capabilities
✓ PASS: Server supports base:1.0
✓ PASS: Server supports base:1.1
✓ PASS: Server supports candidate datastore

...

============================================================
TEST SUMMARY
============================================================
Passed: 15
Failed: 0
Total:  15
============================================================

✓ All tests passed!
```

## Troubleshooting

### Connection Refused

```
ERROR: Connection refused
```

**Solution**: Ensure `arca-netconfd` is running:
```bash
ps aux | grep arca-netconfd
sudo netstat -tlnp | grep 830
```

### Permission Denied (Port 830)

```
ERROR: Permission denied
```

**Solution**: Run `arca-netconfd` with sudo or use high port:
```bash
# Option 1: Use sudo
sudo ./build/bin/arca-netconfd

# Option 2: Use high port (recommended for testing)
./build/bin/arca-netconfd --listen 127.0.0.1:8830
export NETCONF_PORT=8830
```

### Authentication Failed

```
ERROR: Authentication failed
```

**Solution**:
- Phase 3: arca-netconfd uses NoClientAuth (no password required)
- Check SSH server configuration
- Verify user database setup

### ncclient Not Found

```
ERROR: ModuleNotFoundError: No module named 'ncclient'
```

**Solution**:
```bash
pip install ncclient
```

## Phase 3 Limitations

- **Authentication**: NoClientAuth mode (Phase 2-3 will add password auth)
- **YANG Validation**: Parse validation only (full validation in Phase 4)
- **XPath Filtering**: Basic subtree filtering (full XPath in Phase 4)

## Development

### Adding New Tests

```python
def test_my_feature():
    """Test X: Description"""
    print_test("My Feature Test")

    try:
        with manager.connect(...) as m:
            # Test code here
            result = m.some_operation()
            assert_true(result.ok, "Operation successful")

    except Exception as e:
        print(f"✗ Test failed: {e}")
        assert_true(False, "Test successful")
        return False

    return True

# Add to main():
test_my_feature()
```

### Running Specific Tests

Edit `main()` to comment out unwanted tests:

```python
def main():
    test_connection()
    # test_get_config()  # Skip this
    test_lock_unlock()
    # ...
```

## CI/CD Integration

**Phase 5**: GitHub Actions workflow will include:

```yaml
- name: Run NETCONF integration tests
  run: |
    ./build/bin/arca-netconfd --listen 127.0.0.1:8830 &
    sleep 2
    NETCONF_PORT=8830 python3 test/integration/netconf_test.py
```

## References

- [RFC 6241: NETCONF Protocol](https://tools.ietf.org/html/rfc6241)
- [RFC 6242: NETCONF over SSH](https://tools.ietf.org/html/rfc6242)
- [ncclient Documentation](https://ncclient.readthedocs.io/)
- [PHASE3.md](../../PHASE3.md) - Implementation plan
