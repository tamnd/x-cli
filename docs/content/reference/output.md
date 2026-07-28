---
title: "Output formats"
description: "Every -o format defined precisely, the --fields projection, --template semantics, and the exit codes."
weight: 20
---

x turns each command's result into rows and renders them through one of several
formats. This page defines each format, the `--fields` and `--template`
modifiers, and the exit codes.

## Choosing a format

`-o` (or `--output`) selects the format. With no `-o`, x picks `list` when the
output is a terminal and `jsonl` when it is a pipe or file.

| Format | What it produces |
|---|---|
| `list` | Each row as a readable section: a heading, then its fields as a list. The default on a terminal, and the format that reads best when a row has many fields. It streams a row at a time, so a slow command fills in as it goes. |
| `table` | A rounded-border grid with aligned columns. On a wide result it shrinks to your terminal width instead of wrapping. Best when you want to scan one column down many rows. |
| `jsonl` | One JSON object per line. The default when piped. The natural input for `jq`. |
| `json` | A single JSON array of all rows. |
| `csv` | Comma-separated values with a header row. |
| `tsv` | Tab-separated values with a header row. |
| `markdown` | A GitHub-flavored pipe table, ready to paste into an issue, PR, or README. |
| `url` | The canonical X URL of each row, one per line. |
| `raw` | The upstream payload, unshaped, as x received it. |

The `list` and `table` views are two takes on the same rows: `list` puts one
record in front of you at a time and `table` lines many records up in a grid.
Reach for `-o table` when you are comparing a column across rows.

`--no-header` drops the header line from `csv`, `tsv`, and `markdown`, and the
section heading from `list`. `--color` (`auto|always|never`) controls color: a
bold header and dimmed grid lines in `table`, a bold heading and aligned keys in
`list`, and syntax-highlighted `json` and `jsonl`. Color is on by default on a
terminal and off when piped, so machine-read output stays plain. On a terminal
`list` styles its sections with color; piped or with `--color=never` it emits
literal GitHub-flavored markdown you can paste straight into an issue.

## Progress

A read can wait on the network before it has anything to show. When the terminal
is interactive, x prints a small spinner to standard error while it waits, and
clears it the moment the first row is ready. It only ever writes to standard
error, so a pipe like `x timeline nasa | jq` and a redirect like
`x timeline nasa > out.jsonl` never see it; the data on standard output stays
clean. `--quiet` turns it off.

## Projecting columns

`--fields` is a comma-separated list of keys to keep, in the order given:

```bash
x timeline nasa --fields id,text
x followers nasa --guest --fields username,name -o csv
```

The names are the JSON keys of a row, the same keys the `jsonl` and `json`
formats emit. It applies to `list`, `table`, `markdown`, `csv`, and `tsv`.

## Templates

`--template` renders each row with Go's `text/template`. The current row is the
dot (`.`) and its fields are addressed by their JSON-tag keys, including nested
objects:

```bash
x timeline nasa --template '{{.id}} {{.author.username}}: {{.text}}'
x user nasa     --template '{{.username}} {{.metrics.followers}}'
```

Semantics worth knowing:

- Keys mirror the `jsonl` output: `{{.id}}`, `{{.text}}`, `{{.author.username}}`,
  `{{.metrics.followers}}`, and so on.
- Integer fields render as plain digits.
- IDs render as their string value (see below).
- Standard `text/template` actions (`{{if}}`, `{{range}}`, pipelines) are
  available.

To discover the keys for a command, run it once with `-o json` and read the
structure.

## IDs are strings

Tweet and user IDs are 64-bit snowflakes. x always renders them as strings, in
every format, so they survive `jq`, CSV imports, and round-trips without losing
precision. `x tweet 20 -o json | jq .id` is `"20"`, not `20`, and a 19-digit id
comes back exactly as sent.

## What a record could not read

Every record says where it came from: `tier`, `surfaces`, `sources`, and `via`
for the field-by-field breakdown when more than one surface contributed. When a
surface was tried and did not answer, the record also carries `missed`:

```bash
x user nasa -o json | jq .missed
[
  "s2: rate limited by X on syndication.profile; the window resets at 16:45:41"
]
```

A read that falls back still answers, with fewer fields. Without `missed` there
is no way to tell that record from one where X had nothing more to say, and the
two mean different things: one is a fact about the account, the other is a fact
about the last fifteen minutes. x prints the same line on standard error as a
`warn:`, which `--quiet` suppresses.

## Timelines that are not in time order

X sometimes answers a profile timeline with a selection ranked by popularity
rather than the most recent posts, drawn from the whole account. Those records
carry `sample`:

```bash
x timeline jack -o json | jq -r '.[] | "\(.created_at) \(.sample)"'
2024-09-11T19:31:57Z true
2020-04-07T20:04:19Z true
2019-10-30T20:05:08Z true
```

Filtering "the last week" over a set like that answers a question nobody asked.
Pass `--guest` for a walk in time order. x also warns once on standard error
when a run produces sampled records.

## A walk that stopped early

Paging deep can hit a rate window or lose the connection partway. The rows
already printed stay on standard output, and the run ends with the reason and
how far it got:

```bash
x timeline nasa --guest -n 2000
...
Stopped after 612 tweets: rate limited by X on graphql.UserTweets
```

The exit code is whatever stopped it, `5` here, not `0`. Six hundred rows and a
zero exit would say the account has six hundred tweets. On `jsonl` the last line
is the failure record with the same `reason`.

## Exit codes

x uses distinct exit codes so scripts can branch on the outcome:

| Code | Meaning |
|---|---|
| `0` | Success |
| `1` | Generic error |
| `2` | Usage error (bad flags or arguments) |
| `3` | No results |
| `4` | Needs auth (a tier you have not enabled) |
| `5` | Rate-limited |
| `6` | Not found |
| `7` | Unsupported (x has no reader for that kind of thing) |
| `8` | Network error |

See [troubleshooting](/reference/troubleshooting/) for what to do about codes
`4`, `5`, and `6`.
