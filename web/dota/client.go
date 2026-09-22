// Package dota reads Dota match history and match details through Steam's Web API.
package dota

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/paralin/go-steam/web"
	"github.com/pkg/errors"
)

// Client binds an application's Dota endpoints to a shared Web API transport.
type Client struct {
	// web owns HTTP settings, credentials, and response limits.
	web *web.Client
	// iface selects retail Dota (570) or a caller-selected test application.
	iface string
}

// NewClient selects the application's Dota Web API; retail Dota uses app 570.
func NewClient(client *web.Client, app uint32) *Client {
	return &Client{web: client, iface: "IDOTA2Match_" + strconv.FormatUint(uint64(app), 10)}
}

// MatchFilter selects matches; zero values leave the corresponding filter unset.
type MatchFilter struct {
	// PlayerName filters the historical player name where supported by Valve.
	PlayerName string
	// HeroID filters matches containing this hero.
	HeroID uint32
	// Skill selects Valve's skill bracket; zero selects all brackets.
	Skill uint32
	// GameMode optionally selects Valve's numeric game mode, including zero.
	GameMode *uint32
	// DateMin is the inclusive lower match-start time.
	DateMin time.Time
	// DateMax is the inclusive upper match-start time.
	DateMax time.Time
	// MinPlayers requires at least this many human participants.
	MinPlayers uint32
	// AccountID selects one Steam account's visible match history.
	AccountID uint32
	// LeagueID selects one league.
	LeagueID uint32
	// StartAtMatchID is the inclusive pagination cursor.
	StartAtMatchID uint64
	// MatchesRequested limits the collected matches; zero collects all visible pages.
	MatchesRequested uint32
}

// GetMatchHistoryPage returns a single page with Valve's pagination counts.
func (c *Client) GetMatchHistoryPage(ctx context.Context, filter MatchFilter) (*HistoryResult, error) {
	// Encode optional filters without conflating the two date bounds.
	values := url.Values{}
	if filter.PlayerName != "" {
		values.Set("player_name", filter.PlayerName)
	}
	if filter.GameMode != nil {
		values.Set("game_mode", strconv.FormatUint(uint64(*filter.GameMode), 10))
	}
	if filter.HeroID != 0 {
		values.Set("hero_id", strconv.FormatUint(uint64(filter.HeroID), 10))
	}
	if filter.Skill != 0 {
		values.Set("skill", strconv.FormatUint(uint64(filter.Skill), 10))
	}
	if !filter.DateMin.IsZero() {
		values.Set("date_min", strconv.FormatInt(filter.DateMin.Unix(), 10))
	}
	if !filter.DateMax.IsZero() {
		values.Set("date_max", strconv.FormatInt(filter.DateMax.Unix(), 10))
	}
	if filter.MinPlayers != 0 {
		values.Set("min_players", strconv.FormatUint(uint64(filter.MinPlayers), 10))
	}
	if filter.AccountID != 0 {
		values.Set("account_id", strconv.FormatUint(uint64(filter.AccountID), 10))
	}
	if filter.LeagueID != 0 {
		values.Set("league_id", strconv.FormatUint(uint64(filter.LeagueID), 10))
	}
	if filter.StartAtMatchID != 0 {
		values.Set("start_at_match_id", strconv.FormatUint(filter.StartAtMatchID, 10))
	}
	if filter.MatchesRequested != 0 {
		values.Set("matches_requested", strconv.FormatUint(uint64(min(filter.MatchesRequested, 100)), 10))
	}
	response, err := c.web.Call(ctx, http.MethodGet, c.iface, "GetMatchHistory", 1, values)
	if err != nil {
		return nil, err
	}

	// Permission failures remain distinct from a completed empty page.
	result := &HistoryResult{}
	if err := web.Decode(response, result, "result"); err != nil {
		return nil, err
	}
	if result.Status != 1 {
		return nil, errors.Errorf("Dota match history failed with status %d", result.Status)
	}
	return result, nil
}

// GetMatchHistory collects pages until the requested count or end of history.
// The caller's context bounds the entire operation; partial results accompany errors.
func (c *Client) GetMatchHistory(ctx context.Context, filter MatchFilter) ([]*Match, error) {
	var matches []*Match
	for {
		// Request only the remaining count without unsigned subtraction underflow.
		page, err := c.GetMatchHistoryPage(ctx, filter)
		if err != nil {
			return matches, err
		}
		count := len(page.Matches)
		if filter.MatchesRequested != 0 {
			count = min(count, int(filter.MatchesRequested))
		}
		matches = append(matches, page.Matches[:count]...)
		if filter.MatchesRequested != 0 {
			filter.MatchesRequested -= uint32(count)
			if filter.MatchesRequested == 0 {
				return matches, nil
			}
		}
		if page.ResultsRemaining == 0 {
			return matches, nil
		}

		// A stalled cursor must not loop forever or index an empty result page.
		if len(page.Matches) == 0 {
			return matches, errors.New("Dota history pagination returned no matches with results remaining")
		}
		last := page.Matches[len(page.Matches)-1].MatchId
		if last == 0 || (filter.StartAtMatchID != 0 && last >= filter.StartAtMatchID) {
			return matches, errors.New("Dota history pagination did not advance")
		}
		filter.StartAtMatchID = last - 1
	}
}

// GetMatchDetails fetches the complete statistics retained for one match.
func (c *Client) GetMatchDetails(ctx context.Context, id uint64) (*MatchResult, error) {
	// Match IDs remain exact across the request and generated JSON decoder.
	response, err := c.web.Call(ctx, http.MethodGet, c.iface, "GetMatchDetails", 1, url.Values{"match_id": {strconv.FormatUint(id, 10)}})
	if err != nil {
		return nil, err
	}

	// An API error object has no matching match identity.
	result := &MatchResult{}
	if err := web.Decode(response, result, "result"); err != nil {
		return nil, err
	}
	if result.MatchId != id {
		return nil, errors.New("Steam returned no details for the requested Dota match")
	}
	return result, nil
}

// IsDire reports the team bit of a Dota player-slot value.
func IsDire(slot uint32) bool { return slot&128 != 0 }

// Position returns the player-slot index with its team bit removed.
func Position(slot uint32) uint32 { return slot & 127 }
