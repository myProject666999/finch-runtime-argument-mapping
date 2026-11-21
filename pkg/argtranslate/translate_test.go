// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package argtranslate

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// prefixMapper makes the mapping visible in the expected argument list.
type prefixMapper struct{ prefix string }

func (m prefixMapper) MapHostPath(p string) (string, error) { return m.prefix + p, nil }

type failingMapper struct{}

func (failingMapper) MapHostPath(string) (string, error) { return "", errors.New("mapper failed") }

type gatewayResolver struct{}

func (gatewayResolver) ResolveHostGateway(string) (string, error) {
	return "192.0.2.1", nil
}

func envFrom(pairs map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := pairs[key]
		return v, ok
	}
}

func noEnv(string) (string, bool) { return "", false }

type translateTest struct {
	name string
	sub  string
	args []string
	opts *Options
	want []string
	// warnings contains substrings, one per expected warning.
	warnings []string
	// errContains lists substrings that must appear in the single joined error.
	errContains []string
	// noError asserts that no error was returned.
	noError bool
}

func runTests(t *testing.T, cases []translateTest) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := Options{Subcommand: tc.sub, LookupEnv: noEnv}
			if tc.opts != nil {
				opts = *tc.opts
				opts.Subcommand = tc.sub
				if opts.LookupEnv == nil {
					opts.LookupEnv = noEnv
				}
			}
			res := Translate(tc.args, opts)
			assert.Equal(t, tc.want, res.Args)
			for _, w := range tc.warnings {
				assert.Contains(t, strings.Join(res.Warnings, "\n"), w)
			}
			joined := joinErrors(res.Errors)
			for _, e := range tc.errContains {
				assert.Contains(t, joined, e)
			}
			if tc.noError {
				assert.Empty(t, res.Errors)
			}
			if len(tc.errContains) == 0 && !tc.noError {
				assert.Empty(t, joined)
			}
		})
	}
}

func joinErrors(errs []error) string {
	parts := make([]string, 0, len(errs))
	for _, err := range errs {
		parts = append(parts, err.Error())
	}
	return strings.Join(parts, "\n")
}

func TestTranslateRun(t *testing.T) {
	t.Parallel()
	runTests(t, []translateTest{
		{
			name:    "empty argument list",
			sub:     "run",
			args:    nil,
			want:    []string{},
			noError: true,
		},
		{
			name:    "bool flags pass through",
			sub:     "run",
			args:    []string{"-it", "--rm", "alpine"},
			want:    []string{"--interactive", "--tty", "--rm", "alpine"},
			noError: true,
		},
		{
			name:    "long flag with equals and with space",
			sub:     "run",
			args:    []string{"--name=one", "--hostname", "two", "alpine"},
			want:    []string{"--name", "one", "--hostname", "two", "alpine"},
			noError: true,
		},
		{
			name:    "short cluster with trailing value",
			sub:     "run",
			args:    []string{"-dit", "-p8080:80", "alpine"},
			want:    []string{"--detach", "--interactive", "--tty", "--publish", "8080:80", "alpine"},
			noError: true,
		},
		{
			name:    "short cluster with separated value",
			sub:     "run",
			args:    []string{"-dp", "8080:80", "alpine"},
			want:    []string{"--detach", "--publish", "8080:80", "alpine"},
			noError: true,
		},
		{
			name:    "short flag with equals value",
			sub:     "run",
			args:    []string{"-e=FOO=bar", "alpine"},
			want:    []string{"--env", "FOO=bar", "alpine"},
			noError: true,
		},
		{
			name:     "repeated bool flags are all forwarded",
			sub:      "run",
			args:     []string{"--rm", "--rm", "alpine"},
			want:     []string{"--rm", "--rm", "alpine"},
			noError:  true,
			warnings: nil,
		},
		{
			name:     "repeated last-wins flag keeps only the last",
			sub:      "run",
			args:     []string{"--name", "a", "--name=b", "alpine"},
			want:     []string{"--name", "b", "alpine"},
			warnings: []string{"ignoring the earlier --name"},
		},
		{
			name:    "repeated repeatable flag keeps every occurrence",
			sub:     "run",
			args:    []string{"-e", "A=1", "-e", "B=2", "-e", "A=3", "alpine"},
			want:    []string{"--env", "A=1", "--env", "B=2", "--env", "A=3", "alpine"},
			noError: true,
		},
		{
			name:    "double dash stops translation",
			sub:     "run",
			args:    []string{"--rm", "alpine", "--", "--rm", "--hostname", "x"},
			want:    []string{"--rm", "alpine", "--", "--rm", "--hostname", "x"},
			noError: true,
		},
		{
			name:    "double dash before the image keeps everything as payload",
			sub:     "run",
			args:    []string{"--rm", "--", "--expose", "80"},
			want:    []string{"--rm", "--", "--expose", "80"},
			noError: true,
		},
		{
			name:    "container command arguments are never translated",
			sub:     "run",
			args:    []string{"-it", "alpine", "sh", "-c", "echo --rm --expose 80"},
			want:    []string{"--interactive", "--tty", "alpine", "sh", "-c", "echo --rm --expose 80"},
			noError: true,
		},
		{
			name:    "value beginning with a dash is consumed",
			sub:     "run",
			args:    []string{"--health-retries", "-5", "--health-timeout", "-5s", "alpine"},
			want:    []string{"--health-retries", "-5", "--health-timeout", "-5s", "alpine"},
			noError: true,
		},
		{
			name:    "value that looks like a flag is consumed",
			sub:     "run",
			args:    []string{"--label", "--weird", "alpine"},
			want:    []string{"--label", "--weird", "alpine"},
			noError: true,
		},
		{
			name:     "bare env name is resolved on the host",
			sub:      "run",
			args:     []string{"-e", "HOME", "alpine"},
			opts:     &Options{LookupEnv: envFrom(map[string]string{"HOME": "/home/me"})},
			want:     []string{"--env", "HOME=/home/me", "alpine"},
			noError:  true,
			warnings: nil,
		},
		{
			name:     "bare env name with no host value is dropped with a warning",
			sub:      "run",
			args:     []string{"-e", "MISSING", "alpine"},
			want:     []string{"alpine"},
			warnings: []string{"MISSING", "not set in the host environment"},
		},
		{
			name:     "kernel-memory is ignored with a rationale",
			sub:      "run",
			args:     []string{"--kernel-memory", "1G", "alpine"},
			want:     []string{"alpine"},
			warnings: []string{"--kernel-memory"},
		},
		{
			name:    "cpu-quota zero becomes unset",
			sub:     "run",
			args:    []string{"--cpu-quota", "0", "alpine"},
			want:    []string{"--cpu-quota", "-1", "alpine"},
			noError: true,
		},
		{
			name:    "cpu-quota nonzero is untouched",
			sub:     "run",
			args:    []string{"--cpu-quota", "5000", "alpine"},
			want:    []string{"--cpu-quota", "5000", "alpine"},
			noError: true,
		},
		{
			name:    "net is renamed to network",
			sub:     "run",
			args:    []string{"--net", "host", "alpine"},
			want:    []string{"--network", "host", "alpine"},
			noError: true,
		},
		{
			name:    "volume source is mapped and named volume is not",
			sub:     "run",
			args:    []string{"-v", "./src:/dst:ro", "-v", "named:/data", "alpine"},
			opts:    &Options{PathMapper: prefixMapper{prefix: "M:"}},
			want:    []string{"--volume", "M:./src:/dst:ro", "--volume", "named:/data", "alpine"},
			noError: true,
		},
		{
			name:    "windows volume source is not mistaken for a drive separator",
			sub:     "run",
			args:    []string{"-v", `C:\work:/work`, "alpine"},
			opts:    &Options{PathMapper: prefixMapper{prefix: "M:"}},
			want:    []string{"--volume", `M:C:\work:/work`, "alpine"},
			noError: true,
		},
		{
			name:     "bind mount source is mapped and consistency dropped",
			sub:      "run",
			args:     []string{"--mount", "dst=/b,type=bind,src=./a,consistency=cached", "alpine"},
			opts:     &Options{PathMapper: prefixMapper{prefix: "M:"}},
			want:     []string{"--mount", "dst=/b,type=bind,src=M:./a", "alpine"},
			warnings: []string{"--mount", "consistency=cached"},
		},
		{
			name:    "non-bind mount is not path mapped",
			sub:     "run",
			args:    []string{"--mount", "type=volume,src=vol,dst=/b", "alpine"},
			opts:    &Options{PathMapper: prefixMapper{prefix: "M:"}},
			want:    []string{"--mount", "type=volume,src=vol,dst=/b", "alpine"},
			noError: true,
		},
		{
			name:    "mount keeps the order the user wrote",
			sub:     "run",
			args:    []string{"--mount", "type=bind,target=/app,source=./app", "alpine"},
			opts:    &Options{PathMapper: prefixMapper{prefix: "M:"}},
			want:    []string{"--mount", "type=bind,target=/app,source=M:./app", "alpine"},
			noError: true,
		},
		{
			name:    "host-gateway is resolved",
			sub:     "run",
			args:    []string{"--add-host", "db:host-gateway", "alpine"},
			opts:    &Options{HostGateway: gatewayResolver{}},
			want:    []string{"--add-host", "db:192.0.2.1", "alpine"},
			noError: true,
		},
		{
			name:    "host-gateway with an explicit ip is untouched",
			sub:     "run",
			args:    []string{"--add-host=db:10.0.0.1", "alpine"},
			opts:    &Options{HostGateway: gatewayResolver{}},
			want:    []string{"--add-host", "db:10.0.0.1", "alpine"},
			noError: true,
		},
		{
			name:        "add-host without a colon is a usage error, not a panic",
			sub:         "run",
			args:        []string{"--add-host", "nodb", "alpine"},
			want:        []string{"alpine"},
			errContains: []string{"--add-host", "HOST:IP"},
		},
		{
			name:     "debug is consumed by finch",
			sub:      "run",
			args:     []string{"--debug", "alpine"},
			want:     []string{"alpine"},
			noError:  true,
			warnings: nil,
		},
		{
			name:        "a single unsupported flag names the flag, the sub-command, and the doc",
			sub:         "run",
			args:        []string{"--expose", "80", "alpine"},
			want:        []string{"alpine"},
			errContains: []string{"--expose", "finch run", "docs/arg-mapping.md#finch-run"},
		},
		{
			name:        "several unsupported flags are reported together",
			sub:         "run",
			args:        []string{"--expose", "80", "--publish-all", "--volume-driver", "x", "--storage-opt", "size=1G", "alpine"},
			want:        []string{"alpine"},
			errContains: []string{"--expose", "--publish-all", "--volume-driver", "--storage-opt", "flags are not supported"},
		},
		{
			name:        "an unknown flag is unsupported, not forwarded",
			sub:         "run",
			args:        []string{"--no-such-flag=1", "alpine"},
			want:        []string{"alpine"},
			errContains: []string{"--no-such-flag"},
		},
		{
			name:        "an unknown short flag is unsupported",
			sub:         "run",
			args:        []string{"-Z", "alpine"},
			want:        []string{"alpine"},
			errContains: []string{"-Z"},
		},
		{
			name:        "an unsupported flag inside a cluster is reported by letter",
			sub:         "run",
			args:        []string{"-dZ", "alpine"},
			want:        []string{"--detach", "alpine"},
			errContains: []string{"-Z"},
		},
		{
			name:        "a missing value is a usage error",
			sub:         "run",
			args:        []string{"--name"},
			want:        []string{},
			errContains: []string{"--name", "expects a value"},
		},
		{
			name:        "a bad boolean literal is a usage error",
			sub:         "run",
			args:        []string{"--rm=yes", "alpine"},
			want:        []string{"alpine"},
			errContains: []string{"--rm", "not a boolean"},
		},
	})
}

func TestTranslateCreate(t *testing.T) {
	t.Parallel()
	runTests(t, []translateTest{
		{
			name:    "create shares the run matrix",
			sub:     "create",
			args:    []string{"-it", "--name", "c", "-v", "./a:/b", "alpine"},
			want:    []string{"--interactive", "--tty", "--name", "c", "--volume", "./a:/b", "alpine"},
			noError: true,
		},
		{
			name:        "attach is unsupported for create",
			sub:         "create",
			args:        []string{"-a", "stdout", "alpine"},
			want:        []string{"alpine"},
			errContains: []string{"-a", "finch create"},
		},
		{
			name:        "detach is not a create flag",
			sub:         "create",
			args:        []string{"-d", "alpine"},
			want:        []string{"alpine"},
			errContains: []string{"-d"},
		},
		{
			name:    "create still maps paths",
			sub:     "create",
			args:    []string{"--mount", "type=bind,source=./s,target=/t", "alpine"},
			opts:    &Options{PathMapper: prefixMapper{prefix: "M:"}},
			want:    []string{"--mount", "type=bind,source=M:./s,target=/t", "alpine"},
			noError: true,
		},
	})
}

func TestTranslateExec(t *testing.T) {
	t.Parallel()
	runTests(t, []translateTest{
		{
			name:    "exec flags are translated before the container",
			sub:     "exec",
			args:    []string{"-it", "-u", "1000", "web", "sh"},
			want:    []string{"--interactive", "--tty", "--user", "1000", "web", "sh"},
			noError: true,
		},
		{
			name:    "exec container command keeps its own flags",
			sub:     "exec",
			args:    []string{"-it", "web", "sh", "-c", "--detach-keys=x"},
			want:    []string{"--interactive", "--tty", "web", "sh", "-c", "--detach-keys=x"},
			noError: true,
		},
		{
			name:        "detach-keys is unsupported for exec",
			sub:         "exec",
			args:        []string{"--detach-keys", "ctrl-a", "web", "sh"},
			want:        []string{"web", "sh"},
			errContains: []string{"--detach-keys", "finch exec"},
		},
	})
}

func TestTranslateBuild(t *testing.T) {
	t.Parallel()
	runTests(t, []translateTest{
		{
			name:    "build flags before and after the context",
			sub:     "build",
			args:    []string{"-t", "img", ".", "--no-cache"},
			want:    []string{"--tag", "img", "--no-cache", "."},
			noError: true,
		},
		{
			name:    "load becomes an output type",
			sub:     "build",
			args:    []string{"--load", "-t", "img", "."},
			want:    []string{"--output=type=docker", "--tag", "img", "."},
			noError: true,
		},
		{
			name:    "dockerfile, secret, and output destinations are mapped",
			sub:     "build",
			args:    []string{"-f", "Dockerfile", "--secret", "id=s,src=./s", "--output", "type=local,dest=./out", "."},
			opts:    &Options{PathMapper: prefixMapper{prefix: "M:"}},
			want:    []string{"--file", "M:Dockerfile", "--secret", "id=s,src=M:./s", "--output", "type=local,dest=M:./out", "M:."},
			noError: true,
		},
		{
			name:    "cache source and destination are mapped",
			sub:     "build",
			args:    []string{"--cache-from", "type=local,src=./c1", "--cache-to", "type=local,dest=./c2", "."},
			opts:    &Options{PathMapper: prefixMapper{prefix: "M:"}},
			want:    []string{"--cache-from", "type=local,src=M:./c1", "--cache-to", "type=local,dest=M:./c2", "M:."},
			noError: true,
		},
		{
			name:    "build context after a name= prefix is mapped",
			sub:     "build",
			args:    []string{"--build-context", "deps=./deps", "."},
			opts:    &Options{PathMapper: prefixMapper{prefix: "M:"}},
			want:    []string{"--build-context", "deps=M:./deps", "M:."},
			noError: true,
		},
		{
			name:        "legacy builder flags are rejected",
			sub:         "build",
			args:        []string{"--squash", "--compress", "--memory", "1G", "."},
			want:        []string{"."},
			errContains: []string{"--squash", "--compress", "--memory"},
		},
		{
			name:        "push is reported instead of forwarded",
			sub:         "build",
			args:        []string{"--push", "."},
			want:        []string{"."},
			errContains: []string{"--push"},
		},
	})
}

func TestTranslateInjectedDefaults(t *testing.T) {
	t.Parallel()
	env := envFrom(map[string]string{"DOCKER_DEFAULT_PLATFORM": "linux/arm64"})

	for _, sub := range []string{"run", "create", "build"} {
		t.Run(sub+" injects the platform default", func(t *testing.T) {
			t.Parallel()
			res := Translate([]string{"alpine"}, Options{Subcommand: sub, LookupEnv: env})
			require.Empty(t, res.Errors)
			assert.Equal(t, []string{"--platform", "linux/arm64", "alpine"}, res.Args)
		})
	}

	t.Run("no injection when the flag is present", func(t *testing.T) {
		t.Parallel()
		res := Translate([]string{"--platform", "amd64", "alpine"}, Options{Subcommand: "run", LookupEnv: env})
		require.Empty(t, res.Errors)
		assert.Equal(t, []string{"--platform", "amd64", "alpine"}, res.Args)
	})

	t.Run("no injection when the variable is unset", func(t *testing.T) {
		t.Parallel()
		res := Translate([]string{"alpine"}, Options{Subcommand: "run", LookupEnv: noEnv})
		require.Empty(t, res.Errors)
		assert.Equal(t, []string{"alpine"}, res.Args)
	})

	t.Run("no injection for exec", func(t *testing.T) {
		t.Parallel()
		res := Translate([]string{"web", "sh"}, Options{Subcommand: "exec", LookupEnv: env})
		require.Empty(t, res.Errors)
		assert.Equal(t, []string{"web", "sh"}, res.Args)
	})

	t.Run("default is injected before the payload, not after", func(t *testing.T) {
		t.Parallel()
		res := Translate([]string{"-t", "img", "."}, Options{Subcommand: "build", LookupEnv: env})
		require.Empty(t, res.Errors)
		assert.Equal(t, []string{"--tag", "img", "--platform", "linux/arm64", "."}, res.Args)
	})
}

func TestTranslateLenient(t *testing.T) {
	t.Parallel()
	runTests(t, []translateTest{
		{
			name: "unsupported flags become warnings",
			sub:  "run",
			args: []string{"--expose", "80", "--publish-all", "alpine"},
			opts: &Options{Lenient: true},
			want: []string{"alpine"},
			warnings: []string{
				"dropping unsupported flag --expose",
				"dropping unsupported flag --publish-all",
			},
		},
		{
			name:     "ignored flags keep their rationale warning",
			sub:      "run",
			args:     []string{"--kernel-memory", "1G", "alpine"},
			opts:     &Options{Lenient: true},
			want:     []string{"alpine"},
			warnings: []string{"--kernel-memory", "no-op"},
		},
	})
}

func TestTranslateStrictAndLenientAgreeOnSupportedFlags(t *testing.T) {
	t.Parallel()
	args := []string{"-it", "--rm", "-e", "A=1", "-v", "./a:/b", "alpine", "echo", "--rm"}
	strict := Translate(args, Options{Subcommand: "run", LookupEnv: noEnv})
	lenient := Translate(args, Options{Subcommand: "run", Lenient: true, LookupEnv: noEnv})
	assert.Equal(t, strict.Args, lenient.Args)
	require.Empty(t, strict.Errors)
	require.Empty(t, lenient.Errors)
}

func TestTranslateMapperErrorIsReported(t *testing.T) {
	t.Parallel()
	res := Translate([]string{"-v", "/src:/dst", "alpine"}, Options{
		Subcommand: "run",
		PathMapper: failingMapper{},
		LookupEnv:  noEnv,
	})
	require.Len(t, res.Errors, 1)
	assert.Contains(t, res.Errors[0].Error(), "mapper failed")
}

func TestTranslateUnknownSubcommand(t *testing.T) {
	t.Parallel()
	res := Translate([]string{"a", "b"}, Options{Subcommand: "compose"})
	assert.Equal(t, []string{"a", "b"}, res.Args)
	require.Len(t, res.Errors, 1)
	assert.Contains(t, res.Errors[0].Error(), "compose")
}

func TestTranslateOutputIsDeterministic(t *testing.T) {
	t.Parallel()
	args := []string{"--mount", "type=bind,src=./a,target=/t,readonly", "-e", "A=1", "--name", "n", "alpine"}
	opts := Options{Subcommand: "run", PathMapper: prefixMapper{prefix: "M:"}, LookupEnv: noEnv}
	want := Translate(args, opts).Args
	for i := 0; i < 50; i++ {
		assert.Equal(t, want, Translate(args, opts).Args)
	}
}

func TestTranslateWarningsNameTheirFlag(t *testing.T) {
	t.Parallel()
	matrix := []struct {
		sub  string
		args []string
		flag string
	}{
		{sub: "run", args: []string{"--kernel-memory", "1G", "a"}, flag: "--kernel-memory"},
		{sub: "run", args: []string{"-e", "NOPE", "a"}, flag: "NOPE"},
		{sub: "run", args: []string{"--mount", "type=bind,src=./a,dst=/b,consistency=cached", "a"}, flag: "--mount"},
		{sub: "run", args: []string{"--name", "a", "--name", "b", "x"}, flag: "--name"},
		{sub: "build", args: []string{"--disable-content-trust", "."}, flag: "--disable-content-trust"},
	}
	for _, tc := range matrix {
		t.Run(tc.flag, func(t *testing.T) {
			t.Parallel()
			res := Translate(tc.args, Options{Subcommand: tc.sub, LookupEnv: noEnv})
			joined := strings.Join(res.Warnings, "\n")
			require.NotEmpty(t, res.Warnings, "expected a warning about %s", tc.flag)
			assert.Contains(t, joined, tc.flag)
		})
	}
}

func TestUnsupportedErrorFormatsAllFlags(t *testing.T) {
	t.Parallel()
	err := &UnsupportedError{Subcommand: "run", Flags: []string{"--expose", "--link"}}
	msg := err.Error()
	assert.Contains(t, msg, "flags are not supported by finch run")
	assert.Contains(t, msg, "--expose, --link")
	assert.Contains(t, msg, "docs/arg-mapping.md#finch-run")

	single := (&UnsupportedError{Subcommand: "exec", Flags: []string{"--detach-keys"}}).Error()
	assert.Contains(t, single, "flag is not supported")
	assert.Contains(t, single, "finch exec")
}

func TestUsageErrorNamesTheSubcommand(t *testing.T) {
	t.Parallel()
	res := Translate([]string{"--add-host", "nodb", "alpine"}, Options{Subcommand: "run", LookupEnv: noEnv})
	require.Len(t, res.Errors, 1)
	assert.Contains(t, res.Errors[0].Error(), "finch run")
	assert.NotContains(t, res.Errors[0].Error(), "finch add-host")
}

// TestEveryRepeatableShortFlagHasAKnownKind guards against a typo in a Kind that
// would silently make the parser stop consuming the next token.
func TestEveryRepeatableShortFlagHasAKnownKind(t *testing.T) {
	t.Parallel()
	for _, m := range Matrices() {
		for _, e := range m.Entries {
			switch e.Kind {
			case KindBool, KindValue, KindValueRepeat:
			default:
				t.Errorf("finch %s: --%s has an unknown kind %d", m.Subcommand, e.Name, e.Kind)
			}
		}
	}
}
