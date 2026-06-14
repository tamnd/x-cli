---
title: "Output and pipelines"
description: "The -o formats, --fields projection, --template rendering, piping to jq, IDs as strings, and --limit."
weight: 40
---

Every read in x produces rows. The same rows can come out as a readable list, a
table, JSONL, JSON, CSV, TSV, plain URLs, raw bytes, or whatever a Go template
makes of them. This guide shows how to pick a shape and pipe it.

## Formats

`-o` (or `--output`) selects the format:

```bash
x timeline nasa -o list      # each row as a section (default on a terminal)
x timeline nasa -o table     # aligned columns in a grid
x timeline nasa -o jsonl     # one JSON object per line (default when piped)
x timeline nasa -o json      # a single JSON array
x timeline nasa -o csv       # comma-separated, with a header row
x timeline nasa -o tsv       # tab-separated
x timeline nasa -o url       # just the URL of each row
x timeline nasa -o raw       # the raw upstream payload, unshaped
```

When you do not pass `-o`, x chooses for you: `list` on a terminal, `jsonl`
when the output is a pipe or file. So `x timeline nasa` reads nicely on screen
and `x timeline nasa | program` feeds JSONL without a flag. The `list` view
prints one record at a time as a short section, which reads better than a wide
grid when a row has many fields; switch to `-o table` when you want to scan one
column down many rows. Drop the header row from `csv`/`tsv`, and the section
heading from `list`, with `--no-header`.

## Projecting columns

`--fields` keeps only the columns you name, in order:

```bash
x timeline nasa --fields id,text
x following nasa --guest --fields username,name -o csv
```

The field names are the JSON keys of a row (see the template section for how to
discover them).

## Templates

`--template` renders each row with Go's `text/template`. The row is the dot, and
its fields are the JSON-tag keys, including nested ones:

```bash
x timeline nasa --template '{{.id}} {{.author.username}}: {{.text}}'
x user nasa     --template '{{.username}} has {{.metrics.followers}} followers'
```

Keys mirror the JSONL output: top-level fields like `{{.id}}` and `{{.text}}`,
nested objects like `{{.author.username}}`, and metric counts like
`{{.metrics.followers}}`. Integers render as plain digits, so a template is a
clean way to build a custom line per tweet or account. To see the available
keys for a command, run it once with `-o json` and read the structure.

## Piping to jq

JSONL is the natural bridge to `jq`:

```bash
x search "from:nasa" --guest -o jsonl | jq -r .id
x followers nasa --guest -o jsonl | jq -r 'select(.metrics.followers > 1000) | .username'
```

## IDs are strings

X IDs are 64-bit snowflakes. If a tool parses them as numbers it silently
corrupts the low digits. x always emits IDs as strings, so they survive `jq`,
spreadsheets, and round-trips untouched:

```bash
x tweet 20 -o json | jq .id     # "20", a string, not 20
```

A 19-digit tweet id comes back exactly as sent. Build URLs and re-query with the
string as-is.

## Limiting rows

`-n` (or `--limit`) caps the number of rows; `0` means unlimited:

```bash
x timeline nasa --guest -n 100   # at most 100 tweets
x followers nasa --guest -n 0    # everything the tier will return
```

x pages under the hood until it has enough rows or the source runs out, honoring
the request rate between pages so it stays a polite client.
