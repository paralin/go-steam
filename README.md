# Steam for Go

[![Go Reference](https://pkg.go.dev/badge/github.com/paralin/go-steam.svg)](https://pkg.go.dev/github.com/paralin/go-steam)

**go-steam** connects Go programs to the Steam network, game coordinators,
Steam Community, and the Steam Web API. It runs without the Steam desktop
client. Builds and protocol generation use Go; SteamKit, Mono, and .NET are
not required.

## Installation

```sh
go get github.com/paralin/go-steam
```

Use the Go version declared in [go.mod](./go.mod).

## Packages

| Package | Purpose |
| --- | --- |
| [`steam`](https://pkg.go.dev/github.com/paralin/go-steam) | Steam connections, authentication, friends, chat, presence, and GC transport |
| [`gsbot`](./gsbot) | Bot lifecycle helpers and an [example bot](./gsbot/gsbot) |
| [`web`](./web) | Context-aware Steam Web API client, profiles, applications, inventories, schemas, and trade offers |
| [`web/dota`](./web/dota) | Dota match history and match details over HTTP |
| [`steamid`](./steamid) | Steam account and group identifiers |
| [`tradeoffer`](./tradeoffer), [`trade`](./trade) | Steam Community trading and authenticated trade sessions |
| [`economy/inventory`](./economy/inventory) | Steam Community inventories |
| [`tf2`](./tf2) | Team Fortress 2 game-coordinator operations |

Game-coordinator clients for [Deadlock](https://github.com/paralin/go-deadlock)
and [Dota 2](https://github.com/paralin/go-dota2) build on the Steam connection.
The `web` packages use HTTP and an API key independently of that connection.

## Steam Web API

```go
client, err := web.NewClient(web.Config{APIKey: apiKey})
if err != nil {
    return err
}

profiles, err := client.GetPlayerSummaries(ctx, []steamid.SteamId{accountID})
if err != nil {
    return err
}
```

Pass a context to every request. The default client uses a 30-second HTTP
timeout and a 64 MiB response limit. Callers control caching and retries;
mutations are never retried automatically. See [the Web API guide](./web/README.md)
for pagination, error handling, and the port's API changes.

## Protocol generation

Generated sources and their definitions are checked in. Normal builds do not
run generators. To regenerate from the retained definitions, run from the
repository root:

```sh
go mod vendor
go run ./generator steamlang proto
```

The Go SteamLanguage parser emits fixed-width binary messages and enums.
Protobuf messages use
[`protobuf-go-lite`](https://github.com/aperturerobotics/protobuf-go-lite)
for binary and JSON codecs, including the GC interfaces and Web API records.

Refreshing upstream definitions is a separate operation. See
[the generator guide](./generator/README.md) for source provenance and update
commands.

## Testing

```sh
go test -race -timeout=60s ./...
```

Web API tests use local HTTP servers and do not require credentials. Protocol
tests cover binary layouts, protobuf framing, malformed lengths, buffer reuse,
and reproducible generation.

## License and attribution

The Go library is distributed under the [New BSD License](./LICENSE.txt) and
was originally authored by [Philipp Schröer](https://github.com/Philipp15b).
The Web API port retains its [MIT notice](./web/LICENSE). Imported SteamLanguage
definitions retain their [SteamRE LGPL notice](./generator/steamlang/LICENSE)
and [provenance](./generator/steamlang/README.md).
