#!/usr/bin/env python3
# scripts/set-password.py
"""Set an account's password through Kratos' admin API.

For the operator of a deployment who has lost the way in. The password is read
from a terminal prompt or from stdin and is never an argument, so it does not
reach the process list, the shell history or a file. This script writes nothing
to disk.

Kratos' admin API is loopback-only by configuration, which is what makes this
safe to run and impossible to run from anywhere else.
"""

import argparse
import getpass
import json
import sys
import urllib.error
import urllib.request
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[1]


def call(admin, method, path, body=None):
    data = json.dumps(body).encode() if body is not None else None
    request = urllib.request.Request(admin + path, data=data, method=method,
                                     headers={"Accept": "application/json",
                                              "Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(request, timeout=15) as response:
            payload = response.read()
            return json.loads(payload) if payload else None
    except urllib.error.HTTPError as error:
        raise SystemExit(
            f"kratos answered {error.code}: {error.read().decode()[:400]}")
    except urllib.error.URLError as error:
        raise SystemExit(f"cannot reach Kratos at {admin}: {error.reason}")


def admin_url(config_path):
    settings = yaml.safe_load(Path(config_path).read_text()) or {}
    url = (settings.get("identity") or {}).get("admin_url")
    if not url:
        raise SystemExit(f"{config_path} has no identity.admin_url")
    return url.rstrip("/")


def find(admin, email):
    for identity in call(admin, "GET", "/admin/identities?per_page=250") or []:
        traits = identity.get("traits") or {}
        if traits.get("email") == email or traits.get("username") == email:
            return identity
    raise SystemExit(f"no account with the address {email}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("email", help="the account to change")
    parser.add_argument("--config", default=str(Path.home() /
                        ".config/goptivum-server/config.yaml"))
    parser.add_argument("--stdin", action="store_true",
                        help="read the password from stdin instead of prompting")
    arguments = parser.parse_args()

    admin = admin_url(arguments.config)
    identity = find(admin, arguments.email)

    if arguments.stdin:
        password = sys.stdin.readline().rstrip("\n")
    else:
        password = getpass.getpass("New password: ")
        if password != getpass.getpass("Repeat it: "):
            raise SystemExit("the two passwords are not the same")
    if len(password) < 8:
        raise SystemExit("Kratos refuses anything shorter than 8 characters")

    # A read-modify-write through PUT, matching backend/identity/admin.go: a
    # JSON patch of the password config answers 200 and destroys the
    # credential, after which neither password signs in. Everything read is
    # sent back, including metadata_admin, which holds the first-sign-in mark.
    call(admin, "PUT", "/admin/identities/" + identity["id"], {
        "schema_id": identity.get("schema_id", "default"),
        "state": identity.get("state", "active"),
        "traits": identity.get("traits", {}),
        "metadata_public": identity.get("metadata_public", {}),
        "metadata_admin": identity.get("metadata_admin", {}),
        "credentials": {"password": {"config": {"password": password}}},
    })
    print(f"password set for {arguments.email}")


if __name__ == "__main__":
    main()
