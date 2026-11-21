// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

// Command gen regenerates docs/arg-mapping.md from the translation matrix.
//
// It is run by `go generate ./pkg/argtranslate` and by `make gen-code`, so CI
// fails when the documentation and the matrix disagree.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/runfinch/finch/pkg/argtranslate"
)

func main() {
	root, err := moduleRoot()
	if err != nil {
		fail(err)
	}
	out := filepath.Join(root, "docs", "arg-mapping.md")
	if err := os.WriteFile(out, []byte(argtranslate.RenderMarkdown()), 0o644); err != nil {
		fail(fmt.Errorf("failed to write %s: %w", out, err))
	}
}

// moduleRoot walks up from the working directory until it finds go.mod, so the
// generator works both from `go generate` (package directory) and from the
// repository root.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find go.mod above %s", dir)
		}
		dir = parent
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
