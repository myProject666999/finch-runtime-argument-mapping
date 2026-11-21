// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package argtranslate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitVolumeSource(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in     string
		src    string
		rest   string
		wantOK bool
	}{
		{in: "/host:/container", src: "/host", rest: ":/container", wantOK: true},
		{in: "/host:/container:ro", src: "/host", rest: ":/container:ro", wantOK: true},
		{in: `C:\work:/work`, src: `C:\work`, rest: ":/work", wantOK: true},
		{in: `C:/work:/work`, src: `C:/work`, rest: ":/work", wantOK: true},
		{in: "named-volume:/data", src: "named-volume", rest: ":/data", wantOK: true},
		{in: "/container-only", wantOK: false},
		{in: "", wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			src, rest, ok := splitVolumeSource(tc.in)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.src, src)
			assert.Equal(t, tc.rest, rest)
		})
	}
}

func TestLooksLikeHostPath(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"":             false,
		".":            true,
		"..":           true,
		"./src":        true,
		"../src":       true,
		"~/src":        true,
		"/absolute":    true,
		`C:\driven`:    true,
		"named-volume": false,
		"vol":          false,
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, looksLikeHostPath(in))
		})
	}
}

func TestTransformMountKeepsOrderAndDropsConsistency(t *testing.T) {
	t.Parallel()
	opts := Options{PathMapper: prefixMapper{prefix: "M:"}}
	got, warns, err := transformMount("target=/t,type=bind,source=./s,consistency=delegated,readonly", opts)
	require.NoError(t, err)
	assert.Equal(t, "target=/t,type=bind,source=M:./s,readonly", got)
	require.Len(t, warns, 1)
	assert.Contains(t, warns[0], "--mount")
	assert.Contains(t, warns[0], "consistency=delegated")
}

func TestTransformMountWithoutSource(t *testing.T) {
	t.Parallel()
	got, warns, err := transformMount("type=bind,target=/t,consistency=cached", Options{})
	require.NoError(t, err)
	assert.Equal(t, "type=bind,target=/t", got)
	require.Len(t, warns, 1)
}

func TestTransformKeyValueListLeavesUnrelatedKeysAlone(t *testing.T) {
	t.Parallel()
	opts := Options{PathMapper: prefixMapper{prefix: "M:"}}
	got, err := transformKeyValueList("type=local,dest=./out,mode=max", opts, "dest")
	require.NoError(t, err)
	assert.Equal(t, "type=local,dest=M:./out,mode=max", got)

	got, err = transformKeyValueList("id=secret,src=./key,env=FOO", opts, "src")
	require.NoError(t, err)
	assert.Equal(t, "id=secret,src=M:./key,env=FOO", got)
}

func TestTransformKeyValueListWithoutDestination(t *testing.T) {
	t.Parallel()
	got, err := transformKeyValueList("type=local", Options{PathMapper: prefixMapper{prefix: "M:"}}, "dest")
	require.NoError(t, err)
	assert.Equal(t, "type=local", got)
}

func TestMapPathWithoutMapperIsIdentity(t *testing.T) {
	t.Parallel()
	got, err := mapPath("/some/path", Options{})
	require.NoError(t, err)
	assert.Equal(t, "/some/path", got)
}

func TestIsBoolLiteral(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"true", "false", "1", "0", "t", "f", "TRUE", "False"} {
		assert.True(t, isBoolLiteral(v), v)
	}
	for _, v := range []string{"yes", "no", "", "2", "on"} {
		assert.False(t, isBoolLiteral(v), v)
	}
}
