#!/usr/bin/env python3
"""Idempotently configure a Cloudflare Pages project, custom domain, and DNS."""

from __future__ import annotations

import argparse
import json
import os
import urllib.error
import urllib.parse
import urllib.request


API = "https://api.cloudflare.com/client/v4"


def request(method: str, url: str, token: str, payload: dict[str, object] | None = None) -> dict[str, object]:
    data = None if payload is None else json.dumps(payload).encode()
    req = urllib.request.Request(
        url,
        data=data,
        method=method,
        headers={"Authorization": f"Bearer {token}", "Content-Type": "application/json"},
    )
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            body = resp.read().decode()
    except urllib.error.HTTPError as exc:
        body = exc.read().decode()
        try:
            parsed = json.loads(body) if body else {}
        except json.JSONDecodeError:
            parsed = {"raw": body}
        raise SystemExit(f"{method} {url} failed: HTTP {exc.code} {parsed}") from exc

    parsed = json.loads(body) if body else {}
    if not parsed.get("success", True):
        raise SystemExit(f"{method} {url} failed: {parsed}")
    return parsed


def ensure_project(account_id: str, token: str, project: str, branch: str) -> str:
    url = f"{API}/accounts/{account_id}/pages/projects/{project}"
    try:
        result = request("GET", url, token)
        print(f"project exists: {project}")
        return result["result"]["subdomain"]
    except SystemExit as exc:
        if "HTTP 404" not in str(exc):
            raise

    result = request(
        "POST",
        f"{API}/accounts/{account_id}/pages/projects",
        token,
        {"name": project, "production_branch": branch},
    )
    print(f"created project: {project}")
    return result["result"]["subdomain"]


def ensure_custom_domain(account_id: str, token: str, project: str, domain: str) -> None:
    base = f"{API}/accounts/{account_id}/pages/projects/{project}/domains"
    try:
        request("GET", f"{base}/{domain}", token)
        print(f"domain exists: {domain}")
        return
    except SystemExit as exc:
        if "HTTP 404" not in str(exc):
            raise

    request("POST", base, token, {"name": domain})
    print(f"attached domain: {domain}")


def find_zone(token: str, zone_name: str) -> str:
    query = urllib.parse.urlencode({"name": zone_name})
    result = request("GET", f"{API}/zones?{query}", token)
    zones = result.get("result") or []
    if not zones:
        raise SystemExit(f"zone not found: {zone_name}")
    return zones[0]["id"]


def ensure_dns(token: str, zone_name: str, domain: str, target: str) -> None:
    zone_id = find_zone(token, zone_name)
    base = f"{API}/zones/{zone_id}/dns_records"
    query = urllib.parse.urlencode({"name": domain})
    result = request("GET", f"{base}?{query}", token)
    payload = {"type": "CNAME", "name": domain, "content": target, "ttl": 1, "proxied": True}
    matches = result.get("result") or []

    if matches:
        record_id = matches[0]["id"]
        record = matches[0]
        if (
            record.get("type") == payload["type"]
            and record.get("content") == payload["content"]
            and record.get("proxied") is True
        ):
            print(f"DNS exists: {domain} -> {target}")
            return
        request("PUT", f"{base}/{record_id}", token, payload)
        print(f"updated DNS: {domain} -> {target}")
        return

    request("POST", base, token, payload)
    print(f"created DNS: {domain} -> {target}")


def default_zone(domain: str) -> str:
    parts = domain.split(".")
    if len(parts) < 2:
        raise SystemExit(f"cannot infer zone from domain: {domain}")
    return ".".join(parts[-2:])


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--project", required=True)
    parser.add_argument("--domain")
    parser.add_argument("--zone")
    parser.add_argument("--branch", default="main")
    parser.add_argument("--dns-target")
    args = parser.parse_args()

    account_id = os.environ["CLOUDFLARE_ACCOUNT_ID"]
    token = os.environ["CLOUDFLARE_API_TOKEN"]

    # The pages.dev subdomain is not always {project}.pages.dev: when the
    # name is taken globally, Cloudflare assigns a suffixed one. A CNAME at
    # the guessed name points at a stranger's project and the custom domain
    # never validates, so always ask the API for the real subdomain.
    subdomain = ensure_project(account_id, token, args.project, args.branch)
    if args.domain:
        target = args.dns_target or subdomain
        zone = args.zone or default_zone(args.domain)
        ensure_custom_domain(account_id, token, args.project, args.domain)
        ensure_dns(token, zone, args.domain, target)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
