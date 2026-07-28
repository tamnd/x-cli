package x

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// The capture is 1dRJZEpyjlNGB, a Space that ran on 5 December 2023 and timed
// out rather than being ended. It is a finished Space, which is the harder case:
// the roster it kept disagrees with its own total, and half its timestamps
// arrive as strings.
func TestSpaceReadsAFinishedSpace(t *testing.T) {
	s, err := parseSpace([]byte(capture(t, "s4_space_1dRJZEpyjlNGB.json.gz")), "1dRJZEpyjlNGB")
	if err != nil {
		t.Fatalf("parseSpace: %v", err)
	}
	if s.ID != "1dRJZEpyjlNGB" {
		t.Errorf("id %q", s.ID)
	}
	if s.Kind != KindSpace || s.URI != "x://space/1dRJZEpyjlNGB" {
		t.Errorf("kind %q uri %q", s.Kind, s.URI)
	}
	if s.URL != "https://x.com/i/spaces/1dRJZEpyjlNGB" {
		t.Errorf("url %q", s.URL)
	}
	if s.Title != "Cyber security with @mjkabir #USABizParty" {
		t.Errorf("title %q", s.Title)
	}
	// Not normalised to "ended". A Space that timed out and a Space the host
	// closed are different events and only X knows which happened.
	if s.State != "TimedOut" {
		t.Errorf("state %q, want X's own word", s.State)
	}
	if s.MediaKey != "28_1732090503993360384" {
		t.Errorf("media key %q", s.MediaKey)
	}
	if !s.Replayable {
		t.Error("the capture is available for replay")
	}
	if s.LiveListeners != 5 || s.ReplayWatched != 17 {
		t.Errorf("%d live, %d replays, want 5 and 17", s.LiveListeners, s.ReplayWatched)
	}
}

// created_at arrives as a number and ended_at as a quoted string, in the same
// object. A reader that assumes either one drops half the timeline.
func TestSpaceReadsTimestampsInBothShapes(t *testing.T) {
	s, err := parseSpace([]byte(capture(t, "s4_space_1dRJZEpyjlNGB.json.gz")), "1dRJZEpyjlNGB")
	if err != nil {
		t.Fatalf("parseSpace: %v", err)
	}
	for _, c := range []struct {
		name string
		got  time.Time
		want string
	}{
		{"created_at (a number)", s.CreatedAt, "2023-12-05T17:32:11Z"},
		{"scheduled_start (a number)", s.ScheduledStart, "2023-12-05T18:00:00Z"},
		{"started_at (a number)", s.StartedAt, "2023-12-05T18:00:27Z"},
		{"ended_at (a string)", s.EndedAt, "2023-12-05T18:08:08Z"},
	} {
		if got := c.got.UTC().Format(time.RFC3339); got != c.want {
			t.Errorf("%s is %s, want %s", c.name, got, c.want)
		}
	}
	if !s.StartedAt.After(s.CreatedAt) || !s.EndedAt.After(s.StartedAt) {
		t.Error("the Space did not happen in order, so the milliseconds are being read as seconds")
	}
}

// The roster's user_results.result is a stub on this capture: three flags, an
// empty legacy, no screen_name. So the flat fields are the participant, and a
// reader that trusted the nested record would return a roster of blanks. It did.
func TestSpaceRosterComesOffTheFlatFields(t *testing.T) {
	s, err := parseSpace([]byte(capture(t, "s4_space_1dRJZEpyjlNGB.json.gz")), "1dRJZEpyjlNGB")
	if err != nil {
		t.Fatalf("parseSpace: %v", err)
	}
	if got := handles(s.Hosts); len(got) != 2 || got[0] != "USABizparty" || got[1] != "SchellerAnna" {
		t.Errorf("hosts %v, want the two admins", got)
	}
	if got := handles(s.Speakers); len(got) != 1 || got[0] != "mjkabir" {
		t.Errorf("speakers %v, want just @mjkabir", got)
	}
	if s.Speakers[0].Name == "" || s.Speakers[0].ProfileImage == "" {
		t.Errorf("@mjkabir came back as %q with avatar %q", s.Speakers[0].Name, s.Speakers[0].ProfileImage)
	}
	// The handle keeps the case X sent, the address does not: a handle is
	// case-insensitive and two spellings of one account would be two rows.
	for _, u := range append(append([]*User{}, s.Hosts...), s.Speakers...) {
		if want := "https://x.com/" + strings.ToLower(u.Username); u.URL != want {
			t.Errorf("%s addresses %q, want %q", u.Username, u.URL, want)
		}
	}
}

// The numeric id is the one thing worth having out of the stub, and it is not in
// the stub: it sits on the user_results wrapper next to it. Without it a
// participant is a handle and a picture and the roster is a dead end, so this is
// what makes a Space walkable.
func TestSpaceRosterCarriesTheNumericID(t *testing.T) {
	s, err := parseSpace([]byte(capture(t, "s4_space_1dRJZEpyjlNGB.json.gz")), "1dRJZEpyjlNGB")
	if err != nil {
		t.Fatalf("parseSpace: %v", err)
	}
	want := map[string]string{
		"USABizparty":  "3706636217",
		"SchellerAnna": "2293386248",
		"mjkabir":      "14047651",
	}
	for _, u := range append(append([]*User{}, s.Hosts...), s.Speakers...) {
		if u.RestID != want[u.Username] {
			t.Errorf("@%s has id %q, want %q", u.Username, u.RestID, want[u.Username])
		}
	}
}

// The creator is the one profile that does arrive whole, so it is read through
// the ordinary user decoder and has the numeric id the roster never does.
func TestSpaceCreatorIsAWholeProfile(t *testing.T) {
	s, err := parseSpace([]byte(capture(t, "s4_space_1dRJZEpyjlNGB.json.gz")), "1dRJZEpyjlNGB")
	if err != nil {
		t.Fatalf("parseSpace: %v", err)
	}
	if s.Creator == nil {
		t.Fatal("no creator on a Space somebody created")
	}
	if s.Creator.Username != "USABizparty" || s.Creator.RestID != "3706636217" {
		t.Errorf("creator is @%s (%s), want @USABizparty (3706636217)", s.Creator.Username, s.Creator.RestID)
	}
	if s.Creator.Metrics.Followers == nil {
		t.Error("the creator has no follower count, so the whole profile was not read")
	}
}

// X's own count of the room is not the length of the lists, and publishing the
// length as the count would be inventing a number.
func TestSpaceParticipantCountIsXsAndNotTheListLength(t *testing.T) {
	s, err := parseSpace([]byte(capture(t, "s4_space_1dRJZEpyjlNGB.json.gz")), "1dRJZEpyjlNGB")
	if err != nil {
		t.Fatalf("parseSpace: %v", err)
	}
	if n := len(s.Hosts) + len(s.Speakers) + len(s.Listeners); s.Participants == n {
		t.Skip("the capture's total now agrees with its roster, so this no longer distinguishes anything")
	}
	if s.Participants != 0 {
		t.Errorf("participants %d, want X's total of 0 on a finished Space", s.Participants)
	}
}

// A second capture, of a Space with a real audience: 3 admins, 14 speakers, 5253
// people live and 44636 replays. It carries three things the first one does not,
// and all three were found by the unmodeled-keys test rather than by reading the
// docs, because there are no docs.
func TestSpaceReadsTheKeysOnlyABusySpaceSends(t *testing.T) {
	s, err := parseSpace([]byte(capture(t, "s4_space_1MnxnMDeQLeJO.json.gz")), "1MnxnMDeQLeJO")
	if err != nil {
		t.Fatalf("parseSpace: %v", err)
	}
	// replay_start_time is a duration, not a timestamp. 1849 is a second and a
	// half into the recording, which is where the replay picks up.
	if s.ReplayStart != 1849 {
		t.Errorf("replay starts at %d ms, want 1849", s.ReplayStart)
	}
	// Replay on, clipping off. They are two switches and this Space is the proof
	// that one does not imply the other, which is why both are read.
	if !s.Replayable || s.Clippable {
		t.Errorf("replayable %v clippable %v, want true and false", s.Replayable, s.Clippable)
	}
	if len(s.Hosts) != 3 || len(s.Speakers) != 14 {
		t.Errorf("%d hosts and %d speakers, want 3 and 14", len(s.Hosts), len(s.Speakers))
	}
	if s.LiveListeners != 5253 || s.ReplayWatched != 44636 {
		t.Errorf("%d live and %d replays, want 5253 and 44636", s.LiveListeners, s.ReplayWatched)
	}
	// Every speaker in a room of fourteen still has an id, which is the whole
	// case for reading the wrapper rather than the stub inside it.
	for _, u := range s.Speakers {
		if u.RestID == "" {
			t.Errorf("@%s has no numeric id", u.Username)
		}
	}
}

// A Space that does not exist comes back 200 with an empty object, which is a
// not-found wearing a success. `x space <typo>` has to say so.
func TestAnEmptySpaceIsNotFound(t *testing.T) {
	_, err := parseSpace([]byte(`{"data":{"audioSpace":{}}}`), "1YpKkZWlvVeGj")
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("got %v, want a NotFoundError", err)
	}
	if nf.Kind != "space" || nf.Ref != "1YpKkZWlvVeGj" {
		t.Errorf("got %s %q, want the space and the id asked for", nf.Kind, nf.Ref)
	}
}

func handles(us []*User) []string {
	out := make([]string, len(us))
	for i, u := range us {
		out[i] = u.Username
	}
	return out
}
