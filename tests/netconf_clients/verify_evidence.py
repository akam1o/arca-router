#!/usr/bin/env python3
import argparse
import sys
from pathlib import Path


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

REQUIRED_CAPABILITIES = {
    CAP_BASE_10,
    CAP_BASE_11,
    CAP_CANDIDATE,
    CAP_VALIDATE,
    CAP_ROLLBACK,
    CAP_ARCA_ROUTER,
    CAP_ARCA_XPATH_FILTER_SUBSET,
}
FORBIDDEN_CAPABILITIES = {
    CAP_XPATH,
    CAP_STARTUP,
    CAP_WRITABLE_RUNNING,
    CAP_CONFIRMED_COMMIT,
}

COMMON_FILES = {
    "metadata.txt",
    "server.log",
    "server_capabilities.txt",
    "running.conf",
}
CLIENT_FILES = {
    "ncclient": {
        "client_versions.txt",
        "rpc/xpath-node-set.xml",
        "reply/xpath-node-set.xml",
        "rpc/xpath-invalid-predicate.xml",
        "reply/xpath-invalid-predicate.xml",
        "rpc/startup-get-config-rejected.xml",
        "reply/startup-get-config-rejected.xml",
        "rpc/startup-copy-config-rejected.xml",
        "reply/startup-copy-config-rejected.xml",
        "rpc/writable-running-rejected.xml",
        "reply/writable-running-rejected.xml",
        "rpc/discard-changes.xml",
        "reply/discard-changes.xml",
    },
    "libnetconf2": {
        "rpc/xpath-node-set.xml",
        "reply/xpath-node-set.xml",
        "rpc/xpath-scalar-rejected.xml",
        "reply/xpath-scalar-rejected.xml",
        "rpc/xpath-attribute-rejected.xml",
        "reply/xpath-attribute-rejected.xml",
    },
}


def parse_args():
    parser = argparse.ArgumentParser(description="verify NETCONF interop evidence")
    parser.add_argument(
        "evidence_dir",
        nargs="?",
        default="artifacts/netconf-clients",
        help="directory containing ncclient/ and libnetconf2/ evidence",
    )
    parser.add_argument(
        "--expect-standard-xpath",
        action="store_true",
        help="require standard NETCONF :xpath capability instead of forbidding it",
    )
    return parser.parse_args()


def fail(message):
    print(f"netconf evidence verification failed: {message}", file=sys.stderr)
    sys.exit(1)


def read_text(path):
    try:
        return path.read_text(encoding="utf-8")
    except OSError as err:
        fail(f"cannot read {path}: {err}")


def require_files(client_dir, relative_paths):
    missing = sorted(
        str(relative_path)
        for relative_path in relative_paths
        if not (client_dir / relative_path).is_file()
    )
    if missing:
        fail(f"{client_dir.name} evidence missing files: {missing}")


def require_capabilities(client_dir, expect_standard_xpath):
    capabilities = set(read_text(client_dir / "server_capabilities.txt").splitlines())
    required = set(REQUIRED_CAPABILITIES)
    if expect_standard_xpath:
        required.add(CAP_XPATH)
    missing = sorted(required - capabilities)
    if missing:
        fail(f"{client_dir.name} evidence missing capabilities: {missing}")

    forbidden = set(FORBIDDEN_CAPABILITIES)
    if expect_standard_xpath:
        forbidden.discard(CAP_XPATH)
    advertised = sorted(forbidden & capabilities)
    if advertised:
        fail(f"{client_dir.name} evidence advertised unsupported capabilities: {advertised}")


def require_text(client_dir, relative_path, needles):
    content = read_text(client_dir / relative_path)
    missing = [needle for needle in needles if needle not in content]
    if missing:
        fail(f"{client_dir.name} {relative_path} missing text: {missing}")


def verify_client(root, client, expect_standard_xpath):
    client_dir = root / client
    if not client_dir.is_dir():
        fail(f"missing {client} evidence directory: {client_dir}")

    require_files(client_dir, COMMON_FILES | CLIENT_FILES[client])
    require_capabilities(client_dir, expect_standard_xpath)
    require_text(client_dir, "reply/xpath-node-set.xml", ["ge-0/0/0", "interop-uplink"])

    if client == "ncclient":
        require_text(client_dir, "reply/xpath-invalid-predicate.xml", ["invalid-value"])
        require_text(client_dir, "reply/startup-get-config-rejected.xml", ["operation-not-supported"])
        require_text(client_dir, "reply/startup-copy-config-rejected.xml", ["operation-not-supported"])
        require_text(client_dir, "reply/writable-running-rejected.xml", ["operation-not-supported"])
    if client == "libnetconf2":
        require_text(client_dir, "reply/xpath-scalar-rejected.xml", ["invalid-value"])
        require_text(client_dir, "reply/xpath-attribute-rejected.xml", ["invalid-value"])


def main():
    args = parse_args()
    root = Path(args.evidence_dir)
    for client in ("ncclient", "libnetconf2"):
        verify_client(root, client, args.expect_standard_xpath)
    print(f"NETCONF evidence OK: {root}")


if __name__ == "__main__":
    main()
