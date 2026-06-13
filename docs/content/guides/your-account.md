---
title: "Your account"
description: "Import your X session, then post, reply, like, follow, DM, and read your home and bookmarks, with confirmation, --yes, and --dry-run."
weight: 30
---

Everything that acts as you (the writes) and a few reads that only exist for
your account (home, bookmarks) need your own X session. You import it once from
your browser cookies and x stores it on disk.

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

## Reads that need your session

```bash
x home              # your reverse-chron home timeline
x bookmarks         # your saved bookmarks
```

Both are session-only by nature: they are your account's own views.

## Writes

All of these act as you and need a session. Several take `--undo` to reverse the
action.

```bash
x post "hello from the terminal"
x reply 20 "still works"
x delete <ref>                       # delete one of your tweets
x like <ref>                         # x like <ref> --undo  to unlike
x retweet <ref>                      # --undo to unretweet
x bookmark <ref>                     # --undo to remove the bookmark
x follow nasa                        # --undo to unfollow
x mute nasa                          # --undo to unmute
x block nasa                         # --undo to unblock
x dm nasa "sent from the terminal"   # send a direct message
```

`x post` has a few extras: `--reply-to <ref>` and `--quote <ref>` to reply or
quote without the dedicated commands, and `--reply-settings Everyone|Following|MentionedUsers`
to control who can reply.

## Confirm, skip, or rehearse

Writes change your account, so x confirms before firing. You get a prompt
describing the action; answer yes and it sends.

```bash
x post "ship it"          # prompts, then posts
x post "ship it" --yes    # skip the prompt (good for scripts)
x post "ship it" --dry-run
```

`--dry-run` prints the exact request x would send and stops, without touching
your account. It is the safe way to see what a command does before you let it
fire. `--yes` (or `-y`) assumes yes to the prompt for unattended runs.
