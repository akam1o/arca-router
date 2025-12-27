#!/usr/bin/env python3
"""
NETCONF Integration Tests using ncclient

Tests basic NETCONF operations against arca-netconfd:
- Connection/Hello
- get-config (running/candidate)
- edit-config
- lock/unlock
- commit
- discard-changes

Requirements:
- ncclient >= 0.6.0
- arca-netconfd running on localhost:830
"""

import sys
import os
from ncclient import manager
from ncclient.xml_ import to_ele
import xml.etree.ElementTree as ET

# Configuration
HOST = os.getenv('NETCONF_HOST', 'localhost')
PORT = int(os.getenv('NETCONF_PORT', '830'))
USER = os.getenv('NETCONF_USER', 'admin')
PASSWORD = os.getenv('NETCONF_PASSWORD', 'admin')

# Test results
tests_passed = 0
tests_failed = 0

def print_test(name):
    """Print test header"""
    print(f"\n{'='*60}")
    print(f"TEST: {name}")
    print('='*60)

def assert_true(condition, message):
    """Assert condition and track results"""
    global tests_passed, tests_failed
    if condition:
        print(f"✓ PASS: {message}")
        tests_passed += 1
    else:
        print(f"✗ FAIL: {message}")
        tests_failed += 1

def assert_in(substring, text, message):
    """Assert substring in text"""
    assert_true(substring in text, message)

def test_connection():
    """Test 1: Basic connection and hello exchange"""
    print_test("Connection and Hello Exchange")

    try:
        with manager.connect(
            host=HOST,
            port=PORT,
            username=USER,
            password=PASSWORD,
            hostkey_verify=False,
            device_params={'name': 'default'}
        ) as m:
            print(f"Connected to {HOST}:{PORT}")
            print(f"Session ID: {m.session_id}")

            # Check capabilities
            assert_true(len(m.server_capabilities) > 0, "Server has capabilities")

            # Check for required capabilities
            caps = list(m.server_capabilities)
            has_base_1_0 = any('urn:ietf:params:netconf:base:1.0' in c for c in caps)
            has_base_1_1 = any('urn:ietf:params:netconf:base:1.1' in c for c in caps)
            has_candidate = any('urn:ietf:params:netconf:capability:candidate:1.0' in c for c in caps)

            assert_true(has_base_1_0, "Server supports base:1.0")
            assert_true(has_base_1_1, "Server supports base:1.1")
            assert_true(has_candidate, "Server supports candidate datastore")

            print(f"\nServer capabilities ({len(caps)} total):")
            for cap in caps:
                print(f"  - {cap}")

    except Exception as e:
        print(f"✗ Connection failed: {e}")
        assert_true(False, "Connection successful")
        return False

    return True

def test_get_config():
    """Test 2: Get running and candidate config"""
    print_test("Get Config (running/candidate)")

    try:
        with manager.connect(
            host=HOST, port=PORT, username=USER, password=PASSWORD,
            hostkey_verify=False, device_params={'name': 'default'}
        ) as m:
            # Get running config
            running = m.get_config(source='running')
            assert_true(running.ok, "get-config running successful")
            print(f"Running config length: {len(running.data_xml)} bytes")

            # Get candidate config
            candidate = m.get_config(source='candidate')
            assert_true(candidate.ok, "get-config candidate successful")
            print(f"Candidate config length: {len(candidate.data_xml)} bytes")

    except Exception as e:
        print(f"✗ Get config failed: {e}")
        assert_true(False, "Get config successful")
        return False

    return True

def test_lock_unlock():
    """Test 3: Lock and unlock candidate datastore"""
    print_test("Lock/Unlock Candidate")

    try:
        with manager.connect(
            host=HOST, port=PORT, username=USER, password=PASSWORD,
            hostkey_verify=False, device_params={'name': 'default'}
        ) as m:
            # Lock candidate
            lock_reply = m.lock(target='candidate')
            assert_true(lock_reply.ok, "Lock candidate successful")

            # Unlock candidate
            unlock_reply = m.unlock(target='candidate')
            assert_true(unlock_reply.ok, "Unlock candidate successful")

    except Exception as e:
        print(f"✗ Lock/unlock failed: {e}")
        assert_true(False, "Lock/unlock successful")
        return False

    return True

def test_edit_config():
    """Test 4: Edit candidate config"""
    print_test("Edit Config (merge operation)")

    config = """
    <config xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
        <system xmlns="urn:arca:router:config:1.0">
            <host-name>test-router</host-name>
        </system>
    </config>
    """

    try:
        with manager.connect(
            host=HOST, port=PORT, username=USER, password=PASSWORD,
            hostkey_verify=False, device_params={'name': 'default'}
        ) as m:
            # Lock candidate
            m.lock(target='candidate')

            # Edit config
            edit_reply = m.edit_config(target='candidate', config=config)
            assert_true(edit_reply.ok, "edit-config successful")

            # Verify change in candidate
            candidate = m.get_config(source='candidate')
            assert_in('test-router', candidate.data_xml, "Hostname updated in candidate")

            # Discard changes
            discard_reply = m.discard_changes()
            assert_true(discard_reply.ok, "discard-changes successful")

            # Unlock
            m.unlock(target='candidate')

    except Exception as e:
        print(f"✗ Edit config failed: {e}")
        assert_true(False, "Edit config successful")
        return False

    return True

def test_commit():
    """Test 5: Edit, commit, verify, and restore"""
    print_test("Edit, Commit, Verify, and Restore")

    config = """
    <config xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
        <system xmlns="urn:arca:router:config:1.0">
            <host-name>arca-test-commit</host-name>
        </system>
    </config>
    """

    restore_config = """
    <config xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
        <system xmlns="urn:arca:router:config:1.0">
            <host-name>arca-router</host-name>
        </system>
    </config>
    """

    try:
        with manager.connect(
            host=HOST, port=PORT, username=USER, password=PASSWORD,
            hostkey_verify=False, device_params={'name': 'default'}
        ) as m:
            # Lock candidate
            m.lock(target='candidate')

            # Edit config
            m.edit_config(target='candidate', config=config)
            print("Edited candidate config")

            # Commit
            commit_reply = m.commit()
            assert_true(commit_reply.ok, "commit successful")

            # Verify in running config
            running = m.get_config(source='running')
            assert_in('arca-test-commit', running.data_xml, "Hostname committed to running")

            # Restore original config (cleanup)
            m.edit_config(target='candidate', config=restore_config)
            m.commit()
            print("Restored original hostname")

            # Unlock
            m.unlock(target='candidate')

    except Exception as e:
        print(f"✗ Commit failed: {e}")
        assert_true(False, "Commit successful")
        return False

    return True

def test_close_session():
    """Test 6: Close session gracefully"""
    print_test("Close Session")

    try:
        with manager.connect(
            host=HOST, port=PORT, username=USER, password=PASSWORD,
            hostkey_verify=False, device_params={'name': 'default'}
        ) as m:
            # Close session
            close_reply = m.close_session()
            assert_true(close_reply.ok, "close-session successful")

    except Exception as e:
        print(f"✗ Close session failed: {e}")
        assert_true(False, "Close session successful")
        return False

    return True

def main():
    """Run all tests"""
    print("="*60)
    print("NETCONF Integration Tests")
    print(f"Target: {HOST}:{PORT}")
    print(f"User: {USER}")
    print("="*60)

    # Check if ncclient is available
    try:
        import ncclient
        print(f"ncclient version: {ncclient.__version__}")
    except ImportError:
        print("ERROR: ncclient not installed")
        print("Install: pip install ncclient")
        sys.exit(1)

    # Run tests (short-circuit on connection failure)
    if not test_connection():
        print("\n✗ Connection failed - aborting remaining tests")
        sys.exit(1)

    test_get_config()
    test_lock_unlock()
    test_edit_config()
    test_commit()
    test_close_session()

    # Summary
    print("\n" + "="*60)
    print("TEST SUMMARY")
    print("="*60)
    print(f"Passed: {tests_passed}")
    print(f"Failed: {tests_failed}")
    print(f"Total:  {tests_passed + tests_failed}")
    print("="*60)

    if tests_failed > 0:
        sys.exit(1)
    else:
        print("\n✓ All tests passed!")
        sys.exit(0)

if __name__ == '__main__':
    main()
