---
title: "Troubleshooting"
description: "Guest-denied endpoints, needs-auth, rate limits, not-found, query-id rotation, and clearing the cache."
weight: 40
---

x reports a specific exit code for each kind of failure, and the message names
what to do. This page maps the common ones to a fix.

## Endpoints the guest tier cannot reach

X denies a guest token (Tier 1) on almost everything. Under `--guest` the denied
calls come back 404 with an empty body even though the request was well-formed,
which is how X says no to a credential it does not accept.

Measured on 2026-07-28, running each command under `--tier guest`, a guest token
reaches four operations and no others:

- `x user <handle>`, the profile read
- `x user <id> --id`, the same read by numeric account id
- `x timeline`, the deeper walk back through an account, by handle or by `--id`
- `x space`, the whole record of an audio Space

`x space` is the one that took finding. Probing that route with no variables
answers 422 rather than the 404 the walled operations answer, so it reads as
denied until a well-formed request goes out and comes back with the Space.

Everything else on the GraphQL surface was denied: the profile media tab,
`search` and the commands built on it (`counts`, `quotes`, `mentions`),
`followers`, `following`, `likers`, `retweeters`, `likes`, `list`, and the
conversation call behind `thread`. Those need your own session:

```bash
x auth import --auth-token <auth_token> --ct0 <ct0>
x followers nasa
```

`thread` and `replies` are the exception that needs nothing. They stopped going
through GraphQL: the conversation above a tweet comes off the syndication
endpoint and the replies below it off the status page, so both work at Tier 0
with no credential at all.

One operation says no in a way that is easy to miss. `timeline --replies`, and
`replies <handle>` which is the same read, goes to a call the guest tier neither
serves nor refuses: it returns 200 with an empty timeline, which renders as an
account that has never replied to anybody. So x does not send a guest token
there at all. That read stays on Tier 0, where the replies actually are, until
you have a session. The only shape Tier 0 cannot answer is `--id`, since no
anonymous surface takes a numeric account id.

## Needs auth (exit 4)

A command exits with code `4` when it needs a tier you have not enabled. The
message names the tier. Two fixes, depending on which it asks for:

- It wants the **guest** tier: add `--guest`.
- It wants a **session**: run `x auth import` once, then re-run the command.

Reads like `search`, `followers`, and `likes` want a session, and so do `home`
and `bookmarks`. `--guest` only ever helps `x user`, `x timeline` and `x space`,
so those are the only reads whose exit 4 mentions it. Anything else that exits 4
asks for a session, because that is the only thing that would fix it.

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
