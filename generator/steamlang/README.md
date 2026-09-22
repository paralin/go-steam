# SteamLanguage definitions

These definitions originate from SteamRE/SteamKit revision
[`33997711c1996cf4e7655bc99fe0e1d4eae7205c`](https://github.com/SteamRE/SteamKit/tree/33997711c1996cf4e7655bc99fe0e1d4eae7205c/Resources/SteamLanguage),
the revision previously pinned by go-steam's generator submodule.

The enum definitions have been reconciled with go-steam's published constants
at `c82323035020a77ce9fc687b205b96fb710fa682`. That generator's inputs had drifted
from its checked-in outputs. Retaining the published constants avoids silently
changing existing wire identifiers when moving to the Go generator. Source
comments, historical aliases, and the upstream license notice are retained;
line endings are normalized to LF.

The Go parser and emitter live in the parent directory. Regeneration reads these
files directly and does not fetch SteamKit. Future protocol updates should edit
these inputs deliberately and review the generated API and wire changes.

The imported definitions retain the [SteamRE license notice](./LICENSE).
