// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package argtranslate

import (
	"fmt"
	"strings"
)

// DocURL is the page the error messages link to. Keeping it in one place makes
// it cheap to move the documentation later.
const DocURL = "https://github.com/runfinch/finch/blob/main/docs/arg-mapping.md"

// UnsupportedError reports every unsupported argument in one error, so users do
// not have to fix one flag, retry, and fix the next one.
type UnsupportedError struct {
	// Subcommand is the finch sub-command the arguments were meant for.
	Subcommand string
	// Flags are the offending flags in command line order, without duplicates.
	Flags []string
}

// Error implements the error interface.
func (e *UnsupportedError) Error() string {
	noun := "flag"
	if len(e.Flags) > 1 {
		noun = "flags"
	}
	return fmt.Sprintf(
		"%s %s not supported by finch %s: %s (see %s#finch-%s)",
		noun, plural(len(e.Flags)), e.Subcommand,
		strings.Join(e.Flags, ", "), DocURL, e.Subcommand,
	)
}

func plural(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}

// UsageError reports arguments that cannot be parsed at all, for example a flag
// whose value is missing. It is separate from UnsupportedError because the fix
// is different: the flag exists, the command line is malformed.
type UsageError struct {
	Subcommand string
	Flag       string
	Reason     string
}

// Error implements the error interface.
func (e *UsageError) Error() string {
	return fmt.Sprintf("invalid %s for finch %s: %s (see %s#finch-%s)", e.Flag, e.Subcommand, e.Reason, DocURL, e.Subcommand)
}
