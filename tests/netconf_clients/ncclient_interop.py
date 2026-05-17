#!/usr/bin/env python3
import argparse
import sys

from lxml import etree
from ncclient import manager
from ncclient.operations.errors import TimeoutExpiredError
from ncclient.operations.rpc import RPCError, RaiseMode


NETCONF_NS = "urn:ietf:params:xml:ns:netconf:base:1.0"
IETF_INTERFACES_NS = "urn:ietf:params:xml:ns:yang:ietf-interfaces"
CAP_BASE_10 = "urn:ietf:params:netconf:base:1.0"
CAP_BASE_11 = "urn:ietf:params:netconf:base:1.1"
CAP_CANDIDATE = "urn:ietf:params:netconf:capability:candidate:1.0"
CAP_VALIDATE = "urn:ietf:params:netconf:capability:validate:1.1"
CAP_ROLLBACK = "urn:ietf:params:netconf:capability:rollback-on-error:1.0"
CAP_XPATH = "urn:ietf:params:netconf:capability:xpath:1.0"
CAP_STARTUP = "urn:ietf:params:netconf:capability:startup:1.0"
CAP_WRITABLE_RUNNING = "urn:ietf:params:netconf:capability:writable-running:1.0"
CAP_CONFIRMED_COMMIT = "urn:ietf:params:netconf:capability:confirmed-commit:1.1"
CAP_ARCA_ROUTER = "urn:arca:router:config:1.0?module=arca-router&revision=2025-12-27"
CAP_ARCA_XPATH_FILTER_SUBSET = "urn:arca:router:netconf:capability:xpath-filter-subset:1.0"


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
        CAP_ROLLBACK,
        CAP_ARCA_ROUTER,
        CAP_ARCA_XPATH_FILTER_SUBSET,
    }
    missing = sorted(required - caps)
    if missing:
        fail(f"missing server capabilities: {missing}")

    forbidden = {
        CAP_XPATH,
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

        xpath_reply = dispatch_xml(
            session,
            f"""
            <get-config xmlns="{NETCONF_NS}">
              <source><running/></source>
              <filter
                type="xpath"
                xmlns:if="{IETF_INTERFACES_NS}"
                select="/if:interfaces/if:interface[contains(if:name, 'ge-0/0/0')]"/>
            </get-config>
            """,
        )
        xpath_xml = str(xpath_reply.xml)
        if "ge-0/0/0" not in xpath_xml or "interop-uplink" not in xpath_xml:
            fail("experimental XPath filter did not return expected interface")
        if "xe-0/0/0" in xpath_xml or "interop-peer" in xpath_xml:
            fail("experimental XPath filter returned predicate-mismatched interface")

        assert_rpc_error(
            lambda: dispatch_xml(
                session,
                f"""
                <get-config xmlns="{NETCONF_NS}">
                  <source><running/></source>
                  <filter
                    type="xpath"
                    xmlns:if="{IETF_INTERFACES_NS}"
                    select="/if:interfaces/if:interface[if:ipv4-table-id='not-a-number']"/>
                </get-config>
                """,
            ),
            "invalid-value",
        )

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

        assert_rpc_error(
            lambda: dispatch_xml(
                session,
                f"""
                <edit-config xmlns="{NETCONF_NS}">
                  <target><running/></target>
                  <config>
                    <system>
                      <host-name>running-write-ci</host-name>
                    </system>
                  </config>
                </edit-config>
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

            session.lock(target="candidate")
            locked = True
            discard_config = """
            <config>
              <system>
                <host-name>discarded-ci</host-name>
              </system>
            </config>
            """
            session.edit_config(
                target="candidate",
                config=discard_config,
                default_operation="replace",
                test_option="test-then-set",
                error_option="rollback-on-error",
            )
            session.validate(source="candidate")

            candidate = session.get_config(source="candidate").data_xml
            if "discarded-ci" not in candidate:
                fail("candidate did not include staged hostname before discard")

            dispatch_xml(session, f'<discard-changes xmlns="{NETCONF_NS}"/>')

            discarded = session.get_config(source="candidate").data_xml
            if "discarded-ci" in discarded:
                fail("discarded hostname was still visible in candidate configuration")
            if "ncclient-ci" not in discarded:
                fail("candidate fallback after discard did not reflect running configuration")

            session.unlock(target="candidate")
            locked = False

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
