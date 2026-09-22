package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// languageDecl describes an enum or an ordered binary message definition.
type languageDecl struct {
	// kind is enum or class.
	kind string
	// name identifies the generated Go declaration.
	name string
	// option is the enum's storage type or the message's EMsg identifier.
	option string
	// flags distinguishes bit fields from ordinary enum values.
	flags bool
	// fields retains wire order, including constants used by field defaults.
	fields []languageField
}

// languageField describes one enum member, constant, or binary message field.
type languageField struct {
	// name identifies the source member.
	name string
	// typ is the scalar, enum, or protobuf type of a class field.
	typ string
	// flag selects constant, identifier, boolean, or protobuf wire handling.
	flag string
	// option is a fixed array size or a protobuf length field name.
	option string
	// values contains the operands of a bitwise-OR default expression.
	values []string
	// obsolete preserves a source deprecation explanation.
	obsolete string
}

// languageParser consumes one definition file while sharing import ownership.
type languageParser struct {
	// path identifies the source for diagnostics and relative imports.
	path string
	// tokens contains the unconsumed lexical units.
	tokens []string
	// imports records files already included in this parse.
	imports map[string]bool
}

// languageToken recognizes SteamLanguage identifiers, directives, and punctuation.
var languageToken = regexp.MustCompile(`^(?:\s+|//[^\n]*|"(?:\\.|[^"\\])*"|#import|-?[A-Za-z_0-9][A-Za-z_0-9:.]*|[{}<>=|;])`)

// parseLanguage reads a complete import tree in declaration order.
func parseLanguage(path string) ([]languageDecl, error) {
	return parseLanguageFile(path, make(map[string]bool))
}

// parseLanguageFile includes each source once and reports malformed syntax.
func parseLanguageFile(path string, imports map[string]bool) ([]languageDecl, error) {
	// Include each file once, including shared imports and import cycles.
	path = filepath.Clean(path)
	if imports[path] {
		return nil, nil
	}
	imports[path] = true

	// Tokenize the complete file before resolving its declarations.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	p := languageParser{path: path, imports: imports}
	for remaining, line := string(data), 1; remaining != ""; {
		item := languageToken.FindString(remaining)
		if item == "" {
			return nil, fmt.Errorf("%s:%d: unexpected character %q", path, line, remaining[0])
		}

		// Retain syntax tokens and advance source positions past comments and spaces.
		line += strings.Count(item, "\n")
		remaining = remaining[len(item):]
		if strings.TrimSpace(item) != "" && !strings.HasPrefix(item, "//") {
			p.tokens = append(p.tokens, item)
		}
	}

	// Resolve imports at their declaration position so field order remains stable.
	var declarations []languageDecl
	for len(p.tokens) > 0 {
		if p.take("#import") {
			name, err := strconv.Unquote(p.pop())
			if err != nil {
				return nil, fmt.Errorf("%s: invalid import: %w", path, err)
			}
			included, err := parseLanguageFile(filepath.Join(filepath.Dir(path), name), imports)
			if err != nil {
				return nil, err
			}
			declarations = append(declarations, included...)
			continue
		}

		// Append the next local declaration after any preceding imported definitions.
		decl, err := p.declaration()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		declarations = append(declarations, decl)
	}
	return declarations, nil
}

// declaration parses a top-level enum or class and its ordered members.
func (p *languageParser) declaration() (languageDecl, error) {
	// Read the declaration identity and its optional storage or message identifier.
	d := languageDecl{kind: p.pop(), name: p.pop()}
	if d.kind != "enum" && d.kind != "class" {
		return d, fmt.Errorf("expected enum or class, got %q", d.kind)
	}
	if p.take("<") {
		d.option = p.pop()
		if err := p.expect(">"); err != nil {
			return d, err
		}
	}

	// Consume metadata that affects enum formatting or external packet framing.
	for !p.take("{") {
		switch attribute := p.pop(); attribute {
		case "flags":
			d.flags = true
		case "expects":
			// Parent headers belong to packet framing, outside the message body.
			p.pop()
		case "removed":
			// Keep published Go types for historical messages.
		default:
			return d, fmt.Errorf("%s: unexpected declaration attribute %q", d.name, attribute)
		}
	}

	// Preserve the member order because class order defines the wire layout.
	for !p.take("}") {
		field, err := p.field(d.kind)
		if err != nil {
			return d, fmt.Errorf("%s: %w", d.name, err)
		}
		d.fields = append(d.fields, field)
	}
	return d, p.expect(";")
}

// field parses a member and its optional default and deprecation metadata.
func (p *languageParser) field(kind string) (languageField, error) {
	// Parse the first identifier and any fixed-size or protobuf-length option.
	var f languageField
	first := p.pop()
	if first == "" {
		return f, fmt.Errorf("unexpected end of definition")
	}
	if p.take("<") {
		f.option = p.pop()
		if err := p.expect(">"); err != nil {
			return f, err
		}
	}

	// Separate enum names from typed class fields and their wire modifiers.
	if kind == "enum" {
		f.name = first
	} else {
		switch first {
		case "const", "boolmarshal", "steamidmarshal", "gameidmarshal", "protomask", "protomaskgc", "proto":
			f.flag, f.typ = first, p.pop()
		default:
			f.typ = first
		}
		f.name = p.pop()
	}

	// Preserve default operands for alias resolution and constructor emission.
	if p.take("=") {
		for {
			f.values = append(f.values, p.pop())
			if !p.take("|") {
				break
			}
		}
	}
	if err := p.expect(";"); err != nil {
		return f, err
	}

	// Retain deprecation reasons while keeping historical public members available.
	if p.take("obsolete") || p.take("removed") {
		f.obsolete = "No longer used by Steam."
		if len(p.tokens) > 0 && strings.HasPrefix(p.tokens[0], `"`) {
			f.obsolete, _ = strconv.Unquote(p.pop())
		}
		p.take(";")
	}
	return f, nil
}

// pop consumes one token or returns an empty string at end of input.
func (p *languageParser) pop() string {
	// End-of-input is reported by the surrounding grammar production.
	if len(p.tokens) == 0 {
		return ""
	}

	// Consume exactly one token.
	item := p.tokens[0]
	p.tokens = p.tokens[1:]
	return item
}

// take consumes the next token only when it matches the requested literal.
func (p *languageParser) take(want string) bool {
	// Leave the token stream unchanged when the optional token is absent.
	if len(p.tokens) == 0 || p.tokens[0] != want {
		return false
	}

	// Consume the matched token.
	p.tokens = p.tokens[1:]
	return true
}

// expect consumes a required token or explains the syntax mismatch.
func (p *languageParser) expect(want string) error {
	if got := p.pop(); got != want {
		return fmt.Errorf("expected %q, got %q", want, got)
	}
	return nil
}
