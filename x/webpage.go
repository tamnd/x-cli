package x

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tamnd/x-cli/pkg/embed"
)

// Surface 8 is x.com itself, and it turned out to be the richest anonymous
// route this tool has.
//
// A status page ships the focal tweet with every engagement counter including
// the bookmark count, three replies in full, and the author's profile with
// follower and following counts. A profile page ships the account plus nine
// recent tweets. Neither needs a guest token and neither needs a session.
//
// The page states all of that twice, once as a Relay store and once as
// schema.org microdata, and this file reads both and merges them. When X moves
// a field in the store the microdata usually still has it, and the reverse
// happens too, so the two readings are not a primary and a fallback. They are
// two sources that disagree rarely and usefully.

const (
	// webUA is a browser User-Agent. x.com serves a different, much thinner
	// page to a client that does not look like a browser, so this is the one
	// place the tool does not send its own name. It is not an attempt to hide:
	// the request is anonymous either way and no credential is involved.
	webUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36"

	webPageTTL = ttlPage
)

// Page is one x.com HTML page, already split into its two data planes.
type Page struct {
	// URL is the page that was fetched.
	URL string
	// Store is the Relay record store, when the page carried one.
	Store *embed.Store
	// Items is every schema.org item on the page.
	Items []*embed.Item
	// Head is the OpenGraph and card metadata.
	Head *embed.Meta
}

// FetchPage reads one x.com page and extracts every plane from it.
//
// A missing plane is not an error on its own. A page for a protected account
// still has head metadata, and a page X decided to render thin still has the
// microdata, so this returns what it found and lets the caller decide whether
// that was enough.
func (c *Client) FetchPage(ctx context.Context, url string) (*Page, error) {
	body, err := c.Do(ctx, webPageReq(url))
	if err != nil {
		return nil, err
	}
	return ParsePage(url, string(body))
}

// webPageReq is the surface 8 request. It is a function rather than a literal
// so that `x capture` sends the same headers the reader sends: a page fetched
// without them comes back a different page, and a fixture of that tests the
// wrong thing.
func webPageReq(url string) Req {
	return Req{
		URL:      url,
		Endpoint: "web",
		Header: http.Header{
			"User-Agent":                {webUA},
			"Accept":                    {"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"},
			"Accept-Language":           {"en-US,en;q=0.9"},
			"Sec-Fetch-Dest":            {"document"},
			"Sec-Fetch-Mode":            {"navigate"},
			"Sec-Fetch-Site":            {"none"},
			"Sec-Fetch-User":            {"?1"},
			"Upgrade-Insecure-Requests": {"1"},
		},
		CacheTTL: webPageTTL,
	}
}

// ParsePage splits an already-fetched page into its planes. It is separate
// from FetchPage so that every test in this package runs off a saved capture
// rather than off the network.
func ParsePage(url, body string) (*Page, error) {
	p := &Page{URL: url}

	if doc, err := embed.Seroval(body); err == nil {
		if s, err := embed.RelayStore(doc); err == nil {
			p.Store = s
		}
	}
	p.Items, _ = embed.Microdata(body)
	p.Head, _ = embed.HeadMeta(body)

	if p.Store == nil && len(p.Items) == 0 {
		return nil, fmt.Errorf("no data in %s: the page carried neither a relay store nor microdata", url)
	}
	return p, nil
}

// TweetFromPage builds a tweet from a status page.
//
// The Relay store leads because it is typed and keeps null distinct from zero.
// The microdata fills whatever the store did not carry. Anything the two
// disagree on keeps the store's answer, and the disagreement is worth knowing
// about, which is what x edges --conflicts is for.
func (p *Page) TweetFromPage(id string) (*Tweet, error) {
	t := &Tweet{Meta: tweetMeta(id, 8, p.URL)}

	if p.Store != nil {
		if rec, ok := p.tweetRecord(id); ok {
			applyRelayTweet(t, rec)
		}
	}
	if item := p.posting(id); item != nil {
		applyMicrodataTweet(t, item)
	}
	applyHeadTweet(t, p.Head)
	// The store's rest_id wins over the id we asked with, which is how a
	// redirected or edited status ends up under the id X itself uses.
	if t.ID == "" {
		t.ID = id
	}
	t.Identify(KindTweet, t.ID)
	stampTweet(t, 8, p.URL)
	if t.Text == "" && t.Author == nil {
		return nil, fmt.Errorf("no tweet %s on %s", id, p.URL)
	}
	if t.URL == "" {
		author := ""
		if t.Author != nil {
			author = t.Author.Username
		}
		t.URL = TweetURL(author, t.ID)
	}
	return t, nil
}

// tweetRecord finds one tweet in the Relay store, by the root field when the
// page was rendered for that tweet and by a scan otherwise. A status page
// carries the replies too, and they are only reachable by the scan.
func (p *Page) tweetRecord(id string) (map[string]any, bool) {
	if f, ok := p.Store.Field("tweet_result_by_rest_id"); ok {
		res, _ := p.Store.Resolve(f).(map[string]any)
		if rec, ok := res["result"].(map[string]any); ok && rec["rest_id"] == id {
			return rec, true
		}
	}
	for _, v := range p.Store.Typed("Tweet") {
		rec, ok := v.(map[string]any)
		if ok && rec["rest_id"] == id {
			return rec, true
		}
	}
	return nil, false
}

// posting finds one SocialMediaPosting item by tweet id.
func (p *Page) posting(id string) *embed.Item {
	for _, it := range embed.Find(p.Items, "SocialMediaPosting") {
		if it.Str("identifier") == id {
			return it
		}
	}
	return nil
}

// Postings returns every tweet the page carries, in document order. On a
// status page that is the focal tweet and its replies; on a profile page it is
// the timeline.
func (p *Page) Postings() []*Tweet {
	var out []*Tweet
	for _, it := range embed.Find(p.Items, "SocialMediaPosting") {
		id := it.Str("identifier")
		if id == "" {
			continue
		}
		t, err := p.TweetFromPage(id)
		if err != nil {
			continue
		}
		out = append(out, t)
	}
	return out
}

// applyRelayTweet copies a Relay Tweet record onto our model.
func applyRelayTweet(t *Tweet, rec map[string]any) {
	if v, ok := embed.DigStr(rec, "rest_id"); ok {
		t.ID = v
	}
	if v, ok := embed.DigStr(rec, "details", "full_text"); ok {
		t.Text = v
	}
	if ms, ok := embed.Dig(rec, "details", "created_at_ms"); ok {
		if n, ok := asInt(ms); ok {
			t.CreatedAt = time.UnixMilli(n).UTC()
		}
	}
	if v, ok := embed.Dig(rec, "legacy", "possibly_sensitive"); ok {
		if b, ok := v.(bool); ok {
			t.Sensitive = b
		}
	}
	if counts, ok := rec["counts"].(map[string]any); ok {
		set := func(dst **int, key string) {
			n, ok := asInt(counts[key])
			setNum(dst, int(n), ok)
		}
		set(&t.Metrics.Likes, "favorite_count")
		set(&t.Metrics.Retweets, "retweet_count")
		set(&t.Metrics.Replies, "reply_count")
		set(&t.Metrics.Quotes, "quote_count")
		// This one is the correction to spec 3003. The bookmark count was
		// written up as tier 1 only. The status page has it at tier 0.
		set(&t.Metrics.Bookmarks, "bookmark_count")
	}
	if v, ok := embed.Dig(rec, "views", "count"); ok {
		n, ok := asInt(v)
		setNum(&t.Metrics.Impressions, int(n), ok)
	}
	if author, ok := embed.Dig(rec, "core", "user_results", "result"); ok {
		if m, ok := author.(map[string]any); ok {
			t.Author = userFromRelay(m)
		}
	}
	if reply, ok := embed.Dig(rec, "reply_to_results", "result"); ok {
		if v, ok := embed.DigStr(reply, "rest_id"); ok {
			t.ReplyTo = v
			t.IsReply = true
		}
	}
	if u, ok := embed.Dig(rec, "reply_to_user_results", "result"); ok {
		if v, ok := embed.DigStr(u, "core", "screen_name"); ok {
			t.ReplyToUser = v
		}
	}
	for _, m := range asList(rec["mention_entities"]) {
		if v, ok := embed.DigStr(m, "screen_name"); ok {
			t.Entities.Mentions = append(t.Entities.Mentions, v)
		}
	}
	for _, u := range asList(rec["url_entities"]) {
		if v, ok := embed.DigStr(u, "expanded_url"); ok {
			t.Entities.URLs = append(t.Entities.URLs, v)
		}
	}
	for _, h := range asList(embed.MustDig(rec, "details", "hashtag_entities")) {
		if v, ok := embed.DigStr(h, "text"); ok {
			t.Entities.Hashtags = append(t.Entities.Hashtags, v)
		}
	}
}

// userFromRelay copies a Relay User record onto our model.
func userFromRelay(rec map[string]any) *User {
	u := &User{}
	if v, ok := embed.DigStr(rec, "rest_id"); ok {
		u.RestID = v
	}
	if v, ok := embed.DigStr(rec, "core", "screen_name"); ok {
		u.setHandle(v)
	}
	if v, ok := embed.DigStr(rec, "core", "name"); ok {
		u.Name = v
	}
	if v, ok := embed.DigStr(rec, "profile_bio", "description"); ok {
		u.Description = v
	}
	if v, ok := embed.DigStr(rec, "location", "location"); ok {
		u.Location = v
	}
	if v, ok := embed.DigStr(rec, "avatar", "image_url"); ok {
		u.ProfileImage = v
	}
	if v, ok := embed.DigStr(rec, "banner", "image_url"); ok {
		u.ProfileBanner = v
	}
	if v, ok := embed.Dig(rec, "privacy", "protected"); ok {
		if b, ok := v.(bool); ok {
			u.Protected = b
		}
	}
	if v, ok := embed.Dig(rec, "verification", "is_blue_verified"); ok {
		if b, ok := v.(bool); ok {
			u.Verified = b
		}
	}
	if v, ok := embed.DigStr(rec, "verification", "verified_type"); ok {
		u.VerifiedType = v
	}
	n, ok := asInt(embed.MustDig(rec, "relationship_counts", "followers"))
	setNum(&u.Metrics.Followers, int(n), ok)
	n, ok = asInt(embed.MustDig(rec, "relationship_counts", "following"))
	setNum(&u.Metrics.Following, int(n), ok)
	n, ok = asInt(embed.MustDig(rec, "tweet_counts", "tweets"))
	setNum(&u.Metrics.Tweets, int(n), ok)
	return u
}

// applyMicrodataUser fills a profile from a schema.org Person, filling gaps
// only.
//
// Two counters live under agentInteractionStatistic rather than
// interactionStatistic, which is schema.org being precise: follows and tweets
// are things the account did, follower count is something done to it. Reading
// only the one prop name is why a profile page used to report following 0 and
// tweets 0 for an account whose page states both.
func applyMicrodataUser(u *User, person *embed.Item) {
	fillStr(&u.RestID, person.Str("identifier"))
	// A Person on a profile page carries the handle as additionalName; the
	// author of a posting carries it as alternateName. Take whichever is there.
	//
	// It wins even when the record already has a handle, as long as it is the
	// same account: the page states the casing its owner chose, and the handle
	// the record came in with is the casing whoever typed the command chose. Two
	// surfaces disagreeing about whether @nasa is @NASA is not a fact about X.
	for _, stated := range []string{person.Str("additionalName"), person.Str("alternateName")} {
		if stated == "" {
			continue
		}
		if u.Username == "" || handleID(stated) == u.ID {
			u.setHandle(stated)
			break
		}
	}
	fillStr(&u.Name, person.Str("name"))
	fillStr(&u.Description, person.Str("description"))
	if home := person.Item("homeLocation"); home != nil {
		fillStr(&u.Location, home.Str("name"))
	}
	if img := person.Item("image"); img != nil {
		fillStr(&u.ProfileImage, img.Str("thumbnailUrl"))
		fillStr(&u.ProfileImage, img.Str("contentUrl"))
	} else {
		// The author of a posting states the avatar as a plain URL.
		fillStr(&u.ProfileImage, person.Str("image"))
	}
	count := func(dst **int, prop, action string) {
		if *dst != nil {
			return
		}
		if v, ok := person.Counter(prop, action); ok {
			n, err := strconv.Atoi(v)
			setNum(dst, n, err == nil)
		}
	}
	count(&u.Metrics.Followers, "interactionStatistic", "FollowAction")
	count(&u.Metrics.Following, "agentInteractionStatistic", "FollowAction")
	count(&u.Metrics.Tweets, "agentInteractionStatistic", "WriteAction")
	// sameAs is the site the account links from its bio, which belongs with the
	// rest of its entities rather than in URL: that field is the profile itself.
	// It is also the expanded destination, already resolved, so it fills website
	// too and this surface stops being the one tier-0 read that has no website.
	for _, v := range person.Props["sameAs"] {
		if s, ok := v.(string); ok && s != "" && !hasStr(u.Entities.URLs, s) {
			u.Entities.URLs = append(u.Entities.URLs, s)
			if u.Website == "" {
				u.Website = s
			}
		}
	}
}

// applyMicrodataTweet fills in whatever the Relay store did not carry. It
// never overwrites: the store is typed and this plane is all strings.
func applyMicrodataTweet(t *Tweet, it *embed.Item) {
	if t.Text == "" {
		t.Text = it.Str("articleBody")
	}
	if t.URL == "" {
		t.URL = it.Str("url")
	}
	if t.CreatedAt.IsZero() {
		if v, err := time.Parse(time.RFC3339, it.Str("dateCreated")); err == nil {
			t.CreatedAt = v.UTC()
		}
	}
	fill := func(dst **int, prop, action string) {
		if *dst != nil {
			return
		}
		if v, ok := it.Counter(prop, action); ok {
			n, err := strconv.Atoi(v)
			setNum(dst, n, err == nil)
		}
	}
	fill(&t.Metrics.Likes, "interactionStatistic", "LikeAction")
	fill(&t.Metrics.Retweets, "interactionStatistic", "ShareAction")
	fill(&t.Metrics.Impressions, "interactionStatistic", "ViewAction")
	if t.Metrics.Replies == nil {
		n, err := strconv.Atoi(it.Str("commentCount"))
		setNum(&t.Metrics.Replies, n, err == nil)
	}
	if author := it.Item("author"); author != nil {
		if t.Author == nil {
			t.Author = &User{}
		}
		applyMicrodataUser(t.Author, author)
		fillStr(&t.Author.URL, author.Str("url"))
	}
	for _, img := range it.Items("image") {
		url := img.Str("contentUrl")
		if url == "" {
			continue
		}
		if hasMedia(t.Media, url) {
			continue
		}
		m := Media{Type: "photo", URL: url, AltText: img.Str("description")}
		m.Width, _ = strconv.Atoi(img.Str("width"))
		m.Height, _ = strconv.Atoi(img.Str("height"))
		t.Media = append(t.Media, m)
	}
}

// UserFromPage builds a profile from a profile page.
func (p *Page) UserFromPage(handle string) (*User, error) {
	u := &User{}
	u.setHandle(handle)

	if p.Store != nil {
		if f, ok := p.Store.Field("user_result_by_screen_name"); ok {
			if res, ok := p.Store.Resolve(f).(map[string]any); ok {
				if rec, ok := res["result"].(map[string]any); ok {
					u = userFromRelay(rec)
				}
			}
		}
	}
	for _, pp := range embed.Find(p.Items, "ProfilePage") {
		person := pp.Item("mainEntity")
		if person == nil {
			continue
		}
		applyMicrodataUser(u, person)
		if u.CreatedAt.IsZero() {
			if v, err := time.Parse(time.RFC3339, pp.Str("dateCreated")); err == nil {
				u.CreatedAt = v.UTC()
			}
		}
	}
	applyHeadUser(u, p.Head)
	if u.Username == "" && u.RestID == "" {
		return nil, fmt.Errorf("no profile on %s", p.URL)
	}
	if u.Username == "" {
		u.setHandle(handle)
	}
	if u.URL == "" {
		u.URL = UserURL(u.Username)
	}
	stampUser(u, 8, p.URL)
	return u, nil
}

// StatusPageURL is the canonical status page for a tweet id. The author is not
// needed: x.com redirects /i/status/{id} to the real permalink.
func StatusPageURL(id string) string { return "https://x.com/i/status/" + id }

func hasMedia(ms []Media, url string) bool {
	for _, m := range ms {
		if m.URL == url {
			return true
		}
	}
	return false
}

// asInt accepts the two number shapes that reach here: an int64 out of the
// seroval parser and a float64 out of encoding/json.
func asInt(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case float64:
		return int64(n), true
	case int:
		return int64(n), true
	case string:
		i, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
		return i, err == nil
	}
	return 0, false
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}
