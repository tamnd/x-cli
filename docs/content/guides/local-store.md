---
title: "The local store"
description: "Keep a local SQLite copy of the graph: crawl breadth-first, persist a discover walk with --store, inspect with db stats and db query, manage the queue, and export to Markdown."
weight: 50
---

x can keep a local copy of the graph it walks. The store is a single SQLite file
at `x.db` under the data dir; move it by pointing `--data-dir` somewhere else.
`x crawl` fills it with a breadth-first walk, `x discover --store` tees a live
walk into it, and a few commands inspect and export the result.

## Persist a walk

`x discover --store` writes every node and edge it streams into the store as a
side effect, so a live walk doubles as a crawl:

```bash
x discover nasa --follow network --depth 2 --store
x discover 1234567890 --follow all --store
```

See [graph discovery](/guides/graph-discovery/) for the full hop and preset
vocabulary. Plain reads (`x timeline`, `x user`, and the rest) do not write to
the store; `--store` and `x crawl` are what fill it.

## Crawl

```bash
x crawl nasa --depth 1 --max 200
x crawl nasa jack --depth 2 --max 1000
x crawl 1234567890 --follow thread --depth 2
```

`x crawl` is `x discover` pointed at the store instead of stdout. It takes one or
more seeds (tweets or users), walks the graph breadth-first, and writes every
node and edge it reaches, marking the frontier in the queue as it goes. The
`--follow`, `--depth`, and `--fanout` knobs are the same as discover; `--max`
stops after that many stored nodes and `--budget` after that many upstream
requests. A crawl that stops on either says how many nodes it left unexpanded. The engagement and network
hops need your session; `--guest` only pages past the syndication window.

## The queue

A crawl keeps a work queue of nodes it still has to visit, in the store.

```bash
x queue              # show what is still queued
x queue clear        # empty the queue
```

Clearing the queue lets you start a fresh crawl against the same database
without re-walking what is pending.

## What is in it

Two tables carry the graph, and the typed `tweets`, `users`, and `media` tables
sit beside them for the reads that want columns rather than triples.

`nodes` is one row per addressed thing: `uri`, `kind`, `id`, `tier`, the whole
record as JSON, and when it was captured. A revisit keeps the higher-tier
record, so a crawl that later reads a tweet with a session does not lose it to
the next anonymous pass.

`edges` is one row per claim: `from_uri`, `predicate`, `to_uri`, `source`,
`tier`, `captured`. The source is part of the primary key, so two surfaces
asserting the same thing are two rows. That is deliberate, and it is what makes
agreement and disagreement queryable rather than silently collapsed:

```bash
x db query "select from_uri, predicate, to_uri, source from edges
            where from_uri = 'x://user/jack' and predicate = 'authored'"
```

## Inspect the store

```bash
x db stats                 # row counts per table
x db query "select predicate, count(*) from edges group by 1 order by 2 desc"
x db query "select username, count(*) from tweets group by username"
```

`x db stats` is the quick health check: how many tweets, accounts, and so on you
have stored. `x db query` runs read-only SQL against the store, so you can slice
the data any way you like and shape the result with `-o`, `--fields`, or
`--template` just like a live read. `x query` is the same command a word
shallower, since asking the graph a question is the point of having one:

```console
$ x query "select predicate, count(*) n from edges group by predicate order by n desc" -o table
 PREDICATE   N
 authored    4
 mentions    3
 replies_to  2
```

Neither one touches the network. Crawl once and the graph is yours.

## Export to Markdown

```bash
x export nasa ./out
```

`x export` renders a stored user's tweets as Markdown files under the output
directory. It reads only from the local store, so crawl or persist the user
first, then export. The result is a plain, readable archive you can keep,
search, or publish.
