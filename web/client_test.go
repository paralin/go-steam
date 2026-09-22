package web_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/paralin/go-steam/steamid"
	"github.com/paralin/go-steam/web"
)

// newClient serves bounded local requests through the public client contract.
func newClient(t *testing.T, handler http.HandlerFunc, limit int64) *web.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := web.NewClient(web.Config{BaseURL: server.URL, APIKey: "test-key", MaxResponseBytes: limit})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// TestProfiles preserves exact IDs and public response field names.
func TestProfiles(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		// Reject the old leading-comma ID encoding at the HTTP boundary.
		if r.URL.Query().Get("key") != "test-key" {
			t.Error("missing key")
		}
		switch r.URL.Path {
		case "/ISteamUser/GetPlayerSummaries/v2/":
			if r.URL.Query().Get("steamids") != "76561198029304414,76561198000000001" {
				t.Error("incorrect ID list")
			}
			io.WriteString(w, `{"response":{"players":[{"steamid":"76561198029304414","personaname":"Example","avatarfull":"https://example.invalid/avatar"}]}}`)
		case "/ISteamUser/GetPlayerBans/v1/":
			if r.URL.Query().Get("steamids") != "76561198029304414" {
				t.Error("incorrect ban ID list")
			}
			io.WriteString(w, `{"players":[{"SteamId":"76561198029304414","VACBanned":true,"NumberOfVACBans":2}]}`)
		case "/ISteamUser/GetFriendList/v1/":
			io.WriteString(w, `{"friendslist":{"friends":[{"steamid":"76561198000000001","relationship":"friend","friend_since":123}]}}`)
		case "/ISteamUser/ResolveVanityURL/v1/":
			if r.URL.Query().Get("vanityurl") != "example" {
				t.Error("alias lost")
			}
			io.WriteString(w, `{"response":{"success":1,"steamid":"76561198029304414"}}`)
		default:
			t.Error("unexpected path", r.URL.Path)
			w.WriteHeader(404)
		}
	}, 0)

	// Exercise each typed wrapper, including IDs larger than 2^53.
	id := steamid.SteamId(76561198029304414)
	profiles, err := client.GetPlayerSummaries(t.Context(), []steamid.SteamId{id, 76561198000000001})
	if err != nil || len(profiles) != 1 || profiles[0].SteamId != uint64(id) || profiles[0].PersonaName != "Example" {
		t.Fatalf("profiles: %v %v", profiles, err)
	}
	bans, err := client.GetPlayerBans(t.Context(), []steamid.SteamId{id})
	if err != nil || len(bans) != 1 || !bans[0].VacBanned || bans[0].NumberOfVacBans != 2 {
		t.Fatalf("bans: %v %v", bans, err)
	}
	friends, err := client.GetFriendsList(t.Context(), id, "friend")
	if err != nil || len(friends) != 1 || friends[0].FriendSince != 123 {
		t.Fatalf("friends: %v %v", friends, err)
	}
	resolved, err := client.ResolveVanityURL(t.Context(), "example")
	if err != nil || resolved != id {
		t.Fatalf("alias: %v %v", resolved, err)
	}
}

// TestEconomy ports numeric/string attributes, keyed classes, schemas, and prices.
func TestEconomy(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/IStoreService/GetAppList/v1/":
			io.WriteString(w, `{"response":{"apps":[{"appid":570,"name":"Dota 2"}]}}`)
		case "/ISteamApps/UpToDateCheck/v1/":
			io.WriteString(w, `{"response":{"success":true,"up_to_date":true,"required_version":123,"version_is_listable":true}}`)
		case "/ISteamApps/GetServersAtAddress/v1/":
			io.WriteString(w, `{"response":{"success":true,"servers":[{"addr":"127.0.0.1:27015","appid":440,"region":0,"secure":true}]}}`)
		case "/IEconItems_440/GetPlayerItems/v1/":
			io.WriteString(w, `{"result":{"status":1,"num_backpack_slots":840,"items":[{"id":9007199254740993,"inventory":19,"attributes":[{"defindex":8,"value":1049511890,"float_value":0.27},{"defindex":147,"value":"models/example.mdl"}]}]}}`)
		case "/IEconItems_440/GetSchema/v1/":
			io.WriteString(w, `{"result":{"status":1,"items_game_url":"https://example.invalid/items","qualityNames":{"normal":"Normal"},"items":[{"defindex":5,"image_inventory":null,"used_by_classes":["scout"]}]}}`)
		case "/ISteamEconomy/GetAssetPrices/v1/":
			io.WriteString(w, `{"result":{"success":true,"assets":[{"name":"4004","prices":{"USD":99},"tags":["hat"]}]}}`)
		case "/ISteamEconomy/GetAssetClassInfo/v1/":
			io.WriteString(w, `{"result":{"999":{"market_hash_name":"Wrong"},"123":{"market_hash_name":"Correct","tradable":"1"},"success":true}}`)
		default:
			t.Error("unexpected path", r.URL.Path)
			w.WriteHeader(404)
		}
	}, 0)

	// Decode the complete inherited endpoint set through real local HTTP.
	apps, err := client.GetAppList(t.Context())
	if err != nil || len(apps) != 1 || apps[0].Name != "Dota 2" {
		t.Fatalf("apps: %v %v", apps, err)
	}
	version, err := client.GetCurrentAppVersion(t.Context(), 440)
	if err != nil || version != 123 {
		t.Fatalf("version: %v %v", version, err)
	}
	servers, err := client.GetServerInfo(t.Context(), netip.MustParseAddr("127.0.0.1"))
	if err != nil || len(servers) != 1 || !servers[0].VacSecured {
		t.Fatalf("servers: %v %v", servers, err)
	}
	items, err := client.GetPlayerItems(t.Context(), 1, 440)
	if err != nil || len(items.Items) != 1 {
		t.Fatalf("inventory: %v %v", items, err)
	}
	item := items.Items[0]
	if item.Id != 9007199254740993 || item.Position() != 19 || item.Attributes[0].Value.GetNumberValue() != 1049511890 || item.Attributes[1].Value.GetStringValue() != "models/example.mdl" {
		t.Fatal("inventory values lost", item)
	}
	schema, err := client.GetSchema(t.Context(), 440, "en")
	if err != nil || schema.Item(5) == nil || schema.QualityNames["normal"] != "Normal" {
		t.Fatalf("schema: %v %v", schema, err)
	}
	prices, err := client.GetAssetPrices(t.Context(), 440, "en", "USD")
	if err != nil || len(prices) != 1 || prices[0].Defindex != 4004 || prices[0].Prices["USD"] != 99 || !prices[0].HasTag("hat") {
		t.Fatalf("prices: %v %v", prices, err)
	}
	info, err := client.GetAssetClassInfo(t.Context(), 440, 123, "en")
	if err != nil || info.MarketHashName != "Correct" || info.ClassId != "123" {
		t.Fatalf("class: %v %v", info, err)
	}
}

// TestTradeRequests preserves item descriptions and uses POST for mutations.
func TestTradeRequests(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/IEconService/GetTradeOffers/v1/":
			io.WriteString(w, `{"response":{"trade_offers_sent":[{"tradeofferid":"9007199254740993","items_to_give":[{"appid":"440","classid":"123","instanceid":"0","assetid":"9007199254740995"}]}],"descriptions":[{"appid":440,"classid":"123","instanceid":"0","market_hash_name":"Hat"}],"next_cursor":22}}`)
		case "/IEconService/GetTradeOffer/v1/":
			io.WriteString(w, `{"response":{"offer":{"tradeofferid":"9007199254740993","trade_offer_state":2}}}`)
		case "/IEconService/CancelTradeOffer/v1/", "/IEconService/DeclineTradeOffer/v1/":
			if r.Method != http.MethodPost || r.URL.RawQuery != "" {
				t.Error("mutation must use form POST")
			}
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.PostForm.Get("key") != "test-key" || r.PostForm.Get("tradeofferid") != "9007199254740993" {
				t.Error("mutation form lost identity")
			}
			io.WriteString(w, `{"response":{}}`)
		default:
			t.Error("unexpected path", r.URL.Path)
			w.WriteHeader(404)
		}
	}, 0)

	// Match descriptions by all three identity fields while retaining exact IDs.
	offers, err := client.GetTradeOffers(t.Context(), web.TradeFilter{Sent: true, Descriptions: true})
	if err != nil || len(offers.Sent) != 1 || offers.NextCursor != 22 {
		t.Fatalf("offers: %v %v", offers, err)
	}
	asset := offers.Sent[0].ToGive[0]
	if asset.AssetId != 9007199254740995 || asset.MarketHashName != "Hat" {
		t.Fatal("trade asset lost", asset)
	}
	offer, err := client.GetTradeOffer(t.Context(), 9007199254740993, "en")
	if err != nil || offer.TradeOfferId != 9007199254740993 {
		t.Fatalf("offer: %v %v", offer, err)
	}
	if err := client.CancelTradeOffer(t.Context(), 9007199254740993); err != nil {
		t.Fatal(err)
	}
	if err := client.DeclineTradeOffer(t.Context(), 9007199254740993); err != nil {
		t.Fatal(err)
	}
}

// TestRequestFailures checks cancellation, response limits, and credential-safe errors.
func TestRequestFailures(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
	}{
		{"status", "test-key", 429}, {"large", strings.Repeat("x", 65), 200}, {"malformed", "{test-key", 200}, {"redirect", "", 302},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "3")
				w.Header().Set("Location", "https://example.invalid/?key=test-key")
				w.WriteHeader(test.status)
				io.WriteString(w, test.body)
			}, 64)
			_, err := client.GetAppList(t.Context())
			if err == nil || strings.Contains(err.Error(), "test-key") {
				t.Fatalf("unsafe error: %v", err)
			}
			var requestError *web.RequestError
			if !errors.As(err, &requestError) {
				t.Fatal("missing typed request error", err)
			}
			if test.status == 429 && (requestError.StatusCode != 429 || requestError.RetryAfter != "3") {
				t.Fatal("rate limit information lost")
			}
		})
	}

	// An in-flight request ends through its context without an application poller.
	entered := make(chan struct{})
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done() }, 0)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := client.GetAppList(ctx); done <- err }()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}
