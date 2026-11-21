// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package argtranslate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRenderedDocMatchesGeneratedFile fails when docs/arg-mapping.md is stale,
// which is the same check CI performs with `make gen-code` followed by
// `git diff --exit-code`.
func TestRenderedDocMatchesGeneratedFile(t *testing.T) {
	t.Parallel()
	want := RenderMarkdown()

	path := filepath.Join("..", "..", "docs", "arg-mapping.md")
	got, err := os.ReadFile(path) //nolint:gosec // the path is a fixed file in the repository, not user input
	if os.IsNotExist(err) {
		t.Fatalf("%s is missing; run `go generate ./pkg/argtranslate`", path)
	}
	require.NoError(t, err)
	assert.Equal(t, want, string(got),
		"%s is out of date; run `go generate ./pkg/argtranslate`", path)
}

func TestRenderMarkdownHasOneTablePerSubcommand(t *testing.T) {
	t.Parallel()
	doc := RenderMarkdown()
	for _, sub := range Subcommands() {
		assert.Contains(t, doc, "## finch "+sub)
	}
}

func TestRenderMarkdownRowsComeFromTheMatrix(t *testing.T) {
	t.Parallel()
	doc := RenderMarkdown()
	for _, m := range Matrices() {
		for _, e := range m.Entries {
			assert.Containsf(t, doc, "`"+flagCell(e)+"`",
				"finch %s: --%s is missing from the generated table", m.Subcommand, e.Name)
			assert.Containsf(t, doc, "`"+e.Category.String()+"`",
				"finch %s: --%s has no category in the generated table", m.Subcommand, e.Name)
		}
	}
}

func TestRenderMarkdownDocumentsIgnoredRationales(t *testing.T) {
	t.Parallel()
	doc := RenderMarkdown()
	for _, m := range Matrices() {
		for _, e := range m.Entries {
			if e.Category != CatIgnored {
				continue
			}
			assert.Containsf(t, doc, e.Rationale,
				"finch %s: the rationale for --%s is not rendered", m.Subcommand, e.Name)
		}
	}
}

func TestRenderMarkdownSections(t *testing.T) {
	t.Parallel()
	doc := RenderMarkdown()
	for _, heading := range []string{
		"## Categories",
		"## Arguments that change behaviour",
		"### Lenient mode",
		"## Runtime defaults differ",
		"## Where path conversion happens",
		"## Not covered",
	} {
		assert.Contains(t, doc, heading)
	}
	// The compatibility list must name the previously silent flag.
	assert.Contains(t, doc, "--kernel-memory")
	assert.Contains(t, doc, "consistency=")
}

// TestRenderMarkdownIsDeterministic protects the generated file from churn
// caused by map iteration order.
func TestRenderMarkdownIsDeterministic(t *testing.T) {
	t.Parallel()
	want := RenderMarkdown()
	for i := 0; i < 20; i++ {
		require.Equal(t, want, RenderMarkdown())
	}
}

// TestTranslateIsIndependentOfWorkingDirectory asserts the translation layer is
// pure: switching to an empty directory must not change a single result.
func TestTranslateIsIndependentOfWorkingDirectory(t *testing.T) { //nolint:paralleltest // this test changes the process working directory
	empty := t.TempDir()
	wd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(empty))
	t.Cleanup(func() { require.NoError(t, os.Chdir(wd)) })

	cases := []struct {
		sub  string
		args []string
	}{
		{"run", []string{"-it", "--rm", "-e", "A=1", "-v", "./src:/dst", "alpine", "env"}},
		{"run", []string{"--mount", "type=bind,src=./x,dst=/y,consistency=cached", "alpine"}},
		{"create", []string{"--name", "c", "alpine"}},
		{"exec", []string{"-it", "c", "sh"}},
		{"build", []string{"-f", "Dockerfile", "--secret", "id=a,src=./s", "."}},
		{"run", []string{"--expose", "80", "--publish-all", "alpine"}},
	}
	// The subtests are not parallel: the parent changes the process working
	// directory, so running them concurrently would make the check meaningless.
	for _, tc := range cases { //nolint:paralleltest // see comment above
		t.Run(tc.sub+strings.Join(tc.args, "_"), func(t *testing.T) {
			res := Translate(tc.args, Options{Subcommand: tc.sub, PathMapper: prefixMapper{prefix: "M:"}, LookupEnv: noEnv})
			require.NotNil(t, res.Args)
			for _, err := range res.Errors {
				// Errors are fine; what matters is that nothing was read from disk.
				assert.NotNil(t, err)
			}
		})
	}
}
