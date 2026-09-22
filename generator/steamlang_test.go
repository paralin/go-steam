package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestSteamLanguageReproducible checks both outputs without any external checkout.
func TestSteamLanguageReproducible(t *testing.T) {
	decls, err := parseLanguage(filepath.Join("steamlang", "steammsg.steamd"))
	if err != nil {
		t.Fatal(err)
	}

	for _, output := range []struct {
		name string
		emit func([]languageDecl) ([]byte, error)
	}{{"enums.go", emitLanguageEnums}, {"messages.go", emitLanguageMessages}} {
		generated, err := output.emit(decls)
		if err != nil {
			t.Fatal(err)
		}
		retained, err := os.ReadFile(filepath.Join("..", "protocol", "steamlang", output.name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(generated, retained) {
			t.Errorf("%s is stale: run go run ./generator steamlang", output.name)
		}
	}
}

// TestSteamLanguageInvalidDefinition fails promptly on malformed or cyclic inputs.
func TestSteamLanguageInvalidDefinition(t *testing.T) {
	for _, source := range []string{
		`enum Broken { Value = 1;`,
		`enum Broken { Value = Other; Other = Value; };`,
		`enum Broken { Value = Missing; };`,
		`class Broken unknown { uint value; };`,
	} {
		directory := t.TempDir()
		path := filepath.Join(directory, "test.steamd")
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}

		decls, err := parseLanguage(path)
		if err == nil {
			_, err = emitLanguageEnums(decls)
		}
		if err == nil {
			t.Errorf("accepted malformed source: %s", source)
		}
	}
}
