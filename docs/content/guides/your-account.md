---
title: "Your session"
description: "Import your X session to unlock the reads X reserves for logged-in clients: search, followers, your home timeline, and bookmarks. x is read-only."
weight: 30
---

x is read-only: it has no command that posts, likes, follows, or otherwise
changes your account. But a few reads only exist for a logged-in client, so
importing your own X session unlocks more of what you can read. You import it
once from your browser cookies and x stores it on disk.

## Import your session

Log in to X in your browser, then copy two cookies, `auth_token` and `ct0`, from
the developer tools, and hand them to x:

```bash
x auth import --auth-token <auth_token> --ct0 <ct0>
```

Or paste a full `Cookie:` header on stdin and let x pull the two values out:

```bash
pbpaste | x auth import
```

Check and manage the session:

```bash
x auth status     # show the current session and tier
x auth logout     # forget the saved session
```

Once a session is imported, x uses it automatically for any command that needs
it; you do not pass a flag.

## What your session unlocks

With a session imported, the deeper reads X gates behind a logged-in client
start working:

```bash
x search "site reliability" -n 50    # full search
x followers nasa -n 100              # who follows an account
x following nasa -n 100              # who an account follows
x home                               # your reverse-chron home timeline
x bookmarks                          # your saved bookmarks
```

`home` and `bookmarks` are session-only by nature: they are your account's own
views. The rest also work with the opt-in guest tier for shallow windows, but a
session pages deeper and resolves more.

## Read-only by design

x never writes. Your session is used only to fetch data on your behalf, exactly
as a logged-out or logged-in browser would render it. There is nothing to
confirm and nothing to undo, because no command changes anything on X.
