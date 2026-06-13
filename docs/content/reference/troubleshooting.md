---
title: "Troubleshooting"
description: "Guest-denied endpoints, needs-auth, rate limits, not-found, query-id rotation, and clearing the cache."
weight: 40
---

x reports a specific exit code for each kind of failure, and the message names
what to do. This page maps the common ones to a fix.

## Endpoints the guest tier cannot reach

X denies a guest token (Tier 1) on several endpoints. Under `--guest` they come
back as not-found even though the request was well-formed. These need your own
session instead:

- `replies`
- `media`
- `thread`
- `search` (on some accounts and windows)
- `followers`

If one of these returns nothing or not-found under `--guest`, import your
session and run it again:

```bash
x auth import --auth-token <auth_token> --ct0 <ct0>
x replies nasa
```

## Needs auth (exit 4)

A command exits with code `4` when it needs a tier you have not enabled. The
message names the tier. Two fixes, depending on which it asks for:

- It wants the **guest** tier: add `--guest`.
- It wants a **session**: run `x auth import` once, then re-run the command.

Reads like `search`, `followers`, and `likes` want guest-or-session; writes,
`home`, and `bookmarks` always want a session.

## Rate-limited (exit 5)

Code `5` means X throttled the request. x already retries 429s up to `--retries`
times and paces requests at `--rate`. If you still hit it:

- Slow down: raise `--rate` (for example `--rate 3s`).
- Make sure the guest token is being reused, not re-minted on every run; a
  cached token avoids extra mint calls. The cache is on by default.
- Wait a few minutes and try again. Rate limits are time-windowed.

## Not found (exit 6)

Code `6` means the tweet, user, or list does not exist, is private, or is
unreachable on the tier you used. Check that:

- The handle or id is correct (use `--id` if you are passing a numeric user id).
- The account is not protected or suspended.
- You are not hitting a guest-denied endpoint from the list above; if so, switch
  to a session.

## Query-id rotation

x calls the web-client GraphQL with operation ids that X rotates occasionally.
When X rotates one before a new release ships, the affected command starts
failing even though everything else works. Tell it apart from a real not-found:
the same command works on a different surface, or `-v` shows the GraphQL
endpoint returning an operation error.

Override the id for the run without waiting for an update:

```bash
x search "webb telescope" --guest --query-id SearchTimeline=<hash>
```

`--query-id` takes `Op=hash` and can be repeated for commands that issue more
than one operation. Upgrading x to the latest release picks up the current ids.

## Clearing the cache

Stale or corrupt cached responses can be cleared:

```bash
x cache clear        # delete all cached responses
x <command> --no-cache   # bypass the cache for one run
```

For a full reset (no session, no guest token, no cache), point `--data-dir` at a
fresh directory or remove the data directory `x config path` reports.
