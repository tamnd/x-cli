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
| `tweet <ref>` | Show a single tweet | (Tier 0) |
| `user <user>` | Show a profile | `--id` |
| `timeline <user>` | A user's tweets (recent window; deeper with `--guest`/session) | `--id`, `--replies`, `--media` |
| `replies <user>` | A user's tweets including replies (session) | `--id` |
| `media <user>` | Media attached to a user's tweets (session) | `--id` |
| `thread <ref>` | A conversation thread around a tweet (session) | |
| `poll <ref>` | A tweet's poll options and tallies | |
| `search <query>` | Search tweets (needs `--guest` or session) | `--product` |
| `counts <query>` | Per-day tweet counts for a search | `--product` |
| `quotes <ref>` | Quote tweets of a tweet (search-backed) | |
| `mentions <user>` | Tweets mentioning a user (search-backed) | |
| `followers <user>` | Accounts following a user (needs `--guest` or session) | `--id` |
| `following <user>` | Accounts a user follows (needs `--guest` or session) | `--id` |
| `likers <ref>` | Accounts that liked a tweet (needs `--guest` or session) | |
| `retweeters <ref>` | Accounts that retweeted a tweet (needs `--guest` or session) | |
| `likes <user>` | Tweets a user has liked (needs `--guest` or session) | `--id` |
| `list <list-id>` | Tweets in an X List (needs `--guest` or session) | |
| `home` | Your reverse-chron home timeline (session) | |
| `bookmarks` | Your bookmarks (session) | |

`search --product` takes `Top|Latest|People|Photos|Videos` (default `Latest`).
`counts --product` takes `Top|Latest`. `timeline --replies` includes replies and
`--media` keeps only tweets with media.

x is read-only: there are no commands that post, like, follow, or otherwise
change your account. `likes`, `likers`, `followers`, and `bookmarks` only read.

## Local store

| Command | What it does | Key flags |
|---|---|---|
| `discover <seed>...` | Breadth-first walk of the graph linked from a tweet or user (aliases `walk`, `graph`) | `--follow`, `--depth`, `--fanout`, `--store`, `-n` |
| `crawl <seed>...` | The same walk, persisted into the local store | `--follow`, `--depth`, `--fanout`, `--max` |
| `db stats` | Row counts per table | |
| `db query <sql>` | Run a read-only SQL query | |
| `queue` | Show the crawl queue | |
| `queue clear` | Empty the crawl queue | |
| `export <user> <out-dir>` | Render a stored user's tweets as Markdown | |

`discover` and `crawl` share the same walk: `--follow` is a preset (`content`,
`thread`, `engagement`, `network`, `timeline`, `all`) or a comma-separated edge
list, `--depth` is how many hops to follow (default `1`), and `--fanout` caps
neighbors per edge (default `25`). `discover` streams nodes and stops at `-n`
(default `500`); add `--store` to also persist them. `crawl` always persists and
stops at `--max` (default `200`). The store is a fixed `x.db` under `--data-dir`.
Engagement and network edges need `--guest` or a session. See
[graph discovery](/guides/graph-discovery/).

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
| `serve` | Serve the operations over HTTP (NDJSON) | `--addr` |
| `mcp` | Run as an MCP server over stdio | |
| `version` | Print version info | |
| `completion <shell>` | Generate a shell completion script | |

`serve` exposes every read over HTTP as NDJSON, and `mcp` exposes the same set
as MCP tools for an agent. Both reuse the command surface above with no extra
configuration.

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
| `--tier` | Force a tier: `syndication\|guest\|session` |
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
