package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const protoModule = "github.com/paralin/go-steam"

type protoSource struct {
	sourcePath string
	outputPath string
	protoPkg   string
}

var protoSources = []protoSource{
	{"steammessages_base.proto", "protocol/protobuf/base.proto", "protobuf"},
	{"encrypted_app_ticket.proto", "protocol/protobuf/app_ticket.proto", "protobuf"},
	{"steammessages_clientserver.proto", "protocol/protobuf/client_server.proto", "protobuf"},
	{"steammessages_clientserver_2.proto", "protocol/protobuf/client_server_2.proto", "protobuf"},
	{"steammessages_clientserver_friends.proto", "protocol/protobuf/client_server_friends.proto", "protobuf"},
	{"steammessages_clientserver_login.proto", "protocol/protobuf/client_server_login.proto", "protobuf"},
	{"steammessages_sitelicenseclient.proto", "protocol/protobuf/client_site_license.proto", "protobuf"},
	{"content_manifest.proto", "protocol/protobuf/content_manifest.proto", "protobuf"},
	{"generator/extra/cmlist.proto", "protocol/protobuf/cmlist.proto", "protobuf"},
	{"steammessages_unified_base.steamclient.proto", "protocol/protobuf/unified/base.proto", "unified"},
	{"steammessages_cloud.steamclient.proto", "protocol/protobuf/unified/cloud.proto", "unified"},
	{"steammessages_credentials.steamclient.proto", "protocol/protobuf/unified/credentials.proto", "unified"},
	{"steammessages_gamenotifications.steamclient.proto", "protocol/protobuf/unified/gamenotifications.proto", "unified"},
	{"steammessages_offline.steamclient.proto", "protocol/protobuf/unified/offline.proto", "unified"},
	{"steammessages_parental.steamclient.proto", "protocol/protobuf/unified/parental.proto", "unified"},
	{"steammessages_partnerapps.steamclient.proto", "protocol/protobuf/unified/partnerapps.proto", "unified"},
	{"steammessages_player.steamclient.proto", "protocol/protobuf/unified/player.proto", "unified"},
	{"steammessages_publishedfile.steamclient.proto", "protocol/protobuf/unified/publishedfile.proto", "unified"},
	{"steammessages_auth.steamclient.proto", "protocol/protobuf/unified/auth.proto", "unified"},
	{"steammessages_client_objects.proto", "protocol/protobuf/unified/client_objects.proto", "unified"},
	{"enums.proto", "protocol/protobuf/unified/enums.proto", "unified"},
	{"enums_productinfo.proto", "protocol/protobuf/unified/enums_productinfo.proto", "unified"},
	{"offline_ticket.proto", "protocol/protobuf/unified/offline_ticket.proto", "unified"},
	{"steammessages_parental_objects.proto", "protocol/protobuf/unified/parental_objects.proto", "unified"},
}

var (
	blockHeadRe        = regexp.MustCompile(`^\s*(extend\s+\.?google\.protobuf|service\s+)`)
	fieldOptionRe      = regexp.MustCompile(`\s*\[([^\]]+)\]`)
	importRe           = regexp.MustCompile(`\s*import "([^"]+)";`)
	leadingDotTypeRe   = regexp.MustCompile(`(^|[^A-Za-z0-9_])\.([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)`)
	qualifiedFieldRe   = regexp.MustCompile(`\b(optional|repeated|required)\s+\.`)
	topLevelTypeNameRe = regexp.MustCompile(`(?m)^(?:message|enum)\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
)

func updateProtoSources(repoRoot string) error {
	sourceRoot := filepath.Join(repoRoot, "generator", "Protobufs", "steam")
	if info, err := os.Stat(sourceRoot); err != nil || !info.IsDir() {
		return fmt.Errorf("missing proto submodule at %s; run: git submodule update --init --recursive", sourceRoot)
	}

	for _, dir := range []string{
		filepath.Join(repoRoot, "protocol", "protobuf"),
		filepath.Join(repoRoot, "protocol", "protobuf", "unified"),
		filepath.Join(repoRoot, "generator", "extra"),
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	if err := removeProtoFiles(filepath.Join(repoRoot, "protocol", "protobuf")); err != nil {
		return err
	}
	if err := removeProtoFiles(filepath.Join(repoRoot, "protocol", "protobuf", "unified")); err != nil {
		return err
	}

	importPaths := make(map[string]string, len(protoSources))
	sourcePackages := make(map[string]string, len(protoSources))
	sourceTypes := make(map[string]map[string]struct{}, len(protoSources))
	for _, source := range protoSources {
		importPaths[source.sourcePath] = protoModule + "/" + source.outputPath
		sourcePackages[source.sourcePath] = source.protoPkg

		body, err := os.ReadFile(protoSourcePath(repoRoot, sourceRoot, source.sourcePath))
		if err != nil {
			return fmt.Errorf("read proto source %s: %w", source.sourcePath, err)
		}
		sourceTypes[source.sourcePath] = topLevelTypes(string(body))
	}

	for _, source := range protoSources {
		body, err := os.ReadFile(protoSourcePath(repoRoot, sourceRoot, source.sourcePath))
		if err != nil {
			return fmt.Errorf("read proto source %s: %w", source.sourcePath, err)
		}
		out, err := normalizeProtoSource(string(body), source.protoPkg, importPaths, sourcePackages, sourceTypes)
		if err != nil {
			return fmt.Errorf("normalize proto source %s: %w", source.sourcePath, err)
		}
		outPath := filepath.Join(repoRoot, source.outputPath)
		if err := os.WriteFile(outPath, []byte(out), 0644); err != nil {
			return err
		}
		fmt.Println("wrote " + source.outputPath)
	}
	return nil
}

func removeProtoFiles(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".proto" {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func protoSourcePath(repoRoot, sourceRoot, path string) string {
	if path == "generator/extra/cmlist.proto" {
		return filepath.Join(repoRoot, path)
	}
	return filepath.Join(sourceRoot, path)
}

func topLevelTypes(body string) map[string]struct{} {
	types := make(map[string]struct{})
	for _, match := range topLevelTypeNameRe.FindAllStringSubmatch(body, -1) {
		types[match[1]] = struct{}{}
	}
	return types
}

func normalizeProtoSource(body, protoPkg string, importPaths, sourcePackages map[string]string, sourceTypes map[string]map[string]struct{}) (string, error) {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	importedTypes, err := importedTypePackages(body, protoPkg, sourcePackages, sourceTypes)
	if err != nil {
		return "", err
	}

	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			out = append(out, line)
			i++
		case strings.HasPrefix(trimmed, "syntax =") || strings.HasPrefix(trimmed, "package "):
			i++
		case trimmed == `import "google/protobuf/descriptor.proto";`:
			i++
		case blockHeadRe.MatchString(line):
			i = skipProtoBlock(lines, i)
		case strings.HasPrefix(trimmed, "option "):
			i++
		case importRe.MatchString(line):
			match := importRe.FindStringSubmatch(line)
			mapped, ok := importPaths[match[1]]
			if !ok {
				return "", fmt.Errorf("unmapped import %q", match[1])
			}
			out = append(out, fmt.Sprintf("import %q;", mapped))
			i++
		default:
			line = stripProtoFieldOptions(line)
			line = rewriteImportedTypes(line, importedTypes)
			line = leadingDotTypeRe.ReplaceAllString(line, `${1}${2}`)
			line = qualifiedFieldRe.ReplaceAllString(line, `$1 `)
			out = append(out, line)
			i++
		}
	}

	normalized := strings.TrimSpace(strings.Join(out, "\n")) + "\n"
	return fmt.Sprintf("syntax = \"proto2\";\npackage %s;\n\n%s", protoPkg, normalized), nil
}

func importedTypePackages(body, currentPkg string, sourcePackages map[string]string, sourceTypes map[string]map[string]struct{}) (map[string]string, error) {
	packages := make(map[string]string)
	for _, match := range importRe.FindAllStringSubmatch(body, -1) {
		if match[1] == "google/protobuf/descriptor.proto" {
			continue
		}
		importPkg, ok := sourcePackages[match[1]]
		if !ok {
			return nil, fmt.Errorf("unmapped import %q", match[1])
		}
		if importPkg == currentPkg {
			continue
		}
		for typeName := range sourceTypes[match[1]] {
			packages[typeName] = importPkg
		}
	}
	return packages, nil
}

func skipProtoBlock(lines []string, index int) int {
	depth := strings.Count(lines[index], "{") - strings.Count(lines[index], "}")
	index++
	for index < len(lines) && depth > 0 {
		depth += strings.Count(lines[index], "{") - strings.Count(lines[index], "}")
		index++
	}
	return index
}

func stripProtoFieldOptions(line string) string {
	return fieldOptionRe.ReplaceAllStringFunc(line, func(match string) string {
		parts := fieldOptionRe.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}

		kept := make([]string, 0, 2)
		for part := range strings.SplitSeq(parts[1], ",") {
			item := strings.TrimSpace(part)
			if strings.HasPrefix(item, "default") || strings.HasPrefix(item, "deprecated") {
				kept = append(kept, item)
			}
		}
		if len(kept) == 0 {
			return ""
		}
		return " [" + strings.Join(kept, ", ") + "]"
	})
}

func rewriteImportedTypes(line string, importedTypes map[string]string) string {
	for typeName, typePkg := range importedTypes {
		line = regexp.MustCompile(`\.`+regexp.QuoteMeta(typeName)+`\b`).ReplaceAllString(line, typePkg+"."+typeName)
	}
	return line
}
