---
title: "x"
description: "A fast, friendly, read-only command line for X (Twitter). Read tweets, profiles, timelines, search, and followers from X's free public surfaces and crawl it all into a local store, from one pure-Go binary."
heroTitle: "X, from the command line"
heroLead: "x is a single pure-Go binary that reads X's free public surfaces (syndication and the web-client GraphQL) for your own use. Show a tweet, follow a timeline, search, list followers, and persist everything to a local SQLite store as you go. Strictly read-only. No paid API, no developer key, nothing to sign up for."
heroPrimaryURL: "/getting-started/quick-start/"
heroPrimaryText: "Get started"
---

Pulling data out of X usually means a paid API plan, a developer app, and a pile
of OAuth. x skips all of it. It reads the same free endpoints the website and
the embed widgets use, picks the cheapest one that can answer your question, and
shapes the result into output that pipes.

```bash
x tweet 20                          # show a single tweet
x user nasa                         # a profile, with metrics
x timeline nasa --guest -n 50       # deeper timeline via the guest tier
x search "from:nasa filter:images" -o jsonl | jq .id
x followers nasa -n 100 -o csv       # who follows an account
```

It speaks to X over plain HTTPS and auto-selects the cheapest of three free
tiers. Tier 0 (syndication) needs no auth at all. Tier 1 mints a guest token on
demand with `--guest`. Tier 2 uses your own browser cookies, imported once, to
unlock search, followers, your home timeline, and bookmarks. It is strictly
read-only, and the binary is pure Go with no runtime dependencies.

## What you can do with it

- **Read tweets and profiles.** Show a single tweet, a user's timeline, replies,
  media, a conversation thread, or a poll's tallies, mostly with no auth.
- **Search and discover.** Search tweets by query and product, count tweets per
  day, and list followers, following, likers, retweeters, and a user's likes.
- **Use your own session.** Import your session once to unlock the reads X gates
  behind a logged-in client: full search, followers and following, your home
  timeline, and bookmarks. Your session is only ever used to read.
- **Shape the output.** Render as a table, JSONL, JSON, CSV, TSV, plain URLs, or
  a Go template, project columns with `--fields`, and pipe to `jq`. IDs are
  always strings, so snowflake precision survives.
- **Build a local store.** Add `--db` to any read and it persists entities to
  SQLite as a side effect, so a read doubles as a crawl. `x crawl` walks
  accounts breadth-first, and `x export` renders a stored user as Markdown.

## Where to go next

- New here? Start with the [introduction](/getting-started/introduction/) for the
  three-tier mental model, then the [quick start](/getting-started/quick-start/).
- Want to install it? See [installation](/getting-started/installation/).
- Looking for a specific task? The [guides](/guides/) cover reading tweets,
  search and discovery, your session, output and pipelines, and the local store.
- Need every flag? The [CLI reference](/reference/cli/) is the full surface.
