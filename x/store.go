package x

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Store is the local SQLite dataset (spec §4.6), pure-Go so the binary stays
// CGO-free. It holds tweets/users/media, the addressed nodes and edges of spec
// 3003 doc 04 section 5, and a crawl queue.
type Store struct {
	db *sql.DB
}

const storeSchema = `
CREATE TABLE IF NOT EXISTS tweets (
  id TEXT PRIMARY KEY, text TEXT, author_id TEXT, author_username TEXT,
  conversation_id TEXT, reply_to TEXT, lang TEXT, created_at TIMESTAMP,
  replies INT, retweets INT, likes INT, quotes INT, bookmarks INT, impressions INT,
  is_retweet INT, is_quote INT, is_reply INT, possibly_sensitive INT,
  raw TEXT, fetched_at TIMESTAMP);
CREATE TABLE IF NOT EXISTS users (
  id TEXT PRIMARY KEY, rest_id TEXT, username TEXT, name TEXT, description TEXT, location TEXT,
  verified INT, followers INT, following INT, tweet_count INT, listed INT,
  created_at TIMESTAMP, raw TEXT, fetched_at TIMESTAMP);
CREATE TABLE IF NOT EXISTS media (
  key TEXT PRIMARY KEY, tweet_id TEXT, type TEXT, url TEXT, width INT, height INT,
  duration_ms INT, alt_text TEXT, raw TEXT);
CREATE TABLE IF NOT EXISTS nodes (
  uri TEXT PRIMARY KEY, kind TEXT NOT NULL, id TEXT NOT NULL,
  tier INTEGER NOT NULL, record TEXT NOT NULL, captured TIMESTAMP NOT NULL);
CREATE TABLE IF NOT EXISTS edges (
  from_uri TEXT NOT NULL, predicate TEXT NOT NULL, to_uri TEXT NOT NULL,
  source TEXT NOT NULL, tier INTEGER NOT NULL, captured TIMESTAMP NOT NULL,
  PRIMARY KEY (from_uri, predicate, to_uri, source));
CREATE INDEX IF NOT EXISTS edges_to ON edges (to_uri, predicate);
CREATE TABLE IF NOT EXISTS queue (
  url TEXT PRIMARY KEY, kind TEXT, priority INT, state TEXT,
  enqueued_at TIMESTAMP, done_at TIMESTAMP);
`

// OpenStore opens (creating if needed) the SQLite store at path.
func OpenStore(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("no store path: pass --db <file>")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if err := retireHopEdges(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(storeSchema); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// retireHopEdges moves an older store's edge table out of the way.
//
// Until spec 3003 doc 04 landed, `edges` was (src, dst, kind) and kind held a
// hop name, which is the direction the walk travelled rather than the claim the
// records make. The new table is the doc's, and the two shapes cannot share a
// name. The old rows are renamed rather than dropped, because they are somebody
// else's crawl and this is not the code that gets to decide they are worthless.
func retireHopEdges(db *sql.DB) error {
	var ddl string
	err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='edges'`).Scan(&ddl)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if !strings.Contains(ddl, "src") {
		return nil
	}
	_, err = db.Exec(`ALTER TABLE edges RENAME TO edges_hops`)
	return err
}

// Close closes the store.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the underlying handle for `x db query`.
func (s *Store) DB() *sql.DB { return s.db }

// UpsertTweet inserts or refreshes a tweet (and its author + media).
func (s *Store) UpsertTweet(t *Tweet) error {
	raw, _ := json.Marshal(t)
	var authorID, authorName string
	if t.Author != nil {
		authorID, authorName = t.Author.ID, t.Author.Username
		_ = s.UpsertUser(t.Author)
	}
	_, err := s.db.Exec(`INSERT INTO tweets
	  (id,text,author_id,author_username,conversation_id,reply_to,lang,created_at,
	   replies,retweets,likes,quotes,bookmarks,impressions,
	   is_retweet,is_quote,is_reply,possibly_sensitive,raw,fetched_at)
	  VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	  ON CONFLICT(id) DO UPDATE SET text=excluded.text,replies=excluded.replies,
	   retweets=excluded.retweets,likes=excluded.likes,quotes=excluded.quotes,
	   bookmarks=excluded.bookmarks,impressions=excluded.impressions,
	   raw=excluded.raw,fetched_at=excluded.fetched_at`,
		t.ID, t.Text, authorID, authorName, t.ConversationID, t.ReplyTo, t.Lang, t.CreatedAt,
		t.Metrics.Replies, t.Metrics.Retweets, t.Metrics.Likes, t.Metrics.Quotes,
		t.Metrics.Bookmarks, t.Metrics.Impressions,
		b2i(t.IsRetweet), b2i(t.IsQuote), b2i(t.IsReply), b2i(t.Sensitive), string(raw), nowUTC())
	if err != nil {
		return err
	}
	for _, m := range t.Media {
		_ = s.UpsertMedia(t.ID, m)
	}
	return nil
}

// UpsertUser inserts or refreshes a user.
func (s *Store) UpsertUser(u *User) error {
	if u == nil || u.ID == "" {
		return nil
	}
	raw, _ := json.Marshal(u)
	_, err := s.db.Exec(`INSERT INTO users
	  (id,rest_id,username,name,description,location,verified,followers,following,
	   tweet_count,listed,created_at,raw,fetched_at)
	  VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	  ON CONFLICT(id) DO UPDATE SET rest_id=excluded.rest_id,username=excluded.username,
	   name=excluded.name,
	   description=excluded.description,followers=excluded.followers,
	   following=excluded.following,tweet_count=excluded.tweet_count,
	   raw=excluded.raw,fetched_at=excluded.fetched_at`,
		u.ID, u.RestID, u.Username, u.Name, u.Description, u.Location, b2i(u.Verified),
		u.Metrics.Followers, u.Metrics.Following, u.Metrics.Tweets, u.Metrics.Listed,
		u.CreatedAt, string(raw), nowUTC())
	return err
}

// UpsertMedia inserts or refreshes a media row.
func (s *Store) UpsertMedia(tweetID string, m Media) error {
	key := m.Key
	if key == "" {
		key = tweetID + ":" + m.URL
	}
	raw, _ := json.Marshal(m)
	_, err := s.db.Exec(`INSERT INTO media (key,tweet_id,type,url,width,height,duration_ms,alt_text,raw)
	  VALUES (?,?,?,?,?,?,?,?,?)
	  ON CONFLICT(key) DO UPDATE SET url=excluded.url,raw=excluded.raw`,
		key, tweetID, m.Type, m.URL, m.Width, m.Height, m.Duration, m.AltText, string(raw))
	return err
}

// UpsertNode persists a discovered graph node by dispatching on its kind, so the
// discover/crawl walkers have one call to store whatever they reached. It writes
// the typed table, the addressed `nodes` row, and every edge the record asserts,
// because those three are one fact about one read and splitting them across
// three calls is how a store ends up with a node nothing points at.
func (s *Store) UpsertNode(n *Node) error {
	if n == nil {
		return nil
	}
	var err error
	switch n.Kind {
	case KindTweet:
		if n.Tweet == nil {
			return nil
		}
		err = s.UpsertTweet(n.Tweet)
		if err == nil {
			err = s.PutRecord(URI(KindTweet, n.Tweet.ID), KindTweet, n.Tweet.ID, n.Tweet.Meta.Tier, n.Tweet)
		}
	case KindUser:
		if n.User == nil {
			return nil
		}
		err = s.UpsertUser(n.User)
		if err == nil {
			err = s.PutRecord(userURI(n.User.Username), KindUser, n.User.Username, n.User.Meta.Tier, n.User)
		}
	default:
		return nil
	}
	if err != nil {
		return err
	}
	return s.PutEdges(Edges(n))
}

// PutRecord writes one addressed node. Upsert keeps the higher-tier record,
// because a tweet read with a session says strictly more than the same tweet
// read anonymously and a crawl that overwrote the richer read with a thinner one
// would lose data every time it revisited.
func (s *Store) PutRecord(uri, kind, id string, tier int, rec any) error {
	if uri == "" || id == "" {
		return nil
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO nodes (uri,kind,id,tier,record,captured)
	  VALUES (?,?,?,?,?,?)
	  ON CONFLICT(uri) DO UPDATE SET
	    tier=excluded.tier, record=excluded.record, captured=excluded.captured
	  WHERE excluded.tier >= nodes.tier`,
		uri, kind, id, tier, string(raw), nowUTC())
	return err
}

// PutEdges records claims (spec 3003 doc 04 section 5). The primary key carries
// the source, so two surfaces asserting the same thing stay two rows: that is
// the agreement, and the disagreement, that `x edges --conflicts` reads.
func (s *Store) PutEdges(es []Edge) error {
	for _, e := range es {
		if e.From == "" || e.To == "" || e.Predicate == "" {
			continue
		}
		_, err := s.db.Exec(`INSERT INTO edges (from_uri,predicate,to_uri,source,tier,captured)
		  VALUES (?,?,?,?,?,?)
		  ON CONFLICT(from_uri,predicate,to_uri,source) DO UPDATE SET
		    tier=excluded.tier, captured=excluded.captured`,
			e.From, string(e.Predicate), e.To, e.Source, e.Tier, nowUTC())
		if err != nil {
			return err
		}
	}
	return nil
}

// Enqueue adds a crawl target if not present.
func (s *Store) Enqueue(target, kind string, priority int) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO queue (url,kind,priority,state,enqueued_at)
	  VALUES (?,?,?,'pending',?)`, target, kind, priority, nowUTC())
	return err
}

// QueueItem is one crawl-queue entry.
type QueueItem struct {
	Target string
	Kind   string
}

// NextPending returns the next pending target (highest priority first).
func (s *Store) NextPending() (QueueItem, bool, error) {
	var q QueueItem
	err := s.db.QueryRow(`SELECT url,kind FROM queue WHERE state='pending'
	  ORDER BY priority DESC, enqueued_at ASC LIMIT 1`).Scan(&q.Target, &q.Kind)
	if err == sql.ErrNoRows {
		return q, false, nil
	}
	if err != nil {
		return q, false, err
	}
	return q, true, nil
}

// MarkDone marks a queue target done.
func (s *Store) MarkDone(target string) error {
	_, err := s.db.Exec(`UPDATE queue SET state='done', done_at=? WHERE url=?`, nowUTC(), target)
	return err
}

// ClearQueue empties the crawl queue.
func (s *Store) ClearQueue() error {
	_, err := s.db.Exec(`DELETE FROM queue`)
	return err
}

// QueueCounts returns counts by state.
func (s *Store) QueueCounts() (map[string]int, error) {
	rows, err := s.db.Query(`SELECT state, COUNT(*) FROM queue GROUP BY state`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]int{}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return nil, err
		}
		out[st] = n
	}
	return out, rows.Err()
}

// Stats returns row counts per table.
func (s *Store) Stats() (map[string]int, error) {
	out := map[string]int{}
	for _, tbl := range []string{"tweets", "users", "media", "nodes", "edges", "queue"} {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + tbl).Scan(&n); err != nil {
			return nil, err
		}
		out[tbl] = n
	}
	return out, nil
}

// TweetsByAuthor returns stored tweets for a username, oldest first.
func (s *Store) TweetsByAuthor(username string) ([]*Tweet, error) {
	rows, err := s.db.Query(`SELECT raw FROM tweets WHERE author_username=? COLLATE NOCASE ORDER BY created_at ASC`, username)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*Tweet
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var t Tweet
		if json.Unmarshal([]byte(raw), &t) == nil {
			out = append(out, &t)
		}
	}
	return out, rows.Err()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nowUTC() time.Time { return time.Now().UTC() }
