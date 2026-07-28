package x

import (
	"context"
	"encoding/json"
	"time"
)

// space.go reads one audio Space off AudioSpaceById.
//
// It is the fourth operation a guest token reaches, which spec 3003 doc 01
// section 4.2 did not know: the bare probe there answers 422 on this route
// rather than the 404 the walled operations answer, and a well-formed request
// with a guest token comes back 200 with the whole record. Measured 2026-07-28
// on 1dRJZEpyjlNGB, a Space that ran in December 2023 and is still readable.
//
// A Space that does not exist is not an error on the wire. X answers 200 with
// `{"data":{"audioSpace":{}}}`, an empty object, the same shape as a Space
// somebody deleted. That is a not-found and this file turns it into one, because
// a caller who typed an id wrong should hear so rather than get a record with
// every field empty.

// gqlSpace is the AudioSpaceById envelope. The names are X's.
type gqlSpace struct {
	Data struct {
		AudioSpace struct {
			Metadata     *spaceMetadata `json:"metadata"`
			Participants *spaceRoster   `json:"participants"`

			// Sharings is the tweets that shared the Space. The capture has it
			// empty with an empty slice_info, and an empty list carries no shape
			// to model, so it is kept raw rather than guessed at.
			Sharings json.RawMessage `json:"sharings"`
		} `json:"audioSpace"`
	} `json:"data"`
}

// spaceMetadata is the Space itself. The times are all milliseconds, and X is
// not consistent about whether they arrive as numbers or as strings: created_at
// is a number and ended_at is a string in the same object.
type spaceMetadata struct {
	RestID   string `json:"rest_id"`
	Title    string `json:"title"`
	State    string `json:"state"`
	MediaKey string `json:"media_key"`

	CreatedAt      json.Number `json:"created_at"`
	UpdatedAt      json.Number `json:"updated_at"`
	ScheduledStart json.Number `json:"scheduled_start"`
	StartedAt      json.Number `json:"started_at"`
	EndedAt        json.Number `json:"ended_at"`

	// ReplayStartTime is the odd one out: it is milliseconds like the rest, but
	// milliseconds of duration rather than milliseconds since the epoch. It came
	// back 166 on one Space and 1849 on another, which is where the replay picks
	// up rather than a moment in 1970. Reading it as a timestamp would put every
	// replay two seconds after midnight on 1 January 1970.
	ReplayStartTime int `json:"replay_start_time"`

	TotalLiveListeners  int  `json:"total_live_listeners"`
	TotalReplayWatched  int  `json:"total_replay_watched"`
	AvailableForReplay  bool `json:"is_space_available_for_replay"`
	AvailableForClip    bool `json:"is_space_available_for_clipping"`
	IsLocked            bool `json:"is_locked"`
	IsEmployeeOnly      bool `json:"is_employee_only"`
	DisallowJoin        bool `json:"disallow_join"`
	ConversationControl int  `json:"conversation_controls"`
	NarrowCastType      int  `json:"narrow_cast_space_type"`

	CreatorResults struct {
		Result *gqlUserResult `json:"result"`
	} `json:"creator_results"`
}

// spaceRoster is who was in the room. Total is X's own count and is not the sum
// of the three lists: a finished Space reports the roster it kept, which for
// 1dRJZEpyjlNGB was two admins and one speaker with total 0.
type spaceRoster struct {
	Admins    []spaceMember `json:"admins"`
	Speakers  []spaceMember `json:"speakers"`
	Listeners []spaceMember `json:"listeners"`
	Total     int           `json:"total"`
}

// spaceMember is one participant. It carries a flattened profile alongside a
// user_results, and on the measured capture the user_results.result is a stub:
// three flags, an empty legacy, no screen_name. So the flat fields are the
// participant, and the nested record only fills what it happens to have.
//
// The one thing worth having in there is the rest_id, and it sits on the wrapper
// rather than inside the result, which is easy to miss. It is what makes a roster
// row a node: without it a participant is a handle and a picture, with it you can
// look the account up and keep walking.
//
// The creator is the exception. Its user_results.result is a whole profile, which
// is why it is read through the ordinary user decoder and the roster is not.
type spaceMember struct {
	Handle      string `json:"twitter_screen_name"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
	Verified    bool   `json:"is_verified"`

	// PeriscopeUserID is the id this account had in Periscope, which is where
	// Spaces came from. It is not a twitter id and does not resolve anywhere on
	// x.com, so it is read and dropped rather than published as one.
	PeriscopeUserID string `json:"periscope_user_id"`

	MutedByAdmin bool `json:"is_muted_by_admin"`
	MutedByGuest bool `json:"is_muted_by_guest"`

	// Start is when a speaker took the microphone, in milliseconds. Admins do
	// not have one.
	Start json.Number `json:"start"`

	UserResults struct {
		RestID string         `json:"rest_id"`
		Result *gqlUserResult `json:"result"`
	} `json:"user_results"`
}

// SpaceByID reads one Space.
func (g *GraphQL) SpaceByID(ctx context.Context, id string) (*Space, error) {
	b, src, err := g.get(ctx, "AudioSpaceById", map[string]any{
		"id":                      id,
		"isMetatagsQuery":         false,
		"withReplays":             true,
		"withListeners":           true,
		"withDownvotePerspective": false,
	})
	if err != nil {
		return nil, err
	}
	s, err := parseSpace(b, id)
	if err != nil {
		return nil, err
	}
	s.Stamp(g.surface(), src)
	for _, u := range append(append([]*User{s.Creator}, s.Hosts...), s.Speakers...) {
		stampUser(u, g.surface(), src)
	}
	return s, nil
}

// parseSpace turns the response into a record, or into a not-found.
func parseSpace(b []byte, id string) (*Space, error) {
	var env gqlSpace
	if err := json.Unmarshal(b, &env); err != nil {
		return nil, err
	}
	m := env.Data.AudioSpace.Metadata
	if m == nil || m.RestID == "" {
		return nil, &NotFoundError{Kind: "space", Ref: id}
	}

	s := &Space{}
	s.Identify(KindSpace, m.RestID)
	s.Title = m.Title
	s.State = m.State
	s.MediaKey = m.MediaKey
	s.CreatedAt = msTime(m.CreatedAt)
	s.UpdatedAt = msTime(m.UpdatedAt)
	s.ScheduledStart = msTime(m.ScheduledStart)
	s.StartedAt = msTime(m.StartedAt)
	s.EndedAt = msTime(m.EndedAt)
	s.ReplayStart = m.ReplayStartTime
	s.LiveListeners = m.TotalLiveListeners
	s.ReplayWatched = m.TotalReplayWatched
	s.Replayable = m.AvailableForReplay
	s.Clippable = m.AvailableForClip
	s.Locked = m.IsLocked
	s.EmployeeOnly = m.IsEmployeeOnly
	s.DisallowJoin = m.DisallowJoin

	if r := m.CreatorResults.Result; r != nil {
		s.Creator = r.toUser()
	}
	if p := env.Data.AudioSpace.Participants; p != nil {
		s.Hosts = spaceUsers(p.Admins)
		s.Speakers = spaceUsers(p.Speakers)
		s.Listeners = spaceUsers(p.Listeners)
		s.Participants = p.Total
	}
	return s, nil
}

// spaceUsers turns a roster into user records.
//
// The flat fields lead, because they are the ones that are populated, and a
// member with no handle is dropped: a profile with no identity is a row that
// cannot be looked up, joined, or crawled from.
func spaceUsers(members []spaceMember) []*User {
	var out []*User
	for _, m := range members {
		if m.Handle == "" {
			continue
		}
		u := NewUser(m.Handle)
		u.Name = m.DisplayName
		u.ProfileImage = m.AvatarURL
		u.Verified = m.Verified
		u.RestID = m.UserResults.RestID
		if r := m.UserResults.Result; r != nil {
			// Whatever the stub does carry, which today is the blue check and
			// nothing else. Merge rather than overwrite, so the day X starts
			// sending the whole profile here this reads it without a change.
			u = MergeUser(u, r.toUser())
		}
		out = append(out, u)
	}
	return out
}

// msTime reads one of X's millisecond timestamps. They arrive as numbers on some
// keys and as quoted strings on others in the same object, which json.Number
// handles, and a zero or an absent one is the zero time rather than 1970.
func msTime(n json.Number) time.Time {
	if n == "" {
		return time.Time{}
	}
	ms, err := n.Int64()
	if err != nil || ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

// Space reads one audio Space. It is a GraphQL read at either tier: a guest
// token reaches AudioSpaceById, which makes it one of the four operations that
// do not need your session.
func (e *Engine) Space(ctx context.Context, id string) (*Space, error) {
	if !e.canGraphQL() {
		return nil, needGraphQL("reading a Space")
	}
	return e.g.SpaceByID(ctx, id)
}
