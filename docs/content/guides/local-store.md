---
title: "The local store"
description: "Keep a local SQLite copy of the graph: crawl breadth-first, persist a discover walk with --store, inspect with db stats and db query, manage the queue, and export to Markdown or RDF."
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

## Export the graph as RDF

```bash
x export --format nq > nasa.nq
x export --format ttl --kind tweet --since 2026-07-01
```

`x export --format` walks the whole store into RDF, in the same schema.org
vocabulary `x rdf` writes and the same four serializations. There is nothing to
point it at: the store is the one at `x.db` under the data dir, so
`--data-dir ./crawl-a` is how you export one crawl rather than another. Nothing
here goes back to the network, so the crawl you paid for once is a graph
forever, and `nasa.nq` loads into a triple store beside data that has nothing to
do with X.

```console
$ x export --format ttl | head -12
@prefix rdf:    <http://www.w3.org/1999/02/22-rdf-syntax-ns#> .
@prefix schema: <https://schema.org/> .
@prefix x:      <https://x-cli.tamnd.com/ns#> .
@prefix xsd:    <http://www.w3.org/2001/XMLSchema#> .

<x://tweet/1903136743634723031>
  a schema:SocialMediaPosting ;
  schema:identifier "1903136743634723031" ;
  schema:articleBody "@jack This is 2025, if you see this in 2040 bill me for anything i will pay. Pretty sure i will be rich" ;
  schema:datePublished "2025-03-21T17:28:52Z"^^xsd:dateTime ;
  schema:inLanguage "en" ;
  schema:url <https://x.com/marmoushEra/status/1903136743634723031> ;
```

`--kind` keeps the records of one kind and every claim with one of them at
either end. Filtering on the subject alone would read better right up until you
noticed it had dropped authorship, which runs from the account to the post.

`--since` is when a record was captured, not when a tweet was posted. It is
stored alongside the record, and it is the useful one: an export is how you ask
what you have learned lately, and a 2006 tweet you read this morning is
something you learned this morning.

`nq` and `jsonld` carry the URL each claim came from, so a merge of two crawls
keeps knowing which read said what. `nt` and `ttl` have nowhere to put it and
take `--provenance`, which reifies every statement and costs about five lines
per claim.

## Export to Markdown

```bash
x export nasa ./out
```

With no `--format`, `x export` renders a stored user's tweets as Markdown files
under the output directory. It reads only from the local store, so crawl or
persist the user first, then export. The result is a plain, readable archive you
can keep, search, or publish.
