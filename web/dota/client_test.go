package dota_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/paralin/go-steam/web"
	"github.com/paralin/go-steam/web/dota"
)

// TestHistoryPagination checks date bounds, exact cursors, and bounded collection.
func TestHistoryPagination(t *testing.T) {
	// Serve two pages whose second page contains more rows than requested.
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("date_min") != "100" || r.URL.Query().Get("date_max") != "200" {
			t.Error("date bounds were conflated")
		}
		if calls == 1 {
			io.WriteString(w, `{"result":{"status":1,"num_results":2,"results_remaining":2,"matches":[{"match_id":9007199254740995},{"match_id":9007199254740994}]}}`)
			return
		}
		if r.URL.Query().Get("start_at_match_id") != "9007199254740993" || r.URL.Query().Get("matches_requested") != "1" {
			t.Error("pagination cursor or count lost")
		}
		io.WriteString(w, `{"result":{"status":1,"num_results":2,"results_remaining":0,"matches":[{"match_id":9007199254740993},{"match_id":9007199254740992}]}}`)
	}))
	defer server.Close()
	api, err := web.NewClient(web.Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	client := dota.NewClient(api, 570)

	// Collection stops at the requested count and never underflows its limit.
	matches, err := client.GetMatchHistory(t.Context(), dota.MatchFilter{DateMin: time.Unix(100, 0), DateMax: time.Unix(200, 0), MatchesRequested: 3})
	if err != nil || len(matches) != 3 || calls != 2 || matches[2].MatchId != 9007199254740993 {
		t.Fatalf("history: %v %v calls=%d", matches, err, calls)
	}
}

// TestHistoryStall preserves provider failures and refuses non-progressing pages.
func TestHistoryStall(t *testing.T) {
	for _, body := range []string{`{"result":{"status":15}}`, `{"result":{"status":1,"results_remaining":1,"matches":[]}}`, `{"result":{"status":1,"results_remaining":1,"matches":[{"match_id":100}]}}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }))
		api, err := web.NewClient(web.Config{BaseURL: server.URL})
		if err != nil {
			t.Fatal(err)
		}
		_, err = dota.NewClient(api, 570).GetMatchHistory(t.Context(), dota.MatchFilter{StartAtMatchID: 100})
		server.Close()
		if err == nil {
			t.Fatal("invalid history was accepted", body)
		}
	}
}

// TestMatchDetails retains match fields and rejects a missing requested match.
func TestMatchDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"result":{"match_id":9007199254740993,"match_seq_num":9007199254740994,"radiant_win":true,"duration":42,"players":[{"account_id":73,"player_slot":128,"hero_id":1,"ability_upgrades":[{"ability":5,"time":3,"level":1}]}]}}`)
	}))
	defer server.Close()
	api, err := web.NewClient(web.Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	result, err := dota.NewClient(api, 570).GetMatchDetails(t.Context(), 9007199254740993)
	if err != nil || result.MatchSequenceNo != 9007199254740994 || !dota.IsDire(result.Players[0].PlayerSlot) || result.Players[0].Abilities[0].Id != 5 {
		t.Fatalf("details: %v %v", result, err)
	}
}
