// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package argtranslate

import (
	"fmt"
	"strings"
)

// occurrence is one flag as it appeared on the command line, after short flag
// clusters have been expanded.
type occurrence struct {
	// entry is the matrix row for the flag. It is the zero Entry for a flag the
	// matrix does not know, which is always unsupported.
	entry Entry
	// known reports whether the matrix defined the flag.
	known bool
	// value is the flag value for value kinds.
	value string
	// boolValue is the literal written after '=' for a bool flag, if any.
	boolValue string
	// display is the flag as the user spelled it, used in messages.
	display string
}

// parseResult is the intermediate form between tokenising and rendering.
type parseResult struct {
	occurrences []occurrence
	payload     []string
	// positionalAt is the index in payload of the first positional token, or -1.
	positionalAt int
	// flagsDone is set by a standalone "--": later tokens are never parsed as
	// flags, so words that look like docker flags stay part of the command.
	flagsDone bool
	// passthrough records a "--" seen before the image. It is re-emitted so the
	// runtime also treats the following token as the image, not as a flag.
	passthrough bool
	// usageErrors are malformed command lines. They are all reported together.
	usageErrors []error
}

// parser holds the state of one Translate call.
type parser struct {
	subcommand string
	matrix     Matrix
	// splitAt is the number of leading positionals after which every remaining
	// token is payload. Zero means flags are parsed anywhere, which is what
	// `build` does because its context can be followed by more options.
	splitAt int
}

func (p *parser) parse(args []string) *parseResult {
	res := &parseResult{positionalAt: -1}
	for i := 0; i < len(args); i++ {
		tok := args[i]

		if res.flagsDone {
			res.payload = append(res.payload, tok)
			continue
		}
		if tok == "--" {
			res.flagsDone = true
			res.passthrough = true
			continue
		}
		if isLongFlag(tok) {
			occs, consumed, err := p.parseLong(args, i)
			if err != nil {
				res.usageErrors = append(res.usageErrors, err)
			}
			res.occurrences = append(res.occurrences, occs...)
			i += consumed
			continue
		}
		if isShortFlag(tok) {
			occs, consumed, errs := p.parseShort(args, i)
			res.occurrences = append(res.occurrences, occs...)
			res.usageErrors = append(res.usageErrors, errs...)
			i += consumed
			continue
		}

		// A positional token. For run, create, and exec the image or container is
		// followed by the container command, whose own arguments must not be
		// translated even when they look like docker flags.
		if res.positionalAt < 0 {
			res.positionalAt = len(res.payload)
		}
		res.payload = append(res.payload, tok)
		if p.splitAt > 0 && len(res.payload) >= p.splitAt {
			res.flagsDone = true
		}
	}
	return res
}

func isLongFlag(tok string) bool {
	return len(tok) > 2 && strings.HasPrefix(tok, "--")
}

func isShortFlag(tok string) bool {
	return len(tok) > 1 && tok[0] == '-' && tok[1] != '-'
}

// parseLong parses one --flag token plus, for value flags written in the
// separated form, the following token. It returns the number of extra tokens
// consumed.
func (p *parser) parseLong(args []string, i int) ([]occurrence, int, error) {
	tok := args[i]
	name, value, hasEq := strings.Cut(strings.TrimPrefix(tok, "--"), "=")

	entry, known := p.matrix.Long(name)
	if !known {
		// An unknown flag is rejected. When it is written in the separated form
		// we consume the following word as if it were a value, so the scan
		// keeps finding the remaining unsupported flags and reports them all in
		// one error instead of stopping at the first one. Only non-dash tokens
		// are consumed, so a run of unknown flags is still reported in full.
		consumed := 0
		if !hasEq && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			consumed = 1
		}
		return []occurrence{{display: "--" + name, known: false}}, consumed, nil
	}

	if entry.Category == CatUnsupported {
		// Still consume the value so the remaining flags are reported too.
		if entry.Kind != KindBool && !hasEq {
			consumed := 0
			if i+1 < len(args) {
				consumed = 1
			}
			return []occurrence{{entry: entry, known: true, display: entry.Display()}}, consumed, nil
		}
		return []occurrence{{entry: entry, known: true, value: value, display: entry.Display()}}, 0, nil
	}

	if entry.Kind == KindBool {
		if hasEq {
			if !isBoolLiteral(value) {
				return nil, 0, &UsageError{
					Subcommand: p.subcommand,
					Flag:       entry.Display(),
					Reason:     fmt.Sprintf("%q is not a boolean value", value),
				}
			}
			return []occurrence{{entry: entry, known: true, boolValue: value, display: entry.Display()}}, 0, nil
		}
		return []occurrence{{entry: entry, known: true, display: entry.Display()}}, 0, nil
	}

	if !hasEq {
		if i+1 >= len(args) {
			return nil, 0, &UsageError{
				Subcommand: p.subcommand,
				Flag:       entry.Display(),
				Reason:     "the flag expects a value but none was given",
			}
		}
		// The value may start with '-': a negative number, or a value that
		// merely looks like a flag. pflag consumes it for value flags, and so
		// do we.
		value = args[i+1]
		return []occurrence{{entry: entry, known: true, value: value, display: entry.Display()}}, 1, nil
	}
	return []occurrence{{entry: entry, known: true, value: value, display: entry.Display()}}, 0, nil
}

// parseShort parses one -x token. A token may cluster several boolean flags,
// and the last flag in the cluster may carry the value, either attached
// (-p8080:80) or separated (-p 8080:80). It returns the number of extra tokens
// consumed.
func (p *parser) parseShort(args []string, i int) ([]occurrence, int, []error) {
	body := strings.TrimPrefix(args[i], "-")

	var (
		occs []occurrence
		errs []error
	)
	for pos := 0; pos < len(body); pos++ {
		c := body[pos : pos+1]
		entry, ok := p.matrix.Short(c)
		if !ok {
			occs = append(occs, occurrence{display: "-" + c, known: false})
			return occs, 0, errs
		}
		if entry.Category == CatUnsupported {
			occs = append(occs, occurrence{entry: entry, known: true, display: "-" + c})
			if entry.Kind != KindBool {
				rest := strings.TrimPrefix(body[pos+1:], "=")
				if rest != "" {
					return occs, 0, errs
				}
				return occs, 1, errs
			}
			return occs, 0, errs
		}
		if entry.Kind == KindBool {
			occs = append(occs, occurrence{entry: entry, known: true, display: "-" + c})
			continue
		}
		// A value flag ends the cluster. The rest of the token is its value,
		// with pflag's optional '=' stripped.
		rest := strings.TrimPrefix(body[pos+1:], "=")
		if rest != "" {
			occs = append(occs, occurrence{entry: entry, known: true, value: rest, display: "-" + c})
			return occs, 0, errs
		}
		if i+1 >= len(args) {
			errs = append(errs, &UsageError{
				Subcommand: p.subcommand,
				Flag:       "-" + c,
				Reason:     "the flag expects a value but none was given",
			})
			return occs, 0, errs
		}
		occs = append(occs, occurrence{entry: entry, known: true, value: args[i+1], display: "-" + c})
		return occs, 1, errs
	}
	return occs, 0, errs
}

func isBoolLiteral(v string) bool {
	switch v {
	case "true", "false", "1", "0", "t", "f", "T", "F",
		"TRUE", "FALSE", "True", "False":
		return true
	}
	return false
}
