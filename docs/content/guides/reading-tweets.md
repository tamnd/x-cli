---
title: "Reading tweets"
description: "Single tweets, timelines, replies, media, threads, polls, profiles, quotes, and mentions, with the tier each one needs."
weight: 10
---

Most reading on X is free and needs no auth. A guest token (`--guest`) buys five
operations, chiefly the deeper timeline walk and an audio Space, and the rest of
the GraphQL surface X reserves for a real session. This guide walks the read
commands and marks the tier each needs.

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
x replies nasa                # the same thing, said shorter
x replies 1903142823316049977 # the replies to a tweet
```

`x timeline` returns a recent window on Tier 0 and pages further back with
`--guest` or a session.

`x replies` reads whichever of the two things you handed it. A handle is that
account's timeline with the replies left in. A tweet is the replies to that
tweet, which at Tier 0 means the handful X renders on the status page: there is
no cursor there, so the command says how many it got out of how many exist and
points at `x auth import` for the rest.

A timeline is the account's own tweets. X renders a reply on a profile together
with the tweet it answers, and the page's own data says so, so a naive read of
@jack's profile hands back tweets by four other people. They are context on a
profile and they are the answer on a status page, but on `x timeline jack` they
are a wrong answer, and each one also spends one of your `-n`. A repost is the
case this cannot get right: X shows it under the original author and nothing on
the page says it was reposted, so it looks exactly like a reply parent and drops
with them.

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

`--replies` is the exception: it stays on Tier 0 even with `--guest`, and only a
session pages it deeper. The guest tier does not refuse that read, which would
be fine, it answers it with an empty timeline and no error, which reads as an
account that has never replied to anybody. Tier 0 has the replies, so x uses
Tier 0. The one shape it cannot serve is `--id`, because no anonymous surface
takes a numeric account id, and that combination asks for a session.

## Media

```bash
x media 2081860978694594863            # the pictures on one tweet
x media nasa                           # the media on a user's recent tweets
x media nasa --tab                     # the profile's media tab, all of it
x media nasa --download ./out          # the bytes, not the records
```

`x media` takes a tweet or a profile. A tweet is answered at tier 0, and so is
the recent window of a profile; `--tab` reads X's own index of every post with a
picture in it, which is a GraphQL operation X now answers only for a session.

`--size` is `thumb|small|medium|large|orig` and defaults to `orig`, because a
photo URL carries its size in it and the point of downloading a picture is to
have the picture. `--variant` picks a video rendition by resolution or bitrate;
the default is the highest-bitrate MP4, since the playlist X also offers is the
right answer for a player and the wrong one for a file on disk.

## Threads

```bash
x thread <ref>                # the conversation around a tweet
```

`x thread` reconstructs the conversation a tweet belongs to, root first, and it
works with no credential at all.

Going up is free because the syndication endpoint expands a reply's parent in
full, one level deep. That is enough to halve the walk: asking for a tweet hands
back the tweet and its parent, so the next request asks for the grandparent. A
chain of three costs two requests. Going down is the expensive direction, and at
Tier 0 it is whatever the status page rendered, which is usually three.

The upward half also pays for itself in another way. A tweet fetched on its own
carries no retweet count, and the same tweet arriving as somebody's parent
carries one.

## Quotes and mentions

```bash
x quotes <ref>                # quote tweets of a tweet
x mentions nasa               # tweets mentioning a user
```

Both are search-backed: x runs a query under the hood, so they need a tier that
can search, and search is session-only. `--guest` is refused.

## Trends and places

```bash
x trends                      # worldwide
x trends tokyo                # by name
x trends 23424977             # or by woeid, which is the same thing
x places japan                # find a woeid
x places --country US --type town
```

X keys its trend lists by woeid, the Yahoo! Where On Earth id, an identifier
scheme whose owner shut down in 2019. X kept the numbers, so `x places` is how
you find the one you want; the directory is 467 entries and x caches it for a
week.

A trend's id is its woeid and its name, because the same word trending in Tokyo
and in London is two different facts. The `volume` column is empty on every
capture taken: X sends `tweet_volume` null now, everywhere, and an empty cell is
the honest rendering of a number nobody gave you.

Both commands are Tier 0. They go to a v1.1 route that still answers on the
public web bearer, and attaching a guest token to it is worse than useless: it
cuts the budget from 180 requests per fifteen minutes to 15.

## Which tier each needs

| Command | Tier 0 | Guest | Session |
|---|---|---|---|
| `tweet`, `user`, `poll` | yes | yes | yes |
| `timeline` | recent window | deeper | deeper |
| `timeline --replies` | recent window | same read, guest buys nothing | deeper |
| `replies` | what the page renders, or a user's own | same read | the whole tree |
| `media` | a tweet, and a recent window | same | the media tab too |
| `thread` | ancestors, plus what the page renders | same | the whole tree |
| `quotes`, `mentions` | no | denied by X | yes |
| `trends`, `places` | yes | same read | same read |

When a command needs a tier you have not enabled, x exits with code `4`
(needs-auth) and names the tier. See
[troubleshooting](/reference/troubleshooting/) for the guest-denied list and
what to do about it.
