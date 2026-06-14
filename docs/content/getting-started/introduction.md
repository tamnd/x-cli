---
title: "Introduction"
description: "What X's free surfaces are, the three tiers x reads them through, and the output model."
weight: 10
---

![x reading a tweet and a timeline into colored tables, then through jq](https://x-cli.tamnd.com/demo.gif)

X has a paid API with developer apps, keys, and quotas. x does not touch it.
Instead it reads the same free, public surfaces the website and the embed
widgets already use: the syndication endpoints behind tweet embeds, and the
web-client GraphQL the site itself calls. It picks the cheapest surface that can
answer your question and shapes the result for you.

x is for one person reading X for their own use. It is not a scraper farm, a
firehose, or a way around X's rules. It behaves like a careful browser: one
request at a time, with a real delay between them, and it caches what it can.

## The three tiers

x auto-selects the cheapest tier that can serve a command, so you usually do not
think about them. They differ in what they can reach and what they cost you.

- **Tier 0, syndication.** No auth, nothing to set up. This covers single
  tweets, profiles, and a recent window of a user's timeline. It is the default
  and it is enough for a lot of day-to-day lookups.
- **Tier 1, guest GraphQL.** Opt in with `--guest`. x mints a guest token, which
  lets it page deeper into timelines and reach followers, following, likers,
  retweeters, likes, lists, and search. The token is cached on disk between runs
  so repeated commands do not re-mint it and trip X's rate limits. A few
  endpoints (replies, media, thread, search, followers) are denied to guest
  tokens by X and need your own session instead.
- **Tier 2, session GraphQL.** Your own browser cookies, imported once with
  `x auth import`. This reaches the reads X reserves for a logged-in client:
  search, followers and following, your home timeline, and your bookmarks. x is
  read-only, so the session is only ever used to fetch data, never to act.

`x info` prints the tiers it has available and what each can do right now.
Force a specific one with `--tier syndication|guest|session` when you want to
be explicit.

## A browser-faithful client

To make its GraphQL reads look like the web client, x sends an
`x-client-transaction-id` header and a browser-faithful identity on each
request. Together with the cached guest token, this is what keeps repeated
invocations from looking like a bot and getting rate-limited.

## No paid API

There is no API key, no developer app, and no quota to manage. Reads cost
nothing. Some reads need your own session because X reserves them for a
logged-in client, not because they cost money. If a command needs a tier you
have not enabled, x tells you which one and exits with a specific code (see
[troubleshooting](/reference/troubleshooting/)).

## IDs are strings

Tweet and user IDs on X are 64-bit snowflakes that lose precision if a tool
treats them as numbers. x always renders IDs as strings, so they round-trip
safely through JSON and `jq`. The real tweet with id `20` ("just setting up my
twttr") stays `"20"`, and a 19-digit id stays exactly itself.

## The output model

Every read produces rows. On a terminal you get a readable list of sections;
piped into another program you get JSONL by default. Switch formats with `-o`
(`list`, `table`, `jsonl`, `json`, `csv`, `tsv`, `markdown`, `url`, `raw`),
project columns with `--fields`, or render each row with a Go template via
`--template`. The same row shape feeds all of them. See
[output formats](/reference/output/) for the details.

Next: [install it](/getting-started/installation/), then take the
[quick start](/getting-started/quick-start/).
