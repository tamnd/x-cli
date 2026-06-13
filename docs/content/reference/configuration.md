---
title: "Configuration"
description: "The config file and data directory, the tier and query-id overrides, request tuning, the cache, and where the session and guest token live."
weight: 30
---

x runs with sensible defaults and no required setup. Everything here is optional
tuning. See where things resolve to with `x config show`.

## Config file and data directory

```bash
x config path     # print the config file path
x config show     # print the resolved configuration
```

x keeps its state under a single data directory: the HTTP cache, the saved
session, the cached guest token, and the config file. The default location
follows the platform convention (an OS user data directory). Override the root
for one run with `--data-dir`, or for all runs by setting it in the config.

## Tiers

By default x auto-selects the cheapest tier that can serve a command. Two flags
override that:

- `--guest` enables the opt-in guest-GraphQL tier (Tier 1) for the run.
- `--tier syndication|guest|session` forces a specific tier, so you can pin a
  command to one surface instead of letting x choose.

`x info` prints which tiers are available and what each can do right now.

## Query-id overrides

x calls the web-client GraphQL with operation ids that X rotates from time to
time. x ships with current ids, but if X rotates one before an update lands you
can override it:

```bash
x search "webb telescope" --guest --query-id SearchTimeline=<hash>
```

`--query-id` takes `Op=hash` and can be repeated. See
[troubleshooting](/reference/troubleshooting/) for how to tell a rotation apart
from other failures.

## Request tuning

x behaves like a careful browser. These flags shape how it talks to X:

| Flag | Default | Meaning |
|---|---|---|
| `--rate` | `1s` | Minimum delay between requests |
| `--retries` | `3` | Retries on 429 and 5xx responses |
| `--timeout` | `30s` | Per-request timeout |

Lowering `--rate` makes x faster but more likely to be rate-limited; the default
is a polite pace.

## Cache

Reads are cached on disk under the data directory so repeated lookups are
instant and gentle on X.

```bash
x cache clear     # delete all cached responses
```

Bypass the cache for one run with `--no-cache`. The cache holds read responses
only; it never stores writes.

## Where the session and guest token live

Both live under the data directory, not in the config file:

- The **session** (your imported `auth_token` and `ct0`) is saved by
  `x auth import` and removed by `x auth logout`. Check it with `x auth status`.
- The **guest token** is minted on demand by `--guest` and cached between runs,
  so repeated guest commands reuse it instead of re-minting and tripping X's
  rate limit. Clearing it is handled automatically when it expires; you do not
  manage it by hand.

Because both are files under the data directory, pointing `--data-dir` at a
fresh path gives you a clean slate with no session and no cache.
