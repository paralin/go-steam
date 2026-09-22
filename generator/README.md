# Protocol generation

All generated Go files and their schema inputs are retained in this repository.
Normal builds need only the Go module dependencies. The generators run in Go;
there is no SteamKit checkout, C# compiler, Mono, .NET, or Docker requirement.

## Regenerate retained definitions

Run from the repository root:

```sh
go mod vendor
go run ./generator steamlang proto
```

`steamlang` reads the import tree rooted at
[`steamlang/steammsg.steamd`](./steamlang/steammsg.steamd) and emits
`protocol/steamlang/enums.go` and `messages.go`. It retains public enum values,
message field order, fixed-width layouts, and constructor defaults. Protobuf
headers use the same bounded decoder as the network client. Only flag enums
expand bit combinations when formatted; unknown ordinary enum values print
as numbers.

`proto` invokes the pinned Go `aptre` tool to generate `protobuf-go-lite` codecs
for the Steam, unified-message, TF2, and Web API packages. The tool obtains its
compiler dependencies through its normal cache. It reads checked-in schemas
without refreshing them from a separate source checkout. Stage newly added or
changed `.proto` files before generation because `aptre` discovers tracked
inputs through Git.

The same commands also work from this directory as `go run . <target>`.
`clean` removes generated Go outputs and preserves handwritten files and inputs.

## Refresh protobuf inputs

The optional SteamDatabase submodule is needed only when refreshing upstream
protobuf definitions:

```sh
git submodule update --init generator/Protobufs
go run ./generator refresh-proto
```

Review the normalized `.proto` changes, stage the intended schemas, then run
`go run ./generator proto`. Updating the submodule revision is an explicit
protocol update and should include the corresponding compatibility review.

The normalizer retains enum aliases and isolates TF2's protobuf namespace
while preserving its public Go import path. `extra/deviceauth.proto` retains
SteamDatabase's device-auth messages from revision `fd37505^`, before that
source was removed upstream.

## Update SteamLanguage definitions

Edit the retained `.steamd` files and run `go run ./generator steamlang`.
Imports are relative to the importing file. The parser supports enums, flag
combinations, aliases, class constants, fixed arrays, Steam IDs, boolean fields,
protobuf headers, and deprecation metadata. Historical message types remain
available even when the source marks them removed.

See [definition provenance](./steamlang/README.md) before importing a newer
upstream snapshot. Run the focused checks after changing either generator:

```sh
go test -timeout=30s ./generator ./protocol/steamlang ./protocol/gamecoordinator ./web/...
```
