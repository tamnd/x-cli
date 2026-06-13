---
title: "Search and discovery"
description: "Search tweets by query and product, count tweets per day, and list followers, following, likers, retweeters, and likes."
weight: 20
---

Search and the people-listing commands all reach into the GraphQL surface, so
they need at least a guest token (`--guest`) and some need your own session.
This guide covers finding tweets and finding accounts.

## Search

```bash
x search "from:nasa filter:images" --guest
```

The query is X's own search syntax: `from:`, `to:`, `#hashtag`, `filter:`,
`since:`/`until:`, quoted phrases, and the rest. Choose what kind of results you
want with `--product`:

```bash
x search "webb telescope" --product Top --guest
x search "webb telescope" --product Latest --guest   # default
x search "webb telescope" --product People --guest   # accounts, not tweets
x search "webb telescope" --product Photos --guest
x search "webb telescope" --product Videos --guest
```

Search is denied to guest tokens by X on some accounts and windows, so if
`--guest` returns nothing, run it under your session instead (see
[your account](/guides/your-account/)).

## Counts

```bash
x counts "webb telescope" --guest
```

`x counts` buckets matching tweets per day, client-side, and prints a count for
each day. `--product` takes `Top` or `Latest`. It is a quick way to see when a
topic spiked without paging every tweet.

## Followers and following

```bash
x followers nasa --guest          # accounts following a user
x following nasa --guest          # accounts a user follows
```

Both need a guest token or a session. They page through the social graph and
return one account per row, so they shape and pipe like any other list:

```bash
x following nasa --guest --fields username,name -o csv
```

## Likers, retweeters, and likes

```bash
x likers <ref> --guest            # accounts that liked a tweet
x retweeters <ref> --guest        # accounts that retweeted a tweet
x likes nasa --guest              # tweets a user has liked
```

`likers` and `retweeters` take a tweet ref and return accounts. `likes` takes a
user and returns the tweets they liked. All three need a guest token or a
session.

## Lists

```bash
x list <list-id> --guest          # tweets in an X List
```

`x list` reads the timeline of a public X List by its numeric id. It needs a
guest token or your session.

## Session versus guest

A guest token (`--guest`) is enough for search, counts, followers, following,
likers, retweeters, likes, and lists in the common case. Your own session
(`x auth import`) is more reliable for search and is required wherever X denies
the guest token. When a command needs more than you have enabled, x exits with
code `4` and tells you which tier to add.
