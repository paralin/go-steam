package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// protoModule supplies generated import paths for normalized Valve schemas.
const protoModule = "github.com/paralin/go-steam"

// protoSource binds an upstream schema to its retained Go package.
type protoSource struct {
	// sourcePath is relative to the Steam source directory or repository root.
	sourcePath string
	// outputPath retains the public package's checked-in schema.
	outputPath string
	// protoPkg isolates messages that belong to different wire domains.
	protoPkg string
}

// protoSources preserves the Steam and TF2 packages through the same generator.
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
	{"generator/extra/deviceauth.proto", "protocol/protobuf/unified/deviceauth.proto", "unified"},
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
	{"../tf2/steammessages.proto", "tf2/protocol/protobuf/steam.proto", "tf2"},
	{"../tf2/base_gcmessages.proto", "tf2/protocol/protobuf/base.proto", "tf2"},
	{"../tf2/gcsdk_gcmessages.proto", "tf2/protocol/protobuf/gcsdk.proto", "tf2"},
	{"../tf2/gcsystemmsgs.proto", "tf2/protocol/protobuf/system.proto", "tf2"},
	{"../tf2/econ_gcmessages.proto", "tf2/protocol/protobuf/econ.proto", "tf2"},
	{"../tf2/tf_gcmessages.proto", "tf2/protocol/protobuf/tf.proto", "tf2"},
}

var (
	blockHeadRe        = regexp.MustCompile(`^\s*(extend\s+\.?google\.protobuf|service\s+)`)
	fieldOptionRe      = regexp.MustCompile(`\s*\[([^\]]+)\]`)
	importRe           = regexp.MustCompile(`\s*import "([^"]+)";`)
	leadingDotTypeRe   = regexp.MustCompile(`(^|[^A-Za-z0-9_])\.([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)`)
	qualifiedFieldRe   = regexp.MustCompile(`\b(optional|repeated|required)\s+\.`)
	topLevelTypeNameRe = regexp.MustCompile(`(?m)^(?:message|enum)\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
)

// updateProtoSources normalizes pinned Valve inputs into stable public packages.
func updateProtoSources(repoRoot string) error {
	// Require the pinned source checkout before replacing any retained schemas.
	sourceRoot := filepath.Join(repoRoot, "generator", "Protobufs", "steam")
	if info, err := os.Stat(sourceRoot); err != nil || !info.IsDir() {
		return fmt.Errorf("missing proto submodule at %s; run: git submodule update --init --recursive", sourceRoot)
	}

	for _, dir := range []string{
		filepath.Join(repoRoot, "protocol", "protobuf"),
		filepath.Join(repoRoot, "protocol", "protobuf", "unified"),
		filepath.Join(repoRoot, "generator", "extra"),
		filepath.Join(repoRoot, "tf2", "protocol", "protobuf"),
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

	// Resolve upstream import names once across the selected schema set.
	importPaths := make(map[string]string, len(protoSources))
	sourcePackages := make(map[string]string, len(protoSources))
	sourceTypes := make(map[string]map[string]struct{}, len(protoSources))
	for _, source := range protoSources {
		name := filepath.Base(source.sourcePath)
		importPaths[name] = protoModule + "/" + source.outputPath
		sourcePackages[name] = source.protoPkg

		body, err := os.ReadFile(protoSourcePath(repoRoot, sourceRoot, source.sourcePath))
		if err != nil {
			return fmt.Errorf("read proto source %s: %w", source.sourcePath, err)
		}
		sourceTypes[name] = topLevelTypes(string(body))
	}

	// Normalize external extensions and imports without changing message fields.
	for _, source := range protoSources {
		body, err := os.ReadFile(protoSourcePath(repoRoot, sourceRoot, source.sourcePath))
		if err != nil {
			return fmt.Errorf("read proto source %s: %w", source.sourcePath, err)
		}
		out, err := normalizeProtoSource(string(body), source.protoPkg, importPaths, sourcePackages, sourceTypes)
		if err != nil {
			return fmt.Errorf("normalize proto source %s: %w", source.sourcePath, err)
		}
		if source.protoPkg == "tf2" {
			out = strings.Replace(out, "package tf2;", "package tf2;\noption go_package = \""+protoModule+"/tf2/protocol/protobuf;protobuf\";", 1)
		}
		outPath := filepath.Join(repoRoot, source.outputPath)
		if err := os.WriteFile(outPath, []byte(out), 0644); err != nil {
			return err
		}
		fmt.Println("wrote " + source.outputPath)
	}
	return nil
}

// removeProtoFiles removes schemas directly inside a generated package.
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

// protoSourcePath resolves pinned upstream inputs and retained local schemas.
func protoSourcePath(repoRoot, sourceRoot, path string) string {
	if strings.HasPrefix(path, "generator/extra/") {
		return filepath.Join(repoRoot, path)
	}
	return filepath.Join(sourceRoot, path)
}

// topLevelTypes indexes exported schema names for cross-package imports.
func topLevelTypes(body string) map[string]struct{} {
	types := make(map[string]struct{})
	for _, match := range topLevelTypeNameRe.FindAllStringSubmatch(body, -1) {
		types[match[1]] = struct{}{}
	}
	return types
}

// normalizeProtoSource retains wire fields while removing compiler-only extensions.
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
		case strings.HasPrefix(trimmed, "option allow_alias"):
			out = append(out, line)
			i++
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

// importedTypePackages resolves foreign message names to their normalized packages.
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

// skipProtoBlock advances past a balanced service or extension declaration.
func skipProtoBlock(lines []string, index int) int {
	depth := strings.Count(lines[index], "{") - strings.Count(lines[index], "}")
	index++
	for index < len(lines) && depth > 0 {
		depth += strings.Count(lines[index], "{") - strings.Count(lines[index], "}")
		index++
	}
	return index
}

// stripProtoFieldOptions retains supported field semantics and removes custom options.
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

// rewriteImportedTypes qualifies imported message names for normalized namespaces.
func rewriteImportedTypes(line string, importedTypes map[string]string) string {
	for typeName, typePkg := range importedTypes {
		line = regexp.MustCompile(`\.`+regexp.QuoteMeta(typeName)+`\b`).ReplaceAllString(line, typePkg+"."+typeName)
	}
	return line
}
