---
title: "Search and discovery"
description: "Search tweets by query and product, count tweets per day, and list followers, following, likers, retweeters, and likes."
weight: 20
---

Search and the people-listing commands all reach into the GraphQL surface, and
as of July 2026 X answers every one of them only for your own session. A guest
token is not enough. This guide covers finding tweets and finding accounts, and
assumes you have run `x auth import` once.

## Search

```bash
x search "from:nasa filter:images"
```

The query is X's own search syntax: `from:`, `to:`, `#hashtag`, `filter:`,
`since:`/`until:`, quoted phrases, and the rest. Choose what kind of results you
want with `--product`:

```bash
x search "webb telescope" --product Top
x search "webb telescope" --product Latest   # default
x search "webb telescope" --product People   # accounts, not tweets
x search "webb telescope" --product Photos
x search "webb telescope" --product Videos
```

Search is denied to guest tokens outright: under `--guest` the request comes
back not-found however well-formed it is. Run it under your session (see
[your account](/guides/your-account/)).

## Counts

```bash
x counts "webb telescope"
```

`x counts` buckets matching tweets per day, client-side, and prints a count for
each day. `--product` takes `Top` or `Latest`. It is a quick way to see when a
topic spiked without paging every tweet.

## Followers and following

```bash
x followers nasa          # accounts following a user
x following nasa          # accounts a user follows
```

Both need your session. They page through the social graph and
return one account per row, so they shape and pipe like any other list:

```bash
x following nasa --fields username,name -o csv
```

## Likers, retweeters, and likes

```bash
x likers <ref>            # accounts that liked a tweet
x retweeters <ref>        # accounts that retweeted a tweet
x likes nasa              # tweets a user has liked
```

`likers` and `retweeters` take a tweet ref and return accounts. `likes` takes a
user and returns the tweets they liked. All three need a session.

To chain these into one another, hop by hop, from a starting tweet or account
rather than running them one at a time, see [graph discovery](/guides/graph-discovery/).

## Lists

```bash
x list <list-id>          # tweets in an X List
```

`x list` reads the timeline of a public X List by its numeric id. It needs your
session, even though the list is public.

## Session versus guest

A guest token buys two operations: the profile read and the deeper timeline
walk. Measured on 2026-07-28, every other GraphQL call on this page came back
404 with an empty body under `--guest`, which is how X says no to a credential
it does not accept. So `--guest` is worth adding to `x timeline` and worth
nothing here.

Your own session (`x auth import`) is what these need. When a command needs more
than you have enabled, x exits with code `4` and tells you which tier to add.
