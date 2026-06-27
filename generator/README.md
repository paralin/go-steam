# Protocol generation

Initialize the pinned SteamDatabase protobuf source once:

```sh
git submodule update --init --recursive generator/Protobufs
```

Regenerate the checked-in Steam client protobuf files:

```sh
go run generator.go proto
```

The `proto` target copies and normalizes the selected SteamDatabase inputs with
`generator/update_protos.bash`, then runs `aptre` / `protobuf-go-lite` over
`protocol/protobuf` and `protocol/protobuf/unified`.

SteamLanguage generation still uses the legacy SteamKit generator:

```sh
go run generator.go steamlang
```
