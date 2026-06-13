package x

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Writes go through the same GraphQL mutations / REST endpoints the web client
// uses, authenticated by the user's own imported session (Tier 2, spec §3.2).
// Every write requires session cookies; guest mode cannot write.

// NewTweet is the input to CreateTweet.
type NewTweet struct {
	Text          string
	ReplyTo       string
	Quote         string
	MediaIDs      []string
	ReplySettings string // everyone|following|mentioned
}

func (g *GraphQL) requireSession(action string) error {
	if !g.s.IsUser() {
		return &NeedAuthError{Msg: action + " needs your own session — run `x auth import` first", User: true}
	}
	return nil
}

// post performs a GraphQL mutation POST and returns the raw response.
func (g *GraphQL) post(ctx context.Context, op string, variables map[string]any) ([]byte, error) {
	id := g.queryID(op)
	if id == "" {
		return nil, fmt.Errorf("no query id for operation %q", op)
	}
	body := map[string]any{
		"queryId":   id,
		"variables": variables,
	}
	var feats any
	if err := json.Unmarshal([]byte(g.features()), &feats); err == nil {
		body["features"] = feats
	}
	bb, _ := json.Marshal(body)
	h, err := g.s.authHeaders(ctx, g.c)
	if err != nil {
		return nil, err
	}
	h.Set("Content-Type", "application/json")
	u := fmt.Sprintf("https://x.com/i/api/graphql/%s/%s", id, op)
	if tid := g.transactionID(ctx, http.MethodPost, "/i/api/graphql/"+id+"/"+op); tid != "" {
		h.Set("x-client-transaction-id", tid)
	}
	b, err := g.c.Do(ctx, Req{Method: http.MethodPost, URL: u, Endpoint: "graphql." + op, Header: h, Body: bb})
	if err != nil {
		return nil, gqlError(err)
	}
	return b, nil
}

// restPost performs an x.com/i/api/1.1 form POST (follow/mute/block/dm).
func (g *GraphQL) restPost(ctx context.Context, path string, form url.Values) ([]byte, error) {
	h, err := g.s.authHeaders(ctx, g.c)
	if err != nil {
		return nil, err
	}
	h.Set("Content-Type", "application/x-www-form-urlencoded")
	u := "https://api.x.com/1.1/" + path
	b, err := g.c.Do(ctx, Req{Method: http.MethodPost, URL: u, Endpoint: "rest." + path, Header: h, Body: []byte(form.Encode())})
	if err != nil {
		return nil, gqlError(err)
	}
	return b, nil
}

// CreateTweet posts a tweet and returns the created object.
func (g *GraphQL) CreateTweet(ctx context.Context, in NewTweet) (*Tweet, error) {
	if err := g.requireSession("posting"); err != nil {
		return nil, err
	}
	media := map[string]any{"media_entities": []any{}, "possibly_sensitive": false}
	if len(in.MediaIDs) > 0 {
		ents := make([]any, 0, len(in.MediaIDs))
		for _, id := range in.MediaIDs {
			ents = append(ents, map[string]any{"media_id": id, "tagged_users": []any{}})
		}
		media["media_entities"] = ents
	}
	vars := map[string]any{
		"tweet_text":              in.Text,
		"dark_request":            false,
		"media":                   media,
		"semantic_annotation_ids": []any{},
	}
	if in.ReplyTo != "" {
		vars["reply"] = map[string]any{"in_reply_to_tweet_id": in.ReplyTo, "exclude_reply_user_ids": []any{}}
	}
	if in.Quote != "" {
		vars["attachment_url"] = TweetURL("i/web", in.Quote)
	}
	if in.ReplySettings != "" && in.ReplySettings != "everyone" {
		vars["conversation_control"] = map[string]any{"mode": in.ReplySettings}
	}
	b, err := g.post(ctx, "CreateTweet", vars)
	if err != nil {
		return nil, err
	}
	tweets, _ := collectTweets(b)
	if len(tweets) > 0 {
		return tweets[0], nil
	}
	return &Tweet{Text: in.Text, Provenance: "graphql"}, nil
}

// DeleteTweet deletes one of the user's tweets.
func (g *GraphQL) DeleteTweet(ctx context.Context, id string) error {
	if err := g.requireSession("deleting"); err != nil {
		return err
	}
	_, err := g.post(ctx, "DeleteTweet", map[string]any{"tweet_id": id, "dark_request": false})
	return err
}

// Like likes (or, with undo, unlikes) a tweet.
func (g *GraphQL) Like(ctx context.Context, id string, undo bool) error {
	if err := g.requireSession("liking"); err != nil {
		return err
	}
	op := "FavoriteTweet"
	if undo {
		op = "UnfavoriteTweet"
	}
	_, err := g.post(ctx, op, map[string]any{"tweet_id": id})
	return err
}

// Retweet retweets (or un-retweets) a tweet.
func (g *GraphQL) Retweet(ctx context.Context, id string, undo bool) error {
	if err := g.requireSession("retweeting"); err != nil {
		return err
	}
	op := "CreateRetweet"
	if undo {
		op = "DeleteRetweet"
	}
	key := "tweet_id"
	if undo {
		key = "source_tweet_id"
	}
	_, err := g.post(ctx, op, map[string]any{key: id, "dark_request": false})
	return err
}

// Bookmark bookmarks (or un-bookmarks) a tweet.
func (g *GraphQL) Bookmark(ctx context.Context, id string, undo bool) error {
	if err := g.requireSession("bookmarking"); err != nil {
		return err
	}
	op := "CreateBookmark"
	if undo {
		op = "DeleteBookmark"
	}
	_, err := g.post(ctx, op, map[string]any{"tweet_id": id})
	return err
}

// Follow follows (or unfollows) a user by numeric id.
func (g *GraphQL) Follow(ctx context.Context, userID string, undo bool) error {
	if err := g.requireSession("following"); err != nil {
		return err
	}
	path := "friendships/create.json"
	if undo {
		path = "friendships/destroy.json"
	}
	_, err := g.restPost(ctx, path, url.Values{"user_id": {userID}})
	return err
}

// Mute mutes (or unmutes) a user by numeric id.
func (g *GraphQL) Mute(ctx context.Context, userID string, undo bool) error {
	if err := g.requireSession("muting"); err != nil {
		return err
	}
	path := "mutes/users/create.json"
	if undo {
		path = "mutes/users/destroy.json"
	}
	_, err := g.restPost(ctx, path, url.Values{"user_id": {userID}})
	return err
}

// Block blocks (or unblocks) a user by numeric id.
func (g *GraphQL) Block(ctx context.Context, userID string, undo bool) error {
	if err := g.requireSession("blocking"); err != nil {
		return err
	}
	path := "blocks/create.json"
	if undo {
		path = "blocks/destroy.json"
	}
	_, err := g.restPost(ctx, path, url.Values{"user_id": {userID}})
	return err
}

// SendDM sends a direct message to a recipient by numeric id.
func (g *GraphQL) SendDM(ctx context.Context, recipientID, text string) error {
	if err := g.requireSession("sending a DM"); err != nil {
		return err
	}
	payload := map[string]any{
		"event": map[string]any{
			"type": "message_create",
			"message_create": map[string]any{
				"target":       map[string]any{"recipient_id": recipientID},
				"message_data": map[string]any{"text": text},
			},
		},
	}
	bb, _ := json.Marshal(payload)
	h, err := g.s.authHeaders(ctx, g.c)
	if err != nil {
		return err
	}
	h.Set("Content-Type", "application/json")
	_, err = g.c.Do(ctx, Req{
		Method:   http.MethodPost,
		URL:      "https://api.x.com/1.1/direct_messages/events/new.json",
		Endpoint: "rest.dm",
		Header:   h,
		Body:     bb,
	})
	if err != nil {
		return gqlError(err)
	}
	return nil
}

var _ = strings.TrimSpace
