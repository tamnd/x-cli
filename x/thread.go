package x

import (
	"context"
)

// thread.go is the conversation walk (spec 3003 doc 05, `x thread` and
// `x replies`).
//
// Up and down are two different problems on X, and only one of them is free.
//
// Up is free at every tier, because surface 1 expands `parent` in full: the
// answer for a reply carries the tweet it replies to, whole, with its author,
// its counters and its own `in_reply_to_status_id_str`. It goes exactly one
// level and no further. Measured on 1903142823316049977, which is a reply to a
// reply: the answer carried `parent`, and that parent carried a reply-to id but
// no `parent` of its own.
//
// One level is still enough to halve the walk. A fetch of X hands back X and
// X's parent, so the next fetch asks for the grandparent and hands back the
// grandparent and the great-grandparent. A chain of n costs about n/2 requests
// rather than n, and the tweets arrive already stamped.
//
// Down is not free. A tweet does not carry its replies at any tier; you have to
// ask X who answered, and the only place that says so without a session is the
// status page, which renders three of them and no cursor. Tier 2 walks the tree
// with TweetDetail. So `x replies` is the command with a warning on it and
// `x thread` is not.

// maxChain bounds the upward walk when the caller sets no limit.
//
// There is no real conversation this deep. The bound is there because the walk
// follows ids X hands it, and a loop that trusts a remote server to terminate it
// is one strange answer away from running until somebody notices.
const maxChain = 100

// ParentChain walks up from a tweet to the root of its conversation and returns
// the chain root first, with the tweet asked for last.
//
// limit counts tweets, including the one asked for, and counts from the tweet
// rather than from the root, because the root is not known until the walk is
// over. limit <= 0 means the whole chain.
//
// A tweet that replies to nothing is a chain of one, which is the right answer
// and not an error.
func ParentChain(ctx context.Context, c *Client, id string, limit int) ([]*Tweet, error) {
	if limit <= 0 || limit > maxChain {
		limit = maxChain
	}
	// Built newest first, because that is the direction the walk goes, and
	// reversed at the end, because root first is the direction a thread reads.
	var up []*Tweet
	seen := map[string]bool{}
	for next := id; next != "" && len(up) < limit; {
		if seen[next] {
			// X does not build cycles. A walk that assumes so does not need
			// this line right up until the day it hangs.
			break
		}
		seen[next] = true
		t, parent, err := tweetAndParent(ctx, c, next)
		if err != nil {
			return reversed(up), partial(len(up), "ancestors", err)
		}
		up = append(up, t)
		if parent == nil {
			// X inlines the parent on every reply seen so far. If it stops, the
			// id is still on the record, so ask for it directly and carry on at
			// one tweet per request instead of two.
			next = t.ReplyTo
			continue
		}
		if len(up) == limit || seen[parent.ID] {
			break
		}
		seen[parent.ID] = true
		up = append(up, parent)
		next = parent.ReplyTo
	}
	return reversed(up), nil
}

// tweetAndParent is one surface 1 request read for both the tweets in it. The
// parent is stamped with the child's URL because that is honestly where it came
// from: nobody asked X for the parent.
func tweetAndParent(ctx context.Context, c *Client, id string) (*Tweet, *Tweet, error) {
	raw, src, err := fetchSynTweet(ctx, c, id)
	if err != nil {
		return nil, nil, err
	}
	t := raw.toTweet()
	stampTweet(t, 1, src)
	if raw.Parent == nil {
		return t, nil, nil
	}
	p := raw.Parent.toTweet()
	stampTweet(p, 1, src)
	return t, p, nil
}

func reversed(ts []*Tweet) []*Tweet {
	for i, j := 0, len(ts)-1; i < j; i, j = i+1, j-1 {
		ts[i], ts[j] = ts[j], ts[i]
	}
	return ts
}

// RepliesFromPage pulls the replies X chose to render on a tweet's status page,
// which is the whole of what tier 0 and tier 1 can see going downward.
//
// The focal tweet comes back too, first, because the caller wants to know what
// the replies are replies to and because the page's own reply counter is the
// only honest denominator for the warning `x replies` prints.
func RepliesFromPage(ctx context.Context, c *Client, id string) (focal *Tweet, replies []*Tweet, err error) {
	p, err := c.FetchPage(ctx, StatusPageURL(id))
	if err != nil {
		return nil, nil, err
	}
	focal, replies = pageReplies(p.Postings(), id)
	return focal, replies, nil
}

// firstNum is the first counter a surface actually published. It is for a value
// two keys can carry, where either is the answer and neither is a default.
func firstNum(ns ...*int) *int {
	for _, n := range ns {
		if n != nil {
			return n
		}
	}
	return nil
}
