---
title: "Quick start"
description: "From an empty terminal to a real tweet, a timeline, shaped output, and a downloaded image."
weight: 30
---

This walks the core loop: read a tweet with no setup, look at a profile and
timeline, shape the output, and download some media. Every command here hits
live data and finishes in a second or two. None of it needs auth except where
noted.

## 1. Read a tweet

```bash
x tweet 20
```

```
@jack  just setting up my twttr
2006-03-21  •  replies 0  retweets 0  likes 0
```

That is Tier 0, syndication, no auth at all. A profile works the same way:

```bash
x user nasa
```

prints the handle, name, bio, and follower and following counts.

## 2. Follow a timeline

```bash
x timeline nasa -n 10
```

Tier 0 gives you a recent window of a user's tweets. To page deeper, opt into
the guest tier:

```bash
x timeline nasa --guest -n 50
```

x mints a guest token (cached for next time) and pages further back. Add
`--replies` to include replies or `--media` to keep only tweets with media.

## 3. Shape the output

On a terminal you get a table. Pick another shape with `-o`:

```bash
x timeline nasa -o url            # just the tweet URLs, one per line
x timeline nasa -o jsonl          # one JSON object per row (default when piped)
x timeline nasa --fields id,text  # project just the columns you want
```

Pipe JSONL into `jq`. IDs are strings, so they stay exact:

```bash
x timeline nasa --guest -o jsonl | jq -r '.id + "  " + .text'
```

Render each row yourself with a Go template:

```bash
x timeline nasa --template '{{.id}} {{.author.username}}: {{.text}}'
```

## 4. Download media

`x download` pulls a tweet's images and video to disk:

```bash
x download 20 -O ./media
```

It writes each attachment under the output directory and prints the paths it
wrote. To find media-heavy tweets first, then fetch them:

```bash
x media nasa --guest -o jsonl | jq -r .id | head -5 \
  | while read id; do x download "$id" -O ./media; done
```

## Where to next

You have the core loop. From here:

- [Reading tweets](/guides/reading-tweets/) covers timelines, replies, threads,
  and polls, and which tier each needs.
- [Search and discovery](/guides/search-and-discovery/) covers search,
  followers, and likers.
- [Your session](/guides/your-account/) imports your session to unlock the
  logged-in reads.
- The [CLI reference](/reference/cli/) lists every command and flag.
