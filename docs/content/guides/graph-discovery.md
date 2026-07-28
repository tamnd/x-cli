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

## The graph one record already carries

Before walking anywhere, look at what a single read is worth. `x edges` fetches
the references you name and prints the claims those records make, as
`from predicate to` with the URL each claim came from:

```bash
x edges 1903142823316049977 --fields from,predicate,to -o table
```

```text
from                           predicate   to
x://tweet/1903142823316049977  replies_to  x://tweet/1903136743634723031
x://tweet/1903142823316049977  mentions    x://user/jack
x://tweet/1903142823316049977  mentions    x://user/marmoushera
x://user/guyfishermoney        authored    x://tweet/1903142823316049977
x://user/marmoushera           authored    x://tweet/1903136743634723031
```

That is five edges about five nodes for one anonymous request, and one of them
names the author of a tweet nobody has fetched. Every row also carries the tier
and surface it cost, so a graph assembled from mixed reads can be filtered by how
much you trust each claim. `--conflicts` narrows the output to claims two sources
cannot both be right about, printing both sides with a marker on the one that
wins on provenance instead of quietly picking a side.

Those same five claims plus the five nodes they address come back as one value
from `x graph`, which is the shape to hand to something that wants a graph
rather than a table:

```bash
x graph 1903142823316049977 -o json
```

Nodes the read carried whole come with their record, the tweet and its author
here, and nodes that were only named come with just an address. That is the
honest shape: a mention is a claim about an account nobody fetched, and dropping
it would lose four fifths of the graph a single request just paid for.

A hop is not an edge, and the difference matters once you start walking. An edge
is a claim, pointing the way the claim points. A hop is a direction of travel,
and half of them run against the arrow: the `liker` hop goes from a tweet out to
the accounts that liked it, while the edge underneath points from each account
back at the tweet.

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

or a comma-separated list of individual hops:

```bash
x discover <ref> --follow author,quote,mention
x discover <ref> --follow replies,liker
```

The full hop vocabulary:

| Hop | From → to | Tier | What it follows |
|---|---|---|---|
| `author` | tweet → user | 0 | who wrote the tweet |
| `quote` | tweet → tweet | 0 | the tweet it quotes |
| `retweet` | tweet → tweet | 0 | the original it retweets |
| `reply` | tweet → tweet | 0 | the tweet it replies to |
| `mention` | tweet → user | 0 | accounts it @-mentions |
| `pinned` | user → tweet | 0 | the account's pinned tweet |
| `timeline` | user → tweet | 0 | the account's recent tweets |
| `replies` | tweet → tweet | 0 | the replies the status page renders |
| `liker` | tweet → user | session | accounts that liked it |
| `retweeter` | tweet → user | session | accounts that retweeted it |
| `quotedby` | tweet → tweet | session | tweets that quote it |
| `following` | user → user | session | accounts it follows |
| `followers` | user → user | session | accounts that follow it |
| `likes` | user → tweet | session | tweets it liked |

The Tier-0 hops work with nothing. The rest read the GraphQL surface, which X
answers for your own session (`x auth import`) and not for a guest token. When
you ask for a hop you have no tier for, `x discover` drops it with a one-line
note on stderr and keeps going on what it can reach, rather than failing the
whole walk. The one exception is when *every* hop you asked for needs a tier:
then there is nothing to do and it exits `4` with the tier to add.

## How far and how wide

```bash
x discover <ref> --depth 2            # follow two hops from the seed (default 1)
x discover <ref> --fanout 50          # up to 50 neighbors per hop (default 25)
x discover <ref> --fanout 0           # no per-hop cap
x discover <ref> -n 1000              # stop after 1000 nodes total (default 500)
x discover <ref> --budget 20          # stop after 20 upstream requests
```

`--depth` is how many hops to follow. `--fanout` caps how many neighbors each
hop contributes per node, so one hop never pages a whole follower graph unless
you raise it. `-n/--limit` is the total node budget, the hard stop on a deep or
wide walk.

`--budget` is the other kind of ceiling: requests rather than nodes. Nodes are
not all the same price, since a list read hands back whole records and costs
nothing extra to visit, while a mention costs a fetch. Requests are the unit the
rate limits are written in, so that is the unit a careful walk counts:

```text
$ x discover 1903142823316049977 --follow thread --depth 2 --budget 2 -o url
https://x.com/GuyFisherMoney/status/1903142823316049977
https://x.com/GuyFisherMoney
stopped early (request budget of 2 spent): 4 nodes left unexpanded
```

The last line goes to stderr, and it is the point of the flag: a walk that
stopped short says what it left behind, so a partial crawl is never mistaken for
a finished one. An exhausted rate-limit window ends a walk the same way, with
exit `5` and the bucket named.

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
x discover <user> --follow network -o jsonl | jq -r '.user.username' | sort -u
x discover <ref> --fields depth,via,who,url -o table
```

## Persisting a walk

Add `--store` to write every node and edge into the local store as the walk
streams, so you keep the graph as well as see it:

```bash
x discover nasa --follow network --depth 2 --store
x db query "select predicate, count(*) from edges group by 1 order by 2 desc"
```

What lands in `edges` is claims, not hops. Most of them come out of the records
themselves, and four come out of the walk because nothing else asserts them:
`liker`, `retweeter`, `following`, and `followers` are listings rather than
records, so a likers page is the only place that says an account liked a tweet.

When you want the dataset rather than the live answer, reach for
[`x crawl`](/guides/local-store/), which is the same walk pointed at the store
instead of stdout. See [the local store](/guides/local-store/) for inspecting
and exporting what you collect.
