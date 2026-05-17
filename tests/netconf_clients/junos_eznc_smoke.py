#!/usr/bin/env python3
import argparse
import sys

from jnpr.junos import Device


CAP_BASE_10 = "urn:ietf:params:netconf:base:1.0"
CAP_BASE_11 = "urn:ietf:params:netconf:base:1.1"
CAP_CANDIDATE = "urn:ietf:params:netconf:capability:candidate:1.0"
CAP_STARTUP = "urn:ietf:params:netconf:capability:startup:1.0"


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
        missing = sorted({CAP_BASE_10, CAP_BASE_11, CAP_CANDIDATE} - caps)
        if missing:
            fail(f"missing server capabilities: {missing}")
        if CAP_STARTUP in caps:
            fail("startup capability should not be advertised")
    finally:
        if dev.connected:
            dev.close()


if __name__ == "__main__":
    main()
