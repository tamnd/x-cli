package x

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Store is the local SQLite dataset (spec §4.6), pure-Go so the binary stays
// CGO-free. It holds tweets/users/media/edges and a crawl queue.
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
  id TEXT PRIMARY KEY, username TEXT, name TEXT, description TEXT, location TEXT,
  verified INT, followers INT, following INT, tweet_count INT, listed INT,
  created_at TIMESTAMP, raw TEXT, fetched_at TIMESTAMP);
CREATE TABLE IF NOT EXISTS media (
  key TEXT PRIMARY KEY, tweet_id TEXT, type TEXT, url TEXT, width INT, height INT,
  duration_ms INT, alt_text TEXT, raw TEXT);
CREATE TABLE IF NOT EXISTS edges (
  src TEXT, dst TEXT, kind TEXT, PRIMARY KEY (src, dst, kind));
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
	if _, err := db.Exec(storeSchema); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
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
	  (id,username,name,description,location,verified,followers,following,
	   tweet_count,listed,created_at,raw,fetched_at)
	  VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
	  ON CONFLICT(id) DO UPDATE SET username=excluded.username,name=excluded.name,
	   description=excluded.description,followers=excluded.followers,
	   following=excluded.following,tweet_count=excluded.tweet_count,
	   raw=excluded.raw,fetched_at=excluded.fetched_at`,
		u.ID, u.Username, u.Name, u.Description, u.Location, b2i(u.Verified),
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

// UpsertEdge records a graph edge (follow/like/retweet/quote/reply/mention).
func (s *Store) UpsertEdge(src, dst, kind string) error {
	if src == "" || dst == "" {
		return nil
	}
	_, err := s.db.Exec(`INSERT OR IGNORE INTO edges (src,dst,kind) VALUES (?,?,?)`, src, dst, kind)
	return err
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
	for _, tbl := range []string{"tweets", "users", "media", "edges", "queue"} {
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
