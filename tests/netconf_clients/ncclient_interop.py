#!/usr/bin/env python3
import argparse
import sys

from lxml import etree
from ncclient import manager
from ncclient.operations.errors import TimeoutExpiredError
from ncclient.operations.rpc import RPCError, RaiseMode


NETCONF_NS = "urn:ietf:params:xml:ns:netconf:base:1.0"
CAP_BASE_10 = "urn:ietf:params:netconf:base:1.0"
CAP_BASE_11 = "urn:ietf:params:netconf:base:1.1"
CAP_CANDIDATE = "urn:ietf:params:netconf:capability:candidate:1.0"
CAP_VALIDATE = "urn:ietf:params:netconf:capability:validate:1.1"
CAP_STARTUP = "urn:ietf:params:netconf:capability:startup:1.0"
CAP_WRITABLE_RUNNING = "urn:ietf:params:netconf:capability:writable-running:1.0"
CAP_CONFIRMED_COMMIT = "urn:ietf:params:netconf:capability:confirmed-commit:1.1"


def parse_args():
    parser = argparse.ArgumentParser(description="ncclient NETCONF interoperability test")
    parser.add_argument("--host", required=True)
    parser.add_argument("--port", required=True, type=int)
    parser.add_argument("--username", required=True)
    parser.add_argument("--password", required=True)
    return parser.parse_args()


def fail(message):
    print(f"ncclient interop failed: {message}", file=sys.stderr)
    sys.exit(1)


def assert_capabilities(caps):
    required = {
        CAP_BASE_10,
        CAP_BASE_11,
        CAP_CANDIDATE,
        CAP_VALIDATE,
    }
    missing = sorted(required - caps)
    if missing:
        fail(f"missing server capabilities: {missing}")

    forbidden = {
        CAP_STARTUP,
        CAP_WRITABLE_RUNNING,
        CAP_CONFIRMED_COMMIT,
    }
    advertised = sorted(forbidden & caps)
    if advertised:
        fail(f"unsupported capabilities were advertised: {advertised}")


def assert_rpc_error(operation, want_tag):
    try:
        operation()
    except RPCError as err:
        if err.tag != want_tag:
            fail(f"rpc-error tag {err.tag!r}, want {want_tag!r}: {err}")
        return
    except TimeoutExpiredError as err:
        fail(f"RPC timed out instead of returning rpc-error: {err}")
    fail("operation unexpectedly succeeded")


def dispatch_xml(session, operation_xml):
    element = etree.fromstring(operation_xml.encode("utf-8"))
    return session.dispatch(element)


def main():
    args = parse_args()
    locked = False

    with manager.connect_ssh(
        host=args.host,
        port=args.port,
        username=args.username,
        password=args.password,
        hostkey_verify=False,
        look_for_keys=False,
        allow_agent=False,
        device_params={"name": "default"},
        manager_params={"timeout": 10},
        errors_params={"raise_mode": RaiseMode.ALL},
    ) as session:
        caps = {str(cap) for cap in session.server_capabilities}
        assert_capabilities(caps)

        running = session.get_config(source="running").data_xml
        if "arca-ci" not in running:
            fail("initial running configuration did not include arca-ci hostname")

        assert_rpc_error(
            lambda: dispatch_xml(
                session,
                f"""
                <get-config xmlns="{NETCONF_NS}">
                  <source><startup/></source>
                </get-config>
                """,
            ),
            "operation-not-supported",
        )

        session.lock(target="candidate")
        locked = True
        try:
            config = """
            <config>
              <system>
                <host-name>ncclient-ci</host-name>
              </system>
            </config>
            """
            session.edit_config(
                target="candidate",
                config=config,
                default_operation="replace",
                test_option="test-then-set",
                error_option="rollback-on-error",
            )
            session.validate(source="candidate")
            session.copy_config(source="candidate", target="candidate")
            session.commit()
            locked = False

            updated = session.get_config(source="running").data_xml
            if "ncclient-ci" not in updated:
                fail("committed hostname was not visible in running configuration")

            assert_rpc_error(
                lambda: dispatch_xml(
                    session,
                    f"""
                    <copy-config xmlns="{NETCONF_NS}">
                      <target><startup/></target>
                      <source><running/></source>
                    </copy-config>
                    """,
                ),
                "operation-not-supported",
            )
        finally:
            if locked:
                session.unlock(target="candidate")


if __name__ == "__main__":
    main()
