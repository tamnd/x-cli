---
title: "Graph discovery"
description: "Walk the graph linked from a tweet or user breadth-first with x discover: follow authors, quotes, replies, mentions, likers, retweeters, the follow graph, and more."
weight: 45
---

Every other read answers one question about one object. `x discover` chains them:
it starts at a tweet or a user and follows that object's links outward, hop by
hop, streaming every node it reaches. It is a breadth-first walk of the X graph.

```bash
x discover 1234567890        # what is this tweet linked to?
x discover nasa              # what is this account linked to?
```

A *seed* is any tweet or user reference: a tweet id or status URL, or a handle
or profile URL. Pass more than one to start the walk from several places at once.

## What gets followed

The default follows a post's **content** and stays entirely on Tier 0, so it
works with no token: the author, the tweet it quotes or retweets, its reply
parent, the accounts it mentions, and (for a user) their pinned tweet.

Choose what to follow with `--follow`. It takes a preset:

```bash
x discover <ref> --follow content      # author, quote, retweet, reply, mention, pinned (default)
x discover <ref> --follow thread       # author, reply, replies, quote
x discover <ref> --follow engagement   # liker, retweeter, quotedby
x discover <user> --follow network     # following, followers
x discover <user> --follow timeline    # timeline, pinned, author
x discover <ref> --follow all          # everything
```

or a comma-separated list of individual edges:

```bash
x discover <ref> --follow author,quote,mention
x discover <ref> --follow replies,liker
```

The full edge vocabulary:

| Edge | From → to | Tier | What it follows |
|---|---|---|---|
| `author` | tweet → user | 0 | who wrote the tweet |
| `quote` | tweet → tweet | 0 | the tweet it quotes |
| `retweet` | tweet → tweet | 0 | the original it retweets |
| `reply` | tweet → tweet | 0 | the tweet it replies to |
| `mention` | tweet → user | 0 | accounts it @-mentions |
| `pinned` | user → tweet | 0 | the account's pinned tweet |
| `timeline` | user → tweet | 0 | the account's recent tweets |
| `replies` | tweet → tweet | guest/session | the replies under it |
| `liker` | tweet → user | guest/session | accounts that liked it |
| `retweeter` | tweet → user | guest/session | accounts that retweeted it |
| `quotedby` | tweet → tweet | guest/session | tweets that quote it |
| `following` | user → user | guest/session | accounts it follows |
| `followers` | user → user | guest/session | accounts that follow it |
| `likes` | user → tweet | guest/session | tweets it liked |

The Tier-0 edges work with nothing. The rest read the GraphQL surface, so they
need `--guest` or your own session (`x auth import`). When you ask for an edge
you have no tier for, `x discover` drops it with a one-line note on stderr and
keeps going on what it can reach, rather than failing the whole walk. The one
exception is when *every* edge you asked for needs a tier: then there is nothing
to do and it exits `4` with the tier to add.

## How far and how wide

```bash
x discover <ref> --depth 2            # follow two hops from the seed (default 1)
x discover <ref> --fanout 50          # up to 50 neighbors per edge (default 25)
x discover <ref> --fanout 0           # no per-edge cap
x discover <ref> -n 1000              # stop after 1000 nodes total (default 500)
```

`--depth` is how many hops to follow. `--fanout` caps how many neighbors each
edge contributes per node, so one hop never pages a whole follower graph unless
you raise it. `-n/--limit` is the total node budget, the hard stop on a deep or
wide walk.

## Reading the output

`x discover` streams one row per node, tagged with how it was reached:

```text
depth  via      kind   id   who     summary                   url
0               tweet  20   @jack   just setting up my twttr  https://x.com/jack/status/20
1      author   user   12   @jack   jack                      https://x.com/jack
```

Because it streams through the same formatter as every read, it shapes and pipes
the same way. The JSON forms carry the full node, with the nested tweet or user:

```bash
x discover <ref> -o json | jq -r '.via + " -> " + (.tweet.id // .user.username)'
x discover <user> --follow network --guest -o jsonl | jq -r '.user.username' | sort -u
x discover <ref> --fields depth,via,who,url -o table
```

## Persisting a walk

Add `--store` to write every node and edge into the local store as the walk
streams, so you keep the graph as well as see it:

```bash
x discover nasa --follow network --depth 2 --guest --store
x db query "select kind, count(*) from edges group by 1 order by 2 desc"
```

When you want the dataset rather than the live answer, reach for
[`x crawl`](/guides/local-store/), which is the same walk pointed at the store
instead of stdout. See [the local store](/guides/local-store/) for inspecting
and exporting what you collect.
