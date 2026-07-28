---
title: "CLI"
description: "Every command and subcommand, grouped, with the flags that matter, and the global flags."
weight: 10
---

```
x <command> [subcommand] [flags]
```

Run `x <command> --help` for the full flag list on any command. This page is the
map. A `<ref>` is a tweet id, status URL, or anything x can resolve to a tweet; a
`<user>` is a handle, or a numeric id with `--id`.

## Reads

| Command | What it does | Key flags |
|---|---|---|
| `get <ref>...` | Read whatever each reference points at, with the command that fits | |
| `tweet <ref>` | Show a single tweet | (Tier 0) |
| `user <user>` | Show a profile | `--id` |
| `timeline <user>` | A user's tweets (recent window; deeper with `--guest`/session) | `--id`, `--replies`, `--media` |
| `replies <ref>` | Replies to a tweet, or a user's own replies | `--id` |
| `media <ref>` | The media on a tweet, or on a user's tweets | `--id`, `--tab`, `--download`, `--size`, `--variant` |
| `embed <ref>` | Print a tweet's oEmbed blockquote, verbatim | |
| `thread <ref>` | A conversation thread around a tweet | (Tier 0) |
| `poll <ref>` | A tweet's poll options and tallies | |
| `search <query>` | Search tweets (session) | `--product` |
| `counts <query>` | Per-day tweet counts for a search (session) | `--product` |
| `quotes <ref>` | Quote tweets of a tweet (search-backed, session) | |
| `mentions <user>` | Tweets mentioning a user (search-backed, session) | |
| `followers <user>` | Accounts following a user (session) | `--id` |
| `following <user>` | Accounts a user follows (session) | `--id` |
| `likers <ref>` | Accounts that liked a tweet (session) | |
| `retweeters <ref>` | Accounts that retweeted a tweet (session) | |
| `likes <user>` | Tweets a user has liked (session) | `--id` |
| `list <list-id>` | Tweets in an X List (session) | |
| `space <ref>` | An audio Space: hosts, speakers, times, and audience (`--guest`) | |
| `home` | Your reverse-chron home timeline (session) | |
| `bookmarks` | Your bookmarks (session) | |
| `trends [woeid]` | What is trending in a place, worldwide by default | |
| `places [query]` | The places X has trends for, and their woeids | `--country`, `--type` |

`search --product` takes `Top|Latest|People|Photos|Videos` (default `Latest`).
`counts --product` takes `Top|Latest`. `timeline --replies` includes replies and
`--media` keeps only tweets with media. `--replies` stays on Tier 0 even with
`--guest`, because the guest tier answers that read with an empty timeline
rather than refusing it; only a session pages it deeper.

`space` takes the id or the `x.com/i/spaces/` link and reads the whole record:
who created it, the admins and the speakers, when it was scheduled, started and
ended, and how many people heard it live or played the replay. The table
summarises the rosters as counts, and `-o json` has the participants themselves,
each with the numeric account id you need to look them up. It is one of the five
reads `--guest` is worth passing for.

`get` classifies each argument the way `x classify` does and dispatches: a tweet
id or status link reads the tweet, a handle reads the profile, a hashtag or a
search link runs the search, a list link reads the list. A kind it has no reader
for exits 7 rather than guessing. `media --size` takes
`thumb|small|medium|large|orig` (default `orig`) and `--variant` names a video
rendition by resolution or bitrate (default: the highest-bitrate MP4). With
`--download`, `media` writes files and prints their paths, so the record flags
(`-o`, `--fields`, `--template`) do not apply.

`trends` takes a woeid, which is the Yahoo! Where On Earth id X still keys its
trend lists by, and it also takes a place name: `x trends tokyo` works, and an
ambiguous name comes back as a usage error listing the candidates rather than a
pick. `places` is how you find a woeid; it caches the directory for a week, so
the name lookup costs one request ever. Both are Tier 0.

x is read-only: there are no commands that post, like, follow, or otherwise
change your account. `likes`, `likers`, `followers`, and `bookmarks` only read.

## Local store

| Command | What it does | Key flags |
|---|---|---|
| `edges <ref>...` | The graph claims one record makes, without walking anywhere | `--conflicts` |
| `graph <ref>...` | Those claims and the nodes they address, as one document | |
| `rdf <ref>...` | The same graph as RDF, in schema.org's vocabulary | `--format`, `--provenance` |
| `discover <seed>...` | Breadth-first walk of the graph linked from a tweet or user (alias `walk`) | `--follow`, `--depth`, `--fanout`, `--budget`, `--store`, `-n` |
| `crawl <seed>...` | The same walk, persisted into the local store | `--follow`, `--depth`, `--fanout`, `--budget`, `--max` |
| `db stats` | Row counts per table | |
| `db query <sql>` | Run a read-only SQL query | |
| `query <sql>` | The same query, one word shallower | |
| `queue` | Show the crawl queue | |
| `queue clear` | Empty the crawl queue | |
| `export [<user> <out-dir>]` | Render a stored user's tweets as Markdown, or the whole store as RDF | `--format`, `--kind`, `--since`, `--provenance` |

`edges` is one read per reference and no walking at all. It prints the claims a
record already makes about other nodes, as `from predicate to` with the URL it
was read from and the tier that cost. A tweet read with no credential is five or
six edges for a single request, which is what makes it the cheap way to see what
a surface is worth before spending a budget on a crawl. `--conflicts` narrows the
output to claims two sources cannot both be right about, and prints both sides
with a marker on the one that wins on provenance rather than picking for you.

`graph` is the same read printed as one value instead of one line per claim: the
edges, plus every node those edges address. Nodes the read carried whole come
with their record, and nodes that were only named come with just an address,
because a mention is a claim about an account nobody fetched. It is a document,
so `-o json` is the format it is for; on a terminal you get its shape summarized
and reach for `x edges` or `x get` to read the parts.

```console
$ x graph 1903142823316049977 -o table
 GRAPH                          NODES  READ  EDGES  PREDICATES
 x://tweet/1903142823316049977  5      2     5      authored mentions replies_to
```

`rdf` is `graph` said in somebody else's vocabulary: schema.org wherever a term
exists, and `x:` where none does. That is not a taste call. X publishes
schema.org microdata on its own status and profile pages, so a tweet already has
a vendor-blessed RDF shape and this tool agrees with it rather than inventing a
parallel one, which is what lets the output be checked against the source. Every
`x:` term is defined at [the namespace URL](/ns/).

```console
$ x rdf 20 --format ttl | head -8
@prefix rdf:    <http://www.w3.org/1999/02/22-rdf-syntax-ns#> .
@prefix schema: <https://schema.org/> .
@prefix x:      <https://x-cli.tamnd.com/ns#> .
@prefix xsd:    <http://www.w3.org/2001/XMLSchema#> .

<x://tweet/20>
  a schema:SocialMediaPosting ;
  schema:identifier "20" ;
```

`--format` is `nt`, `ttl`, `jsonld`, or `nq` (default `ttl`). `nq` carries the
URL each claim was read from as the graph name, and `jsonld` as one named graph
per source, which is how provenance survives a merge of two crawls. `nt` and
`ttl` have nowhere to put it, so `--provenance` adds reified statements there; it
is off by default because reification costs five lines per claim and outnumbers
the data. `rdf` writes bytes rather than records, so `-o` and `--template` do not
apply to it.

`discover` and `crawl` share the same walk: `--follow` is a preset (`content`,
`thread`, `engagement`, `network`, `timeline`, `all`) or a comma-separated hop
list, `--depth` is how many hops to follow (default `1`), and `--fanout` caps
neighbors per hop (default `25`). `discover` streams nodes and stops at `-n`
(default `500`); add `--store` to also persist them. `crawl` always persists and
stops at `--max` (default `200`). The store is a fixed `x.db` under `--data-dir`.
Engagement and network hops need a session.

`query` is `db query` without the `db`: SQL against the store a crawl filled in,
never the network. The tables are `nodes` and `edges`; see
[the local store](/guides/local-store/).

`--budget` caps what the walk spends upstream, counted in requests rather than
nodes, because requests are the unit the rate limits are written in. A walk that
stops early says so and names how many nodes it never expanded, on stderr, so a
partial crawl is never mistaken for a finished one. An exhausted rate-limit
window ends the walk the same way, with exit `5` and the bucket named: an empty
window is not one node's problem, it is every node's. See
[graph discovery](/guides/graph-discovery/).

A hop is not an edge. A hop is a direction the walk travels and an edge is a
claim a record makes, and half the hops run against the arrow: the `liker` hop
goes from a tweet to the accounts that liked it, and the edge under it points
from each account back to the tweet.

`export` writes what the store already holds and never makes a request. With
`--format` it writes the whole store as RDF on stdout, the same vocabulary and
the same four serializations as `x rdf`, so a crawl becomes a file you can load
into a triple store. `--kind` keeps the records of one kind and every claim with
one of them at either end, which is why `--kind tweet` still tells you who wrote
them. `--since` is when a record was captured rather than when a tweet was
posted: the question an export answers is what you have learned lately, and a
2006 tweet read this morning was learned this morning. Without `--format` it
renders one account's stored tweets as Markdown, which is what the two arguments
are for.

## Meta

| Command | What it does | Key flags |
|---|---|---|
| `auth import` | Save your `auth_token` + `ct0` (or paste a Cookie header on stdin) | `--auth-token`, `--ct0` |
| `auth status` | Show the current session and tier | |
| `auth logout` | Forget the saved session | |
| `cache clear` | Delete all cached responses | |
| `config path` | Print the config file path | |
| `config show` | Print the resolved configuration | |
| `download <ref>` | Download a tweet's media to disk | `-O`/`--out` |
| `open <ref>` | Open a tweet or profile in your browser | |
| `info` | Show resolved tiers and capabilities | |
| `capture <ref>...` | Record what a surface says now, into a fixture file | `--out` |
| `serve` | Serve the operations over HTTP (NDJSON) | `--addr` |
| `mcp` | Run as an MCP server over stdio | |
| `version` | Print version info | |
| `completion <shell>` | Generate a shell completion script | |

`capture` is how `x/testdata` gets refreshed, and why no fixture in this repo is
hand-written. It asks every surface that serves a reference, sends the reader's
own request down to the headers, and writes each answer gzipped under the name
the tests already load. A surface that will not answer gets a row saying so
rather than aborting the run, so a rate limit on one of them does not cost you
the two that worked. `x capture 20 jack` rewrites five fixtures.

`serve` exposes the reads over HTTP as NDJSON, one route each under `/v1/`, and
`mcp` exposes the same set as MCP tools for an agent. Both take the global flags,
so `x serve --guest` and `x mcp --tier session` serve at that tier and nothing
else needs configuring. `x mcp` says how many tools it registered on stderr
before it blocks, because a server waiting for JSON-RPC on stdin is otherwise
indistinguishable from a hang.

They carry the reads and not the whole command line: 24 of the commands above,
which is everything that takes a reference and answers with records. `get`,
`classify`, `crawl`, `discover`, `export`, `rdf`, `db`, `auth`, and the rest of
the local-store and session commands stay on the command line, because a walk
that writes to your disk and a command that saves your cookies are not things to
hand a network port.

## Global flags

These apply to every command. See [configuration](/reference/configuration/)
for defaults and [output formats](/reference/output/) for what `-o` produces.

| Flag | Meaning |
|---|---|
| `-o, --output` | Output format: `list\|table\|jsonl\|json\|csv\|tsv\|markdown\|url\|raw` (default auto: `list` on a terminal, `jsonl` when piped) |
| `--fields` | Comma-separated columns to project |
| `--template` | Go text/template rendered per row |
| `-n, --limit` | Maximum rows (`0` means unlimited) |
| `--no-header` | Omit the header row |
| `--color` | `auto\|always\|never` (default auto) |
| `--guest` | Enable the opt-in free guest-GraphQL tier |
| `--tier` | Cap the tier (`0\|1\|2`) or pin one surface (`syndication\|oembed\|web\|guest\|session`) |
| `--db` | Generic record sink provided by the framework; x's own typed store lives under `--data-dir`, not here |
| `--data-dir` | Cache and store root |
| `--query-id` | Override a GraphQL query id (`Op=hash`) |
| `--rate` | Minimum delay between requests (default `1s`) |
| `--retries` | Retries on 429/5xx (default `3`) |
| `--timeout` | Per-request timeout (default `30s`) |
| `--no-cache` | Bypass the HTTP cache |
| `--dry-run` | Print the target instead of acting (e.g. `open` prints the URL) |
| `-q, --quiet` | Suppress progress on stderr |
| `-v, --verbose` | Show tier, endpoint, and timing |
| `-h, --help` | Help for a command |
| `--version` | Print the version |
