# Protocol generation

Initialize the pinned SteamDatabase protobuf source once:

```sh
git submodule update --init --recursive generator/Protobufs
```

Regenerate the checked-in Steam client protobuf files:

```sh
cd generator && go run . proto
```

The `proto` target copies and normalizes the selected SteamDatabase inputs in
Go, then runs `aptre` / `protobuf-go-lite` over `protocol/protobuf` and
`protocol/protobuf/unified`.

SteamLanguage generation still uses the legacy SteamKit generator:

```sh
cd generator && go run . steamlang
```
