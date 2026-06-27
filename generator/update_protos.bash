#!/bin/bash
# Selectively copy and normalize Steam client proto sources from the pinned
# SteamDatabase/Protobufs submodule into protocol/protobuf for aptre's
# protobuf-go-lite generator.
set -eo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
SRC_DIR="${SCRIPT_DIR}/Protobufs/steam"
ROOT_DIR="${REPO_ROOT}/protocol/protobuf"
UNIFIED_DIR="${ROOT_DIR}/unified"

if [ ! -d "${SRC_DIR}" ]; then
	echo "missing proto submodule at ${SRC_DIR}" >&2
	echo "run: git submodule update --init --recursive" >&2
	exit 1
fi

mkdir -p "${ROOT_DIR}" "${UNIFIED_DIR}" "${SCRIPT_DIR}/extra"

python3 - "$SRC_DIR" "$REPO_ROOT" <<'PY'
from pathlib import Path
import re
import sys

src_dir = Path(sys.argv[1])
repo_root = Path(sys.argv[2])
module = "github.com/paralin/go-steam"

files = [
    ("steammessages_base.proto", "protocol/protobuf/base.proto", "protobuf"),
    ("encrypted_app_ticket.proto", "protocol/protobuf/app_ticket.proto", "protobuf"),
    ("steammessages_clientserver.proto", "protocol/protobuf/client_server.proto", "protobuf"),
    ("steammessages_clientserver_2.proto", "protocol/protobuf/client_server_2.proto", "protobuf"),
    ("steammessages_clientserver_friends.proto", "protocol/protobuf/client_server_friends.proto", "protobuf"),
    ("steammessages_clientserver_login.proto", "protocol/protobuf/client_server_login.proto", "protobuf"),
    ("steammessages_sitelicenseclient.proto", "protocol/protobuf/client_site_license.proto", "protobuf"),
    ("content_manifest.proto", "protocol/protobuf/content_manifest.proto", "protobuf"),
    ("generator/extra/cmlist.proto", "protocol/protobuf/cmlist.proto", "protobuf"),
    ("steammessages_unified_base.steamclient.proto", "protocol/protobuf/unified/base.proto", "unified"),
    ("steammessages_cloud.steamclient.proto", "protocol/protobuf/unified/cloud.proto", "unified"),
    ("steammessages_credentials.steamclient.proto", "protocol/protobuf/unified/credentials.proto", "unified"),
    ("steammessages_gamenotifications.steamclient.proto", "protocol/protobuf/unified/gamenotifications.proto", "unified"),
    ("steammessages_offline.steamclient.proto", "protocol/protobuf/unified/offline.proto", "unified"),
    ("steammessages_parental.steamclient.proto", "protocol/protobuf/unified/parental.proto", "unified"),
    ("steammessages_partnerapps.steamclient.proto", "protocol/protobuf/unified/partnerapps.proto", "unified"),
    ("steammessages_player.steamclient.proto", "protocol/protobuf/unified/player.proto", "unified"),
    ("steammessages_publishedfile.steamclient.proto", "protocol/protobuf/unified/publishedfile.proto", "unified"),
    ("steammessages_auth.steamclient.proto", "protocol/protobuf/unified/auth.proto", "unified"),
    ("steammessages_client_objects.proto", "protocol/protobuf/unified/client_objects.proto", "unified"),
    ("enums.proto", "protocol/protobuf/unified/enums.proto", "unified"),
    ("enums_productinfo.proto", "protocol/protobuf/unified/enums_productinfo.proto", "unified"),
    ("offline_ticket.proto", "protocol/protobuf/unified/offline_ticket.proto", "unified"),
    ("steammessages_parental_objects.proto", "protocol/protobuf/unified/parental_objects.proto", "unified"),
]

import_map = {src: f"{module}/{dst}" for src, dst, _ in files}
local_sources = {"generator/extra/cmlist.proto"}
source_packages = {src: package for src, _, package in files}

def top_level_types(text: str) -> set[str]:
    return {
        match.group(1)
        for match in re.finditer(r"^(?:message|enum)\s+([A-Za-z_][A-Za-z0-9_]*)\b", text, re.MULTILINE)
    }


for _, dst, _ in files:
    out = repo_root / dst
    out.parent.mkdir(parents=True, exist_ok=True)

for directory in [repo_root / "protocol/protobuf", repo_root / "protocol/protobuf/unified"]:
    for path in directory.glob("*.proto"):
        path.unlink()

def source_path(name: str) -> Path:
    if name in local_sources:
        return repo_root / name
    return src_dir / name

source_types = {
    src: top_level_types(source_path(src).read_text())
    for src, _, _ in files
}

def strip_block(lines, index):
    depth = lines[index].count("{") - lines[index].count("}")
    index += 1
    while index < len(lines) and depth > 0:
        depth += lines[index].count("{") - lines[index].count("}")
        index += 1
    return index

def strip_field_options(line: str) -> str:
    def repl(match):
        kept = []
        for part in match.group(1).split(","):
            item = part.strip()
            if item.startswith("default") or item.startswith("deprecated"):
                kept.append(item)
        if not kept:
            return ""
        return " [" + ", ".join(kept) + "]"
    return re.sub(r"\s*\[([^\]]+)\]", repl, line)

def imported_type_packages(text: str, package: str) -> dict[str, str]:
    type_packages = {}
    for imp in re.findall(r'import "([^"]+)";', text):
        imp_package = source_packages.get(imp)
        if imp_package is None or imp_package == package:
            continue
        for type_name in source_types[imp]:
            type_packages[type_name] = imp_package
    return type_packages

def rewrite_imported_types(line: str, type_packages: dict[str, str]) -> str:
    for type_name, type_package in type_packages.items():
        line = re.sub(rf"\.{type_name}\b", f"{type_package}.{type_name}", line)
    return line

def normalize(text: str, package: str) -> str:
    text = text.replace("\r\n", "\n")
    type_packages = imported_type_packages(text, package)
    src_lines = text.splitlines()
    out = []
    i = 0
    while i < len(src_lines):
        line = src_lines[i]
        stripped = line.strip()
        if not stripped:
            out.append(line)
            i += 1
            continue
        if stripped.startswith("syntax =") or stripped.startswith("package "):
            i += 1
            continue
        if stripped == 'import "google/protobuf/descriptor.proto";':
            i += 1
            continue
        if stripped.startswith("extend .google.protobuf") or stripped.startswith("extend google.protobuf"):
            i = strip_block(src_lines, i)
            continue
        if stripped.startswith("service "):
            i = strip_block(src_lines, i)
            continue
        if stripped.startswith("option "):
            i += 1
            continue
        m = re.match(r'\s*import "([^"]+)";', line)
        if m:
            imp = m.group(1)
            mapped = import_map.get(imp)
            if mapped is None:
                raise SystemExit(f"unmapped import {imp!r}")
            out.append(f'import "{mapped}";')
            i += 1
            continue
        line = strip_field_options(line)
        line = rewrite_imported_types(line, type_packages)
        line = re.sub(r"(?<![\w])\.([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)", r"\1", line)
        line = re.sub(r"\b(optional|repeated|required)\s+\.", r"\1 ", line)
        out.append(line)
        i += 1
    body = "\n".join(out).strip() + "\n"
    return f'syntax = "proto2";\npackage {package};\n\n{body}'

for src, dst, package in files:
    in_path = source_path(src)
    if not in_path.exists():
        raise SystemExit(f"missing source proto: {in_path}")
    out_path = repo_root / dst
    out_path.write_text(normalize(in_path.read_text(), package))
    print(f"wrote {dst}")
PY
