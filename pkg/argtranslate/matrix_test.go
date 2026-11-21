// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package argtranslate

import (
	_ "embed"
	"encoding/json"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// snapshotJSON is a local snapshot of the runtime's flag tables. It is checked
// in so the test never depends on the network or on a machine having a
// particular nerdctl version installed. Regenerate it from the runtime sources
// named in the snapshot's "source" field.
//
//go:embed testdata/nerdctl_flags.json
var snapshotJSON []byte

type snapshotFlag struct {
	Name  string `json:"name"`
	Short string `json:"short"`
	Kind  string `json:"kind"`
}

type snapshot struct {
	Source      string                    `json:"source"`
	Runtime     string                    `json:"runtime"`
	Subcommands map[string][]snapshotFlag `json:"subcommands"`
}

func loadSnapshot(t *testing.T) snapshot {
	t.Helper()
	var s snapshot
	require.NoError(t, json.Unmarshal(snapshotJSON, &s))
	require.NotEmpty(t, s.Subcommands)
	return s
}

// TestMatrixCoversEveryMatrixFlag is the forward direction: every flag the
// matrix classifies must be a flag the runtime actually understands, unless it
// is intentionally rejected. A flag the runtime does not have, but the matrix
// passes through, would fail at run time with an opaque error.
func TestMatrixCoversEveryMatrixFlag(t *testing.T) {
	t.Parallel()
	snap := loadSnapshot(t)

	for _, m := range Matrices() {
		runtimeFlags, ok := snap.Subcommands[m.Subcommand]
		require.Truef(t, ok, "snapshot has no entry for %q", m.Subcommand)
		runtime := make(map[string]bool, len(runtimeFlags))
		for _, f := range runtimeFlags {
			runtime[f.Name] = true
		}

		for _, e := range m.Entries {
			if e.Category == CatUnsupported {
				// Rejected on purpose; it does not need to exist in the runtime.
				continue
			}
			if e.Category == CatIgnored || e.FinchOnly {
				// Not forwarded, so the runtime does not need to know it.
				continue
			}
			if e.Name == "help" {
				// cobra adds --help to every command, so it is never in the
				// snapshot taken from the flag registration code.
				continue
			}
			name := e.Name
			if e.Category == CatRewrite && e.RuntimeName != "" {
				name = e.RuntimeName
			}
			assert.Truef(t, runtime[name],
				"finch %s classifies %q as %s but the runtime (%s) has no such flag",
				m.Subcommand, e.Name, e.Category, snap.Runtime)
		}
	}
}

// TestSnapshotCoveredByMatrix is the reverse direction: every runtime flag
// should have a row. It only warns, because the runtime may add flags between
// finch releases and that must not break the build.
func TestSnapshotCoveredByMatrix(t *testing.T) {
	t.Parallel()
	snap := loadSnapshot(t)

	for _, m := range Matrices() {
		classified := make(map[string]bool, len(m.Entries))
		for _, e := range m.Entries {
			classified[e.Name] = true
			if e.RuntimeName != "" {
				classified[e.RuntimeName] = true
			}
		}
		var unclassified []string
		for _, f := range snap.Subcommands[m.Subcommand] {
			if !classified[f.Name] {
				unclassified = append(unclassified, f.Name)
			}
		}
		sort.Strings(unclassified)
		if len(unclassified) > 0 {
			t.Logf("finch %s: runtime flags with no matrix row (add them to pkg/argtranslate/matrix.go): %v",
				m.Subcommand, unclassified)
		}
	}
}

func TestSnapshotMatchesMatrixKind(t *testing.T) {
	t.Parallel()
	snap := loadSnapshot(t)
	for _, m := range Matrices() {
		runtimeKind := make(map[string]string)
		for _, f := range snap.Subcommands[m.Subcommand] {
			runtimeKind[f.Name] = f.Kind
		}
		for _, e := range m.Entries {
			if e.Category == CatUnsupported || e.Category == CatIgnored || e.FinchOnly {
				continue
			}
			got, ok := runtimeKind[e.Name]
			if !ok {
				continue // reported by the coverage test
			}
			want := "value"
			if e.Kind == KindBool {
				want = "bool"
			}
			assert.Equalf(t, want, got,
				"finch %s: %q is %s in the matrix but %s in the runtime", m.Subcommand, e.Name, e.Kind, got)
		}
	}
}

func TestShortFlagsDoNotCollide(t *testing.T) {
	t.Parallel()
	for _, m := range Matrices() {
		seen := map[string]string{}
		for _, e := range m.Entries {
			if e.Short == "" {
				continue
			}
			if other, ok := seen[e.Short]; ok {
				t.Errorf("finch %s: -%s is claimed by both --%s and --%s", m.Subcommand, e.Short, other, e.Name)
			}
			seen[e.Short] = e.Name
		}
	}
}

func TestSharedEntriesAgreeBetweenRunAndCreate(t *testing.T) {
	t.Parallel()
	run, ok := Lookup("run")
	require.True(t, ok)
	create, ok := Lookup("create")
	require.True(t, ok)

	for _, e := range create.Entries {
		// Flags that exist in both must agree exactly; create may only add
		// flags run does not have, or mark one unsupported.
		other, ok := run.Long(e.Name)
		if !ok || e.Category == CatUnsupported {
			continue
		}
		assert.Equal(t, other, e, "--%s differs between run and create", e.Name)
	}
}

func TestMatrixIsSortedAndComplete(t *testing.T) {
	t.Parallel()
	for _, m := range Matrices() {
		assert.True(t, sort.SliceIsSorted(m.Entries, func(i, j int) bool {
			return m.Entries[i].Name < m.Entries[j].Name
		}), "finch %s entries are not sorted by name", m.Subcommand)

		for _, e := range m.Entries {
			assert.NotEmpty(t, e.Name)
			if e.Category == CatIgnored {
				assert.NotEmptyf(t, e.Rationale,
					"finch %s: ignored flag --%s must explain why dropping it is harmless", m.Subcommand, e.Name)
			}
			if e.Category == CatSpecial {
				assert.NotEmptyf(t, string(e.Special),
					"finch %s: special flag --%s must name its handler", m.Subcommand, e.Name)
			}
			if e.Category == CatRewrite {
				assert.True(t, e.RuntimeName != "" || e.RuntimeValue != "",
					"finch %s: rewritten flag --%s must say what it becomes", m.Subcommand, e.Name)
			}
			if e.Category == CatUnsupported {
				assert.NotEmptyf(t, e.Note,
					"finch %s: unsupported flag --%s must explain why", m.Subcommand, e.Name)
			}
		}
	}
}

func TestEverySubcommandHasAMatrix(t *testing.T) {
	t.Parallel()
	for _, sub := range Subcommands() {
		m, ok := Lookup(sub)
		require.True(t, ok, "no matrix for %q", sub)
		assert.NotEmpty(t, m.Entries)
	}
}
