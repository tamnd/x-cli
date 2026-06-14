---
title: "Output formats"
description: "Every -o format defined precisely, the --fields projection, --template semantics, and the exit codes."
weight: 20
---

x turns each command's result into rows and renders them through one of several
formats. This page defines each format, the `--fields` and `--template`
modifiers, and the exit codes.

## Choosing a format

`-o` (or `--output`) selects the format. With no `-o`, x picks `table` when the
output is a terminal and `jsonl` when it is a pipe or file.

| Format | What it produces |
|---|---|
| `table` | A rounded-border grid with aligned columns. The default on a terminal. On a wide result it shrinks to your terminal width instead of wrapping. |
| `jsonl` | One JSON object per line. The default when piped. The natural input for `jq`. |
| `json` | A single JSON array of all rows. |
| `csv` | Comma-separated values with a header row. |
| `tsv` | Tab-separated values with a header row. |
| `markdown` | A GitHub-flavored pipe table, ready to paste into an issue, PR, or README. |
| `url` | The canonical X URL of each row, one per line. |
| `raw` | The upstream payload, unshaped, as x received it. |

`--no-header` drops the header line from `csv`, `tsv`, and `markdown`. `--color`
(`auto|always|never`) controls color: a bold header and dimmed grid lines in the
`table` format, and syntax-highlighted `json` and `jsonl`. Color is on by default
on a terminal and off when piped, so machine-read output stays plain.

## Projecting columns

`--fields` is a comma-separated list of keys to keep, in the order given:

```bash
x timeline nasa --fields id,text
x followers nasa --guest --fields username,name -o csv
```

The names are the JSON keys of a row, the same keys the `jsonl` and `json`
formats emit. It applies to `table`, `csv`, `tsv`, `json`, and `jsonl`.

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

See [troubleshooting](/reference/troubleshooting/) for what to do about codes
`4`, `5`, and `6`.
