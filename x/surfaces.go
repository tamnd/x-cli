package x

// surfaces.go is the routing table as data (spec 3003 doc 01 sections 7, 9 and
// 10). It is here rather than in a doc because the binary prints it and the
// client reads it, so what the tool says about itself and what it does come from
// one place. A row that stops being true shows up as a wrong answer, not as a
// stale paragraph in a README.

// Surface is one free public route into X's data.
type Surface struct {
	N        int    // the number the docs use, 1 through 8
	Name     string // short label, also the key in the routing table
	Host     string
	Tier     int    // the lowest tier that can read it
	Limit    string // the observed rate limit
	Window   string // the window that limit applies to
	Headers  bool   // whether X publishes rate-limit headers on it
	CacheTTL string // what the tool caches it for
	Note     string
}

// Surfaces is every route the tool knows, in the order the docs number them.
var Surfaces = []Surface{
	{1, "syndication tweet", "cdn.syndication.twimg.com", 0, "none observed", "", false, "60s to 24h",
		"one tweet with entities and media; the token is not validated"},
	{2, "syndication timeline", "syndication.twitter.com", 0, "30", "15 min", true, "5m",
		"about 100 recent tweets, ranked, no paging"},
	{3, "oembed", "publish.x.com", 0, "none observed", "", false, "30d",
		"the rendered blockquote, and the author handle"},
	{4, "guest graphql", "x.com/i/api/graphql", 1, "50 to 500 per operation", "15 min", true, "varies",
		"five operations survive the allowlist"},
	{5, "app-only v1.1", "api.x.com/1.1", 0, "180 place, 75 directory", "15 min", true, "5m to 7d",
		"trends and the place directory; a guest token here cuts the budget to 15"},
	{6, "media cdn", "pbs.twimg.com, video.twimg.com", 0, "none observed", "", false, "forever",
		"the bytes; the URL carries a content hash"},
	{7, "session graphql", "x.com/i/api/graphql", 2, "50 to 500 per operation", "15 min", true, "varies",
		"search, followers, likers, threads, lists, your bookmarks"},
	{8, "x.com html", "x.com", 0, "none published", "", false, "5m",
		"the page's own data planes; two routes carry data and the rest are a shell"},
}

// Tier describes one credential class.
type Tier struct {
	N          int
	Credential string
	Reads      string
}

// Tiers is the table `x tiers` prints. The status column is filled in at run
// time from the config, because it is a statement about this machine rather than
// about X.
var Tiers = []Tier{
	{0, "none", "tweet, user, timeline sample, list tweets, replies, media, oembed, bookmark and view counts, trends, places"},
	{1, "guest token", "adds source, the paged archive, and Spaces"},
	{2, "session cookies", "adds search, whole threads, followers, likers, lists, your bookmarks"},
}

// Route says which surface answers one question at each tier. An empty string
// means nothing serves it at that tier, which is the difference between exit
// code 4 and exit code 7.
type Route struct {
	Question string
	Tier0    string
	Tier1    string
	Tier2    string
}

// Routes is the table `x routes` prints, from doc 01 section 9. The client
// resolves a question to the best tier available and falls back down, never up.
var Routes = []Route{
	{"one tweet",
		"surface 1, plus surface 8 for every count including bookmarks and views",
		"surface 4 TweetResultByRestId, adds source",
		"surface 7, adds distinguishable errors"},
	{"one user",
		"surface 2, or surface 8 for the modern shape",
		"surface 4 UserByScreenName",
		"surface 7 UserByRestId for an id lookup"},
	{"a user's tweets",
		"surface 2, about 100 ranked and no paging; surface 8 gives 9 and is not on surface 2's budget",
		"surface 4 UserTweets, paged, the full archive",
		"surface 7, plus the replies and media tabs"},
	{"a reply's parent", "surface 1 parent", "same, richer", "surface 7, the full thread"},
	{"a quoted tweet", "surface 1 quoted_tweet", "same, richer", "same"},
	{"media urls", "surface 1 mediaDetails, or surface 8 og:image at original size",
		"surface 4 extended_entities", "same"},
	{"a list's tweets", "surface 2 /srv/list/{id}", "surface 2, the same read", "surface 7"},
	{"trends", "surface 5, on the public bearer alone", "the same read, and a guest token would only shrink the budget", "the same read"},
	{"places", "surface 5, the whole woeid directory", "the same read", "the same read"},
	{"replies to a tweet", "surface 8, three of them, not paged", "the same three",
		"surface 7, the whole tree, paged"},
	{"search", "", "", "surface 7"},
	{"followers, following", "", "", "surface 7"},
	{"likers, reposters", "", "", "surface 7"},
	{"a user's likes", "", "", "surface 7"},
	{"bookmarks", "", "", "surface 7, your own only"},
	{"one Space", "", "surface 4 AudioSpaceById, the whole record", "the same read"},
}

// LowestTier returns the cheapest tier that answers a question, and false when
// nothing serves it anywhere. That false is exit code 7, and it is what stops a
// caller from going to find a credential that would not help.
func LowestTier(question string) (int, bool) {
	for _, r := range Routes {
		if r.Question != question {
			continue
		}
		switch {
		case r.Tier0 != "":
			return 0, true
		case r.Tier1 != "":
			return 1, true
		case r.Tier2 != "":
			return 2, true
		}
		return 0, false
	}
	return 0, false
}
