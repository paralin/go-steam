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
	// Require an explicit target before selecting repository inputs.
	if len(os.Args) < 2 {
		fatal(errors.New("usage: go run ./generator <proto|steamlang|refresh-proto|clean>..."))
	}

	// Accept invocation from either the repository root or generator directory.
	repoRoot, err := os.Getwd()
	fatal(err)
	if filepath.Base(repoRoot) == "generator" {
		repoRoot = filepath.Dir(repoRoot)
	}

	// Complete targets sequentially so refreshes precede consumers of their output.
	for _, target := range os.Args[1:] {
		switch target {
		case "clean":
			fatal(clean(repoRoot))
		case "proto":
			fatal(buildProto(context.Background(), repoRoot))
		case "steamlang":
			fatal(generateSteamLanguage(repoRoot))
		case "refresh-proto":
			fatal(updateProtoSources(repoRoot))
		default:
			fatal(fmt.Errorf("invalid target %q: available targets: clean, proto, steamlang, refresh-proto", target))
		}
	}
}

// clean removes generated outputs without changing retained schema inputs.
func clean(repoRoot string) error {
	// Delete only generated protobuf files from the supported output packages.
	for _, root := range []string{
		filepath.Join(repoRoot, "protocol", "protobuf"),
		filepath.Join(repoRoot, "tf2", "protocol", "protobuf"),
		filepath.Join(repoRoot, "web"),
	} {
		if err := removeGeneratedGo(root); err != nil {
			return err
		}
	}

	// Remove the two generated SteamLanguage files while retaining framing helpers.
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
		// Stop on traversal failures and leave all non-generated paths intact.
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".pb.go") {
			return nil
		}

		// Delete the selected generated file.
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove %s: %w", path, err)
		}
		return nil
	})
}

// buildProto generates lite codecs from checked-in schemas without source refresh.
func buildProto(ctx context.Context, repoRoot string) error {
	return run(ctx, repoRoot,
		"go", "run", "github.com/aperturerobotics/common/cmd/aptre@v0.35.2",
		"generate", "--language", "go", "--rpc", "none",
		"--targets", "protocol/protobuf/*.proto",
		"--targets", "protocol/protobuf/unified/*.proto",
		"--targets", "tf2/protocol/protobuf/*.proto",
		"--targets", "web/*.proto",
		"--targets", "web/dota/*.proto",
	)
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
	// Successful targets do not produce CLI diagnostics.
	if err == nil {
		return
	}

	// Print one failure and stop before running dependent targets.
	var message bytes.Buffer
	message.WriteString(err.Error())
	message.WriteByte('\n')
	_, _ = os.Stderr.Write(message.Bytes())
	os.Exit(1)
}
