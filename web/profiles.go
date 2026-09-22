package web

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/aperturerobotics/fastjson"
	"github.com/paralin/go-steam/steamid"
	"github.com/pkg/errors"
)

// GetPlayerSummaries fetches the current profiles for up to 100 Steam IDs.
func (c *Client) GetPlayerSummaries(ctx context.Context, ids []steamid.SteamId) ([]*PlayerSummary, error) {
	// Preserve each 64-bit identifier exactly, without empty leading entries.
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > 100 {
		return nil, errors.New("GetPlayerSummaries accepts at most 100 Steam IDs")
	}
	values := make([]string, len(ids))
	for i, id := range ids {
		values[i] = strconv.FormatUint(uint64(id), 10)
	}
	response, err := c.Call(ctx, http.MethodGet, "ISteamUser", "GetPlayerSummaries", 2, url.Values{"steamids": {strings.Join(values, ",")}})
	if err != nil {
		return nil, err
	}

	// Decode only the documented player records.
	return decodeList[PlayerSummary, *PlayerSummary](response, "response", "players")
}

// GetPlayerBans fetches ban summaries for up to 100 Steam IDs.
func (c *Client) GetPlayerBans(ctx context.Context, ids []steamid.SteamId) ([]*PlayerBan, error) {
	// Compose a complete comma-separated ID list.
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > 100 {
		return nil, errors.New("GetPlayerBans accepts at most 100 Steam IDs")
	}
	values := make([]string, len(ids))
	for i, id := range ids {
		values[i] = strconv.FormatUint(uint64(id), 10)
	}
	response, err := c.Call(ctx, http.MethodGet, "ISteamUser", "GetPlayerBans", 1, url.Values{"steamids": {strings.Join(values, ",")}})
	if err != nil {
		return nil, err
	}

	// Ban field capitalization follows Valve's response contract.
	return decodeList[PlayerBan, *PlayerBan](response, "players")
}

// GetFriendsList fetches public relationships; private profiles retain HTTP errors.
// filter is "friend" or "all" according to the Steam API.
func (c *Client) GetFriendsList(ctx context.Context, id steamid.SteamId, filter string) ([]*Friend, error) {
	// Let Valve enforce profile access for the current API key.
	response, err := c.Call(ctx, http.MethodGet, "ISteamUser", "GetFriendList", 1, url.Values{"steamid": {strconv.FormatUint(uint64(id), 10)}, "relationship": {filter}})
	if err != nil {
		return nil, err
	}

	// An absent list represents no visible relationships.
	if response.Get("friendslist") == nil {
		return nil, nil
	}
	return decodeList[Friend, *Friend](response, "friendslist", "friends")
}

// ResolveVanityURL resolves a profile alias to its Steam identity.
func (c *Client) ResolveVanityURL(ctx context.Context, alias string) (steamid.SteamId, error) {
	// Request the alias without applying Steam ID parsing heuristics.
	response, err := c.Call(ctx, http.MethodGet, "ISteamUser", "ResolveVanityURL", 1, url.Values{"vanityurl": {alias}})
	if err != nil {
		return 0, err
	}

	// Success 1 is Valve's documented resolution result.
	var result ResolveVanityURLResponse
	if err := Decode(response, &result, "response"); err != nil {
		return 0, err
	}
	if result.Success != 1 {
		return 0, errors.New("Steam vanity URL was not found")
	}
	return steamid.SteamId(result.SteamId), nil
}

// decodeList decodes a required provider array using generated message codecs.
func decodeList[T any, P interface {
	*T
	JSONMessage
}](value *fastjson.Value, path ...string) ([]P, error) {
	// Require the array so malformed envelopes do not look like empty success.
	array := value.Get(path...)
	if array == nil {
		return nil, errors.New("Steam Web API response is missing its list")
	}
	items, err := array.Array()
	if err != nil {
		return nil, errors.New("invalid Steam Web API list")
	}

	// Decode each item before publishing the complete list.
	result := make([]P, 0, len(items))
	for _, item := range items {
		record := P(new(T))
		if err := Decode(item, record); err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return result, nil
}
