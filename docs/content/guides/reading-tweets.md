---
title: "Reading tweets"
description: "Single tweets, timelines, replies, media, threads, polls, profiles, quotes, and mentions, with the tier each one needs."
weight: 10
---

Most reading on X is free and needs no auth. Some of it needs a guest token
(`--guest`), and a few endpoints X reserves for a real session. This guide walks
the read commands and marks the tier each needs.

A `<ref>` anywhere below is a tweet id (`20`), a status URL, or anything x can
resolve to a tweet. A `<user>` is a handle (`nasa`), or a numeric id with
`--id`.

## A single tweet, a profile

```bash
x tweet 20            # one tweet, Tier 0, no auth
x user nasa           # a profile with metrics, Tier 0
x poll <ref>          # a tweet's poll options and current tallies
```

`x tweet` and `x user` work straight off syndication. `x poll` reads the same
tweet and prints each option with its vote count.

## Timelines and replies

```bash
x timeline nasa               # recent window, Tier 0
x timeline nasa --guest -n 50 # deeper, guest tier
x timeline nasa --media       # only tweets with media
x timeline nasa --replies     # include the user's replies
x replies nasa --guest        # a user's tweets including replies
```

`x timeline` returns a recent window on Tier 0 and pages further back with
`--guest` or a session. `x replies` is the replies-inclusive view; X denies it
to guest tokens, so it needs your own session.

## Media

```bash
x media nasa --guest          # media attached to a user's tweets
```

`x media` lists the photo and video tweets for a user. It is one of the
endpoints X denies to guest tokens, so in practice it needs your session; pass
`--guest` only to try the guest path.

## Threads

```bash
x thread <ref>                # the conversation around a tweet
```

`x thread` reconstructs the conversation a tweet belongs to. Like `replies` and
`media`, X reserves it for a real session.

## Quotes and mentions

```bash
x quotes <ref>                # quote tweets of a tweet
x mentions nasa               # tweets mentioning a user
```

Both are search-backed: x runs a query under the hood, so they need a tier that
can search, which means `--guest` or a session.

## Which tier each needs

| Command | Tier 0 | Guest | Session |
|---|---|---|---|
| `tweet`, `user`, `poll` | yes | yes | yes |
| `timeline` | recent window | deeper | deeper |
| `replies` | no | denied by X | yes |
| `media` | no | denied by X | yes |
| `thread` | no | denied by X | yes |
| `quotes`, `mentions` | no | yes | yes |

When a command needs a tier you have not enabled, x exits with code `4`
(needs-auth) and names the tier. See
[troubleshooting](/reference/troubleshooting/) for the guest-denied list and
what to do about it.
