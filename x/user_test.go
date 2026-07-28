package x

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// A profile is the one record x can build at every tier, which is why it is the
// one worth pinning down: surface 4 in one request, surfaces 2 and 8 in two, and
// the answers have to be the same account either way.

// decodeS4User is UserByName's decode without the request in front of it.
func decodeS4User(t *testing.T) *User {
	t.Helper()
	objs := userResults([]byte(capture(t, "s4_user_nasa.json.gz")))
	if len(objs) == 0 {
		t.Fatal("no user in the UserByScreenName capture")
	}
	var ur gqlUserResult
	if err := json.Unmarshal(objs[0], &ur); err != nil {
		t.Fatal(err)
	}
	u := ur.toUser()
	if u == nil {
		t.Fatal("the capture decoded to no user at all")
	}
	return u
}

// The 2026 UserByScreenName response splits what used to be one flat legacy
// object across core, location, avatar, privacy, verification and profile_bio,
// and it does not leave a copy behind (doc 03 section 3.1). Every row of that
// mapping table is a field a legacy-only reader loses without noticing.
func TestGuestUserByScreenNameReadsTheModernShape(t *testing.T) {
	u := decodeS4User(t)
	for _, c := range []struct{ field, got, want string }{
		{"handle, from core.screen_name", u.Username, "NASA"},
		{"id, the handle lowercased", u.ID, "nasa"},
		{"rest_id", u.RestID, "11348282"},
		{"name, from core.name", u.Name, "NASA"},
		{"bio, from profile_bio.description", u.Description, "Making the seemingly impossible, possible. ✨"},
		{"location, from location.location", u.Location, "Pale Blue Dot"},
		{"avatar, from avatar.image_url", u.ProfileImage, "https://pbs.twimg.com/profile_images/1321163587679784960/0ZxKlEKB_normal.jpg"},
		{"banner, only legacy has one", u.ProfileBanner, "https://pbs.twimg.com/profile_banners/11348282/1775567134"},
		{"website, expanded, off legacy.entities", u.Website, "http://www.nasa.gov/"},
		{"verified_type, from verification", u.VerifiedType, "government"},
	} {
		if c.got != c.want {
			t.Errorf("%s: got %q, want %q", c.field, c.got, c.want)
		}
	}
	if u.CreatedAt.Format("2006-01-02") != "2007-12-19" {
		t.Errorf("created_at, from core: got %v", u.CreatedAt)
	}
	// verification.verified is false and is_blue_verified is true, and the tick
	// on the account is the grey one. Reporting the account as unverified
	// because one of the two flags is false is the mistake this guards.
	if !u.Verified {
		t.Error("a government account with a tick came back unverified")
	}
	if u.Protected {
		t.Error("privacy.protected is false on this capture")
	}
	for _, c := range []struct {
		field string
		got   *int
		want  int
	}{
		{"followers", u.Metrics.Followers, 92219437},
		{"following", u.Metrics.Following, 119},
		{"tweets", u.Metrics.Tweets, 74261},
		{"listed", u.Metrics.Listed, 97038},
		{"likes", u.Metrics.Likes, 16894},
		{"media", u.Metrics.Media, 28050},
	} {
		if c.got == nil {
			t.Errorf("%s: nothing, want %d", c.field, c.want)
		} else if *c.got != c.want {
			t.Errorf("%s: got %d, want %d", c.field, *c.got, c.want)
		}
	}
	if !hasStr(u.Entities.URLs, "http://www.nasa.gov/") {
		t.Errorf("the bio link is missing from entities: %v", u.Entities.URLs)
	}
}

// Tier 0 is not a cut-down profile. Surface 2 states the counters tier 1 states,
// the banner, and what the tick means, and the only reason any of it used to be
// missing from a tier-0 record was that the decoder was not reading it.
func TestSyndicationProfileGivesWhatTierOneGives(t *testing.T) {
	raw, ok := extractNextData([]byte(capture(t, "s2_timeline_nasa.html.gz")))
	if !ok {
		t.Fatal("no __NEXT_DATA__ in the s2 fixture")
	}
	s2, ok := profileFromTimeline(raw, "nasa")
	if !ok {
		t.Fatal("the profile widget carried no author to read a profile off")
	}
	s4 := decodeS4User(t)
	if s2.Username != s4.Username || s2.ID != s4.ID || s2.RestID != s4.RestID {
		t.Errorf("the two surfaces name different accounts: %s/%s/%s vs %s/%s/%s",
			s2.Username, s2.ID, s2.RestID, s4.Username, s4.ID, s4.RestID)
	}
	for _, c := range []struct{ field, s2, s4 string }{
		{"name", s2.Name, s4.Name},
		{"description", s2.Description, s4.Description},
		{"website", s2.Website, s4.Website},
		{"verified_type", s2.VerifiedType, s4.VerifiedType},
		{"profile_banner", s2.ProfileBanner, s4.ProfileBanner},
	} {
		if c.s2 != c.s4 {
			t.Errorf("%s: surface 2 says %q, surface 4 says %q", c.field, c.s2, c.s4)
		}
	}
	if !s2.Verified {
		t.Error("surface 2 dropped the government tick")
	}
	for _, c := range []struct {
		field    string
		s2, s4   *int
		optional bool
	}{
		{field: "followers", s2: s2.Metrics.Followers, s4: s4.Metrics.Followers, optional: true},
		{field: "following", s2: s2.Metrics.Following, s4: s4.Metrics.Following},
		{field: "tweets", s2: s2.Metrics.Tweets, s4: s4.Metrics.Tweets},
		{field: "listed", s2: s2.Metrics.Listed, s4: s4.Metrics.Listed},
		{field: "likes", s2: s2.Metrics.Likes, s4: s4.Metrics.Likes},
		{field: "media", s2: s2.Metrics.Media, s4: s4.Metrics.Media},
	} {
		if c.s2 == nil {
			t.Errorf("%s: surface 2 has no number", c.field)
			continue
		}
		// The follower count moves between two captures of the same account and
		// is not worth pinning; that it is there and is the same order of
		// magnitude is.
		if c.optional {
			continue
		}
		if c.s4 != nil && *c.s2 != *c.s4 {
			t.Errorf("%s: surface 2 says %d, surface 4 says %d", c.field, *c.s2, *c.s4)
		}
	}
}

// The handle a command was typed with is not evidence of anything. Whichever
// surface answered, the record has to come back with the casing the account
// itself publishes, or two reads of one account produce two different records.
func TestTheProfilePageStatesTheCasing(t *testing.T) {
	for _, typed := range []string{"nasa", "NASA", "NaSa"} {
		p, err := ParsePage(UserURL(typed), capture(t, "profile_nasa.html.gz"))
		if err != nil {
			t.Fatalf("ParsePage: %v", err)
		}
		u, err := p.UserFromPage(typed)
		if err != nil {
			t.Fatalf("UserFromPage(%s): %v", typed, err)
		}
		if u.Username != "NASA" {
			t.Errorf("x user %s came back as @%s, want @NASA", typed, u.Username)
		}
		if u.ID != "nasa" {
			t.Errorf("x user %s got id %q, want the lowercased handle", typed, u.ID)
		}
	}
}

// A record that is thin because a window was spent has to say so. Without that
// the reader cannot tell it from a record that is thin because the account has
// nothing more to it, and the two call for different actions.
func TestMissedNamesTheSurfaceAndTheReason(t *testing.T) {
	var m Meta
	m.Miss(2, errors.New("rate limited by X on syndication.profile"))
	m.Miss(2, errors.New("rate limited by X on syndication.profile"))
	m.Miss(4, nil)
	if len(m.Missed) != 1 {
		t.Fatalf("got %v, want one note", m.Missed)
	}
	if m.Missed[0] != "s2: rate limited by X on syndication.profile" {
		t.Errorf("got %q", m.Missed[0])
	}
	if _, err := json.Marshal(m); err != nil {
		t.Fatal(err)
	}
	// A clean record says nothing, so `missed` never turns up empty in the JSON.
	var clean Meta
	b, err := json.Marshal(clean)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "missed") {
		t.Errorf("a record with nothing missing still mentions missed: %s", b)
	}
}

// The misses survive the merge, because the merge is where a two-surface read
// becomes one record and it is the merged record the user sees.
func TestMergeKeepsTheMisses(t *testing.T) {
	web := NewUser("nasa")
	web.Stamp(8, "https://x.com/nasa")
	web.Miss(2, errors.New("rate limited by X on syndication.profile"))
	syn := NewUser("nasa")
	syn.Stamp(2, "https://syndication.twitter.com/srv/timeline-profile/screen-name/nasa")
	syn.Miss(4, errors.New("SearchTimeline needs tier 2"))

	got := MergeUser(web, syn)
	if len(got.Missed) != 2 {
		t.Fatalf("got %v, want both misses", got.Missed)
	}
	if !hasStr(got.Missed, "s2: rate limited by X on syndication.profile") ||
		!hasStr(got.Missed, "s4: SearchTimeline needs tier 2") {
		t.Errorf("got %v", got.Missed)
	}
	// Merging the same record twice does not say it twice.
	got = MergeUser(got, syn)
	if len(got.Missed) != 2 {
		t.Errorf("got %v after a second merge", got.Missed)
	}
}
