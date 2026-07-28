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

A profile at tier 0 is not a cut-down profile: the syndication widget states the
counters, the banner, the website and what the verified tick means, the same
fields the guest tier gives. What tier 0 costs is two requests instead of one and
a smaller window, 30 profile reads per fifteen minutes against 150. With
`--guest`, `x user` takes the one-request route and says so in `surfaces`. When
the widget's window is spent it falls back to x.com's own page, which has fewer
fields, and the record says which surface it went without:

```bash
x user nasa -o json | jq '{surfaces, missed}'
```

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

Two things about tier 0 here. The syndication widget answers an account that
posts often with the last hundred or so posts, and an account that posts rarely
with a ranked selection from its whole history: @jack's came back spanning 2006
to 2025 in like order. x measures which it got and marks the ranked case
`sample`, warning once on stderr. And the widget's window is 30 requests per
fifteen minutes, so when it is spent the read falls back to x.com's own page,
which is fewer tweets; the records name the surface they went without in
`missed`.

With `--guest`, x walks the cursor instead, which is chronological and pages
back a long way. Measured on @NASA: 812 posts in one run, from the day of the
read back eight months, no repeats. Ask for more than a window allows and the
run stops with the reason and the count rather than pretending it reached the
end:

```bash
x timeline nasa --guest -n 2000 -o jsonl > nasa.jsonl
```

## Media

```bash
x media 2081860978694594863            # the pictures on one tweet
x media nasa                           # the media on a user's recent tweets
x media nasa --tab --guest             # the profile's media tab, all of it
x media nasa --download ./out          # the bytes, not the records
```

`x media` takes a tweet or a profile. A tweet is answered at tier 0, and so is
the recent window of a profile; `--tab` reads X's own index of every post with a
picture in it, which is a GraphQL operation and needs `--guest` or your session.

`--size` is `thumb|small|medium|large|orig` and defaults to `orig`, because a
photo URL carries its size in it and the point of downloading a picture is to
have the picture. `--variant` picks a video rendition by resolution or bitrate;
the default is the highest-bitrate MP4, since the playlist X also offers is the
right answer for a player and the wrong one for a file on disk.

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
