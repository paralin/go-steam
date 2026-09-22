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
`protocol/protobuf/unified` and `tf2/protocol/protobuf`. All message packages and
GC interfaces use `protobuf-go-lite`; no reflection-based protobuf runtime is
required. Newly introduced schemas must be added to Git before generation.

The retained `extra/deviceauth.proto` comes from SteamDatabase revision
`fd37505^`, before that source was removed. It preserves the existing public
device-auth messages. TF2 uses a separate protobuf namespace while retaining its
public Go package path. The source normalizer preserves enum aliases.

SteamLanguage generation still uses the legacy SteamKit generator:

```sh
cd generator && go run . steamlang
```
