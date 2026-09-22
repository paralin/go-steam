# Steam Web API

`github.com/paralin/go-steam/web` is an HTTP client for Valve's Steam Web API.
It does not open a Steam network connection or require a running Steam client.

```go
client, err := web.NewClient(web.Config{APIKey: apiKey})
if err != nil {
    return err
}

profiles, err := client.GetPlayerSummaries(ctx, ids)
```

## Operations

| Area | Methods |
| --- | --- |
| Accounts | `GetPlayerSummaries`, `GetPlayerBans`, `GetFriendsList`, `ResolveVanityURL` |
| Applications | `GetAppListPage`, `GetAppList`, `CheckAppVersion`, `GetCurrentAppVersion`, `IsAppUpToDate` |
| Servers | `GetServerInfo` |
| Economy | `GetPlayerItems`, `GetSchema`, `GetAssetPrices`, `GetAssetClassInfo` |
| Trade offers | `GetTradeOffers`, `GetTradeOffer`, `CancelTradeOffer`, `DeclineTradeOffer` |
| Dota | `dota.Client.GetMatchHistoryPage`, `GetMatchHistory`, `GetMatchDetails` |

`Call` exposes other Web API methods through the same bounded transport and
returns a parsed `fastjson.Value`. `Decode` uses generated JSON codecs to
replace a typed result. Account, item, match, and trade identifiers retain
64-bit precision whether Valve sends them as numbers or strings.

## Pagination and caching

The app directory uses
[`IStoreService/GetAppList`](https://partner.steamgames.com/doc/webapi/IStoreService#GetAppList).
Valve deprecated the old single-response `ISteamApps/GetAppList` endpoint.
A Web API key is required. `GetAppListPage` accepts a cursor and modification
timestamp for controlled refreshes; `GetAppList` collects all categories until
Valve reports the end. Cache the directory and use `ModifiedSince` for updates.

`GetTradeOffers` returns one page with `NextCursor` and item descriptions.
Dota history has separate page and collection methods. The collection method's
`MatchesRequested` limits the total; zero collects all visible pages. A single
context bounds the full operation. Collections return partial results with an
error if a request fails or a cursor stops advancing.

The client does not cache, retry, or impose an application-wide rate limiter.
Those policies belong to the application sharing the client. Economy schemas
and inventory availability depend on the selected game and Valve's permissions.

## Transport and errors

`Config` accepts an API key, origin, HTTP client, and maximum response size.
Defaults are Valve's public HTTPS origin, a 30-second HTTP timeout, and a 64 MiB
response limit. A supplied HTTP client retains its own timeout and transport.
Each request has a context, closes its response body, and disables redirects
so an API key cannot follow a redirect to another origin.

`RequestError` exposes the operation, HTTP status, and `Retry-After` header.
Its printable message omits credentials, URLs, and response bodies. Network
errors remain available through `errors.Is` and `errors.As`, including context
cancellation. Cancel and decline use form POST requests exactly once; callers
must resolve uncertain outcomes before retrying a mutation.

Authentication follows [Valve's Web API documentation](https://partner.steamgames.com/doc/webapi_overview).
Creating and accepting Steam Community trade offers use the separate
`tradeoffer` package and its authenticated Community session.

## Port provenance

This package incorporates the useful endpoint families from
[Philipp15b/go-steamapi](https://github.com/Philipp15b/go-steamapi), retaining its
[MIT license](./LICENSE). It replaces global settings with an instance client,
adds contexts and transport limits, and uses `protobuf-go-lite` records and
JSON codecs. Steam ID utilities come from go-steam's existing `steamid` package.
Game-owned numeric values remain open rather than copying stale Dota enums.

The API is intentionally updated for its new package path: create a client,
pass contexts, use explicit pagination, and use generated record field names.
Tests exercise the endpoint families with local HTTP servers; they do not need
or access saved credentials.
