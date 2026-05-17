#!/usr/bin/env python3
import argparse
import sys

from jnpr.junos import Device


CAP_BASE_10 = "urn:ietf:params:netconf:base:1.0"
CAP_BASE_11 = "urn:ietf:params:netconf:base:1.1"
CAP_CANDIDATE = "urn:ietf:params:netconf:capability:candidate:1.0"
CAP_VALIDATE = "urn:ietf:params:netconf:capability:validate:1.1"
CAP_ROLLBACK = "urn:ietf:params:netconf:capability:rollback-on-error:1.0"
CAP_STARTUP = "urn:ietf:params:netconf:capability:startup:1.0"
CAP_WRITABLE_RUNNING = "urn:ietf:params:netconf:capability:writable-running:1.0"
CAP_CONFIRMED_COMMIT = "urn:ietf:params:netconf:capability:confirmed-commit:1.1"
CAP_ARCA_ROUTER = "urn:arca:router:config:1.0?module=arca-router&revision=2025-12-27"
CAP_ARCA_XPATH_FILTER_SUBSET = "urn:arca:router:netconf:capability:xpath-filter-subset:1.0"


def parse_args():
    parser = argparse.ArgumentParser(description="Junos PyEZ NETCONF smoke test")
    parser.add_argument("--host", required=True)
    parser.add_argument("--port", required=True, type=int)
    parser.add_argument("--username", required=True)
    parser.add_argument("--password", required=True)
    return parser.parse_args()


def fail(message):
    print(f"junos-eznc smoke failed: {message}", file=sys.stderr)
    sys.exit(1)


def main():
    args = parse_args()
    dev = Device(
        host=args.host,
        port=args.port,
        user=args.username,
        passwd=args.password,
        gather_facts=False,
        hostkey_verify=False,
        look_for_keys=False,
        allow_agent=False,
    )

    try:
        dev.open(gather_facts=False)
        if not dev.connected:
            fail("Device.open() returned but device is not connected")

        caps = {str(cap) for cap in dev._conn.server_capabilities}
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
            CAP_STARTUP,
            CAP_WRITABLE_RUNNING,
            CAP_CONFIRMED_COMMIT,
        }
        advertised = sorted(forbidden & caps)
        if advertised:
            fail(f"unsupported capabilities were advertised: {advertised}")
    finally:
        if dev.connected:
            dev.close()


if __name__ == "__main__":
    main()
