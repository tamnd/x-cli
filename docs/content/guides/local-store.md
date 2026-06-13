---
title: "The local store"
description: "Persist reads to SQLite with --db, crawl accounts breadth-first, inspect with db stats and db query, manage the queue, and export to Markdown."
weight: 50
---

x can keep a local copy of everything it reads. Point any read at a SQLite file
with `--db` and the tweets and accounts it returns are persisted as a side
effect, so a read doubles as a crawl. `x crawl` automates that into a
breadth-first walk, and a few commands inspect and export the result.

## Persist as you read

```bash
x timeline nasa --guest --db x.db -n 200
x user nasa --db x.db
x followers nasa --guest --db x.db
```

The command does exactly what it normally does (prints rows), and on the way it
upserts the entities into `x.db`. Run more reads against the same file and the
store grows. Nothing is lost between runs; the database is the accumulated state.

## Crawl

```bash
x crawl nasa --db x.db --depth 1 --max 200
x crawl nasa jack --db x.db --depth 2 --max 1000
```

`x crawl` takes one or more seed users and walks outward breadth-first, storing
tweets and the accounts it discovers. `--depth` is how many mention-hops to
follow from the seeds (default `1`). `--max` stops the crawl after that many
stored tweets (default `200`). Add `--guest` or a session so the crawl can page
past the syndication window.

## The queue

A crawl keeps a work queue of accounts it still has to visit, in the store.

```bash
x queue              # show what is still queued
x queue clear        # empty the queue
```

Clearing the queue lets you start a fresh crawl against the same database
without re-walking what is pending.

## Inspect the store

```bash
x db stats                 # row counts per table
x db query "select username, count(*) from tweets group by username"
```

`x db stats` is the quick health check: how many tweets, accounts, and so on you
have stored. `x db query` runs read-only SQL against the store, so you can slice
the data any way you like and shape the result with `-o`, `--fields`, or
`--template` just like a live read.

## Export to Markdown

```bash
x export nasa ./out
```

`x export` renders a stored user's tweets as Markdown files under the output
directory. It reads only from the local store, so crawl or persist the user
first, then export. The result is a plain, readable archive you can keep,
search, or publish.
