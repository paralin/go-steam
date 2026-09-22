package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// main executes requested generation targets in order.
func main() {
	if len(os.Args) < 2 {
		fatal(errors.New("invalid target: available targets: clean, proto, steamlang"))
	}

	repoRoot, err := filepath.Abs("..")
	fatal(err)

	for _, target := range os.Args[1:] {
		switch target {
		case "clean":
			fatal(clean(repoRoot))
		case "proto":
			fatal(buildProto(context.Background(), repoRoot))
		case "steamlang":
			fatal(buildSteamLanguage(context.Background(), repoRoot))
		default:
			fatal(fmt.Errorf("invalid target %q: available targets: clean, proto, steamlang", target))
		}
	}
}

// clean removes generated outputs without changing retained schema inputs.
func clean(repoRoot string) error {
	for _, root := range []string{
		filepath.Join(repoRoot, "protocol", "protobuf"),
		filepath.Join(repoRoot, "tf2", "protocol", "protobuf"),
	} {
		if err := removeGeneratedGo(root); err != nil {
			return err
		}
	}

	for _, path := range []string{
		filepath.Join(repoRoot, "protocol", "steamlang", "enums.go"),
		filepath.Join(repoRoot, "protocol", "steamlang", "messages.go"),
	} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", path, err)
		}
	}
	return nil
}

// removeGeneratedGo removes generated Go files below one package root.
func removeGeneratedGo(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".pb.go") {
			return nil
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove %s: %w", path, err)
		}
		return nil
	})
}

// buildProto normalizes pinned Valve schemas and generates lite codecs.
func buildProto(ctx context.Context, repoRoot string) error {
	if err := updateProtoSources(repoRoot); err != nil {
		return err
	}
	return run(ctx, repoRoot,
		"go", "run", "github.com/aperturerobotics/common/cmd/aptre@v0.35.2",
		"generate", "--language", "go", "--rpc", "none",
		"--targets", "protocol/protobuf/*.proto",
		"--targets", "protocol/protobuf/unified/*.proto",
		"--targets", "tf2/protocol/protobuf/*.proto",
		"--force", "--verbose",
	)
}

// buildSteamLanguage runs the retained SteamKit binary-message generator.
func buildSteamLanguage(ctx context.Context, repoRoot string) error {
	exePath := filepath.Join("generator", "GoSteamLanguageGenerator", "bin", "Debug", "GoSteamLanguageGenerator.exe")
	return run(ctx, repoRoot, "mono", exePath, filepath.Join("generator", "SteamKit"), filepath.Join("protocol", "steamlang"))
}

// run joins one cancellable generation command with inherited diagnostics.
func run(ctx context.Context, dir, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

// fatal prints a failed generation operation and terminates the command.
func fatal(err error) {
	if err == nil {
		return
	}
	var message bytes.Buffer
	message.WriteString(err.Error())
	message.WriteByte('\n')
	_, _ = os.Stderr.Write(message.Bytes())
	os.Exit(1)
}
