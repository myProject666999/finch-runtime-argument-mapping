// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package argtranslate

import (
	"errors"
	"fmt"
	"strings"
)

// PathMapper converts a host path into the path the runtime sees. Finch's
// macOS implementation is the identity because the host home directory is
// mounted into the VM at the same path; the Windows implementation maps a drive
// letter to /mnt/<drive>.
//
// The translator only decides which values are host paths. It never knows how
// to convert them, so a path is converted exactly once.
type PathMapper interface {
	MapHostPath(path string) (string, error)
}

// HostGatewayResolver resolves the docker-specific "host-gateway" pseudo IP to
// an address reachable from containers.
type HostGatewayResolver interface {
	ResolveHostGateway(host string) (string, error)
}

// Options configures a Translate call.
type Options struct {
	// Subcommand is the canonical finch sub-command, one of Subcommands().
	Subcommand string
	// Lenient downgrades unsupported flags to warnings and drops them.
	Lenient bool
	// PathMapper converts host paths. A nil mapper leaves paths unchanged.
	PathMapper PathMapper
	// HostGateway resolves "host-gateway". A nil resolver leaves it unchanged.
	HostGateway HostGatewayResolver
	// LookupEnv looks up an environment variable on the host. It is used to
	// resolve a bare "--env NAME" argument and to read DOCKER_DEFAULT_PLATFORM.
	// A nil LookupEnv treats every variable as unset.
	LookupEnv func(key string) (string, bool)
}

// Result is the output of Translate.
type Result struct {
	// Args is the translated argument list for the runtime.
	Args []string
	// Warnings describe arguments that were dropped or adjusted. Each warning
	// names the flag it is about.
	Warnings []string
	// Errors are the problems that prevent the command from running. Unsupported
	// flags are always reported together in a single UnsupportedError.
	Errors []error
	// FinchFlags are finch-level flags the translator consumed. The caller is
	// responsible for acting on them; they are never passed to the runtime.
	FinchFlags []string
}

// Translate converts docker-style arguments into the argument list the runtime
// understands. It is pure: it does not execute commands or read files.
func Translate(args []string, opts Options) Result {
	if opts.Subcommand == "" {
		return Result{Args: append([]string(nil), args...)}
	}
	matrix, ok := Lookup(opts.Subcommand)
	if !ok {
		return Result{
			Args:   append([]string(nil), args...),
			Errors: []error{fmt.Errorf("unknown sub-command %q", opts.Subcommand)},
		}
	}

	p := &parser{subcommand: opts.Subcommand, matrix: matrix, splitAt: payloadSplit(opts.Subcommand)}
	parsed := p.parse(args)

	res := Result{}
	var unsupported []string
	unsupportedSet := map[string]bool{}

	res.Errors = append(res.Errors, parsed.usageErrors...)

	// Remember the position of the last occurrence of every last-wins flag so
	// earlier duplicates can be dropped.
	lastIndex := map[string]int{}
	for i, occ := range parsed.occurrences {
		if occ.known && occ.entry.Kind == KindValue {
			lastIndex[occ.entry.Name] = i
		}
	}

	// Inject defaults that the runtime does not apply but docker does.
	defaults := injectedDefaults(parsed, opts)

	var flags []string
	for i, occ := range parsed.occurrences {
		if !occ.known {
			unsupported = appendUnique(unsupported, unsupportedSet, occ.display)
			continue
		}
		entry := occ.entry
		switch entry.Category {
		case CatUnsupported:
			unsupported = appendUnique(unsupported, unsupportedSet, entry.Display())
			continue
		case CatIgnored:
			if entry.FinchOnly {
				res.FinchFlags = append(res.FinchFlags, entry.Display())
				continue
			}
			res.Warnings = append(res.Warnings, fmt.Sprintf(
				"ignoring %s for finch %s: %s", entry.Display(), opts.Subcommand, entry.Rationale))
			continue
		}
		if entry.Kind == KindValue && lastIndex[entry.Name] != i {
			// An earlier occurrence of a last-wins flag; the runtime would use
			// the later value, so forwarding this one would only add noise.
			res.Warnings = append(res.Warnings, fmt.Sprintf(
				"ignoring the earlier %s for finch %s: the last occurrence wins",
				entry.Display(), opts.Subcommand))
			continue
		}
		rendered, warns, err := renderOccurrence(occ, opts)
		res.Warnings = append(res.Warnings, warns...)
		if err != nil {
			res.Errors = append(res.Errors, err)
			continue
		}
		if len(rendered) == 0 {
			// The flag was accepted but its value resolved to nothing, for
			// example an unset variable named by -e.
			continue
		}
		flags = append(flags, rendered...)
	}

	if len(unsupported) > 0 {
		if opts.Lenient {
			for _, flag := range unsupported {
				res.Warnings = append(res.Warnings, fmt.Sprintf(
					"dropping unsupported flag %s for finch %s", flag, opts.Subcommand))
			}
		} else {
			res.Errors = append(res.Errors, &UnsupportedError{Subcommand: opts.Subcommand, Flags: unsupported})
		}
	}

	// Assemble: validated flags first, then injected defaults, then everything
	// from the image or container name onwards, untouched.
	payload := parsed.payload
	if opts.Subcommand == "build" && parsed.positionalAt >= 0 {
		// The build context is a host path, so it is translated like any other.
		mapped, err := mapPath(payload[0], opts)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("failed to map the build context: %w", err))
		} else {
			payload = append([]string(nil), payload...)
			payload[0] = mapped
		}
	}

	out := make([]string, 0, len(flags)+len(defaults)+len(payload)+1)
	out = append(out, flags...)
	out = append(out, defaults...)
	if parsed.passthrough {
		out = append(out, "--")
	}
	out = append(out, payload...)
	res.Args = out
	return res
}

// payloadSplit returns the number of leading positionals after which every
// remaining token is passed to the container untouched. exec treats the
// container name as the last parsed positional; build accepts options after its
// context, so it parses flags anywhere.
func payloadSplit(subcommand string) int {
	switch subcommand {
	case "run", "create", "exec":
		return 1
	default:
		return 0
	}
}

func appendUnique(list []string, seen map[string]bool, value string) []string {
	if seen[value] {
		return list
	}
	seen[value] = true
	return append(list, value)
}

// injectedDefaults computes the flags finch must add so that docker defaults
// and runtime defaults agree. See the "runtime defaults differ" section of
// docs/arg-mapping.md.
func injectedDefaults(parsed *parseResult, opts Options) []string {
	mentions := func(name string) bool {
		for _, occ := range parsed.occurrences {
			if occ.known && occ.entry.Name == name {
				return true
			}
		}
		return false
	}

	var defaults []string
	if mentions("platform") {
		return defaults
	}
	if !acceptsPlatform(opts.Subcommand) {
		return defaults
	}
	if opts.LookupEnv == nil {
		return defaults
	}
	if platform, ok := opts.LookupEnv("DOCKER_DEFAULT_PLATFORM"); ok && platform != "" {
		defaults = append(defaults, "--platform", platform)
	}
	return defaults
}

// acceptsPlatform reports whether the sub-command accepts --platform.
func acceptsPlatform(subcommand string) bool {
	switch subcommand {
	case "run", "create", "build":
		return true
	default:
		return false
	}
}

func renderOccurrence(occ occurrence, opts Options) ([]string, []string, error) {
	entry := occ.entry
	name := entry.Display()
	value := occ.value

	if entry.Category == CatRewrite {
		if entry.RuntimeName != "" {
			name = "--" + entry.RuntimeName
		}
		if entry.RuntimeValue != "" {
			return []string{fmt.Sprintf("%s=%s", name, entry.RuntimeValue)}, nil, nil
		}
	}

	var warns []string
	if entry.Category == CatSpecial {
		var (
			drop bool
			err  error
		)
		value, drop, warns, err = applySpecial(entry.Special, value, opts)
		if err != nil {
			usage := &UsageError{}
			if errors.As(err, &usage) {
				usage.Subcommand = opts.Subcommand
			}
			return nil, warns, err
		}
		if drop {
			// Accepted but resolves to nothing, for example an unset variable
			// named by -e. Forwarding the flag with an empty value would
			// silently set it to the empty string, which is not what docker does.
			return nil, warns, nil
		}
	}

	if entry.Kind == KindBool {
		if occ.boolValue != "" {
			return []string{fmt.Sprintf("%s=%s", name, occ.boolValue)}, warns, nil
		}
		return []string{name}, warns, nil
	}
	return []string{name, value}, warns, nil
}

// applySpecial performs the value surgery for one Special entry. drop reports
// that the flag was accepted but resolves to nothing and must not be forwarded.
func applySpecial(s Special, value string, opts Options) (string, bool, []string, error) {
	switch s {
	case SpecialPath:
		mapped, err := mapPath(value, opts)
		return mapped, false, nil, err

	case SpecialVolume:
		mapped, err := mapVolumeSource(value, opts)
		return mapped, false, nil, err

	case SpecialMount:
		mapped, warns, err := transformMount(value, opts)
		return mapped, false, warns, err

	case SpecialEnv:
		if strings.Contains(value, "=") {
			return value, false, nil, nil
		}
		// Docker resolves a bare NAME from the host environment and omits it
		// when unset. Resolve it on the host so the value does not depend on the
		// environment inside the VM.
		if opts.LookupEnv != nil {
			if v, ok := opts.LookupEnv(value); ok {
				return value + "=" + v, false, nil, nil
			}
		}
		return "", true, []string{fmt.Sprintf(
			"not passing environment variable %q: it is not set in the host environment", value)}, nil

	case SpecialEnvFile:
		mapped, err := mapPath(value, opts)
		return mapped, false, nil, err

	case SpecialHostGateway:
		host, ip, ok := strings.Cut(value, ":")
		if !ok {
			return "", false, nil, &UsageError{
				Flag:   "--add-host",
				Reason: `the value must be HOST:IP, for example "name:192.0.2.1" or "name:host-gateway"`,
			}
		}
		if ip != "host-gateway" || opts.HostGateway == nil {
			return value, false, nil, nil
		}
		resolved, err := opts.HostGateway.ResolveHostGateway(host)
		if err != nil {
			return "", false, nil, fmt.Errorf("failed to resolve host-gateway for %q: %w", host, err)
		}
		return host + ":" + resolved, false, nil, nil

	case SpecialCPUQuota:
		// Docker treats an explicit 0 as "unset"; the runtime treats -1 as
		// unset and 0 as a zero quota.
		if value == "0" {
			return "-1", false, nil, nil
		}
		return value, false, nil, nil

	case SpecialOutput:
		mapped, err := transformKeyValueList(value, opts, "dest")
		return mapped, false, nil, err

	case SpecialSecret:
		mapped, err := transformKeyValueList(value, opts, "src")
		return mapped, false, nil, err

	case SpecialCache:
		mapped, err := transformKeyValueList(value, opts, "src", "dest")
		return mapped, false, nil, err

	case SpecialBuildContext:
		name, path, ok := strings.Cut(value, "=")
		if !ok {
			return value, false, nil, nil
		}
		mapped, err := mapPath(path, opts)
		return name + "=" + mapped, false, nil, err
	}
	return value, false, nil, nil
}

func mapPath(value string, opts Options) (string, error) {
	if opts.PathMapper == nil || value == "" {
		return value, nil
	}
	return opts.PathMapper.MapHostPath(value)
}

// transformMount handles --mount while preserving the order of the key=value
// pairs. Docker accepts an unordered list, but a stable order keeps the
// translated command line comparable.
func transformMount(value string, opts Options) (string, []string, error) {
	parts := strings.Split(value, ",")
	var (
		kv      [][2]string
		bare    []string
		dropped []string
	)
	for _, part := range parts {
		key, val, ok := strings.Cut(part, "=")
		if !ok {
			bare = append(bare, part)
			continue
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		if key == "consistency" {
			dropped = append(dropped, part)
			continue
		}
		kv = append(kv, [2]string{key, val})
	}

	typeValue := ""
	for _, pair := range kv {
		if pair[0] == "type" {
			typeValue = pair[1]
			break
		}
	}

	var warns []string
	if len(dropped) > 0 {
		warns = append(warns, fmt.Sprintf(
			"--mount: dropping %s, which the runtime does not support",
			strings.Join(dropped, ",")))
	}

	if typeValue == "bind" {
		for i, pair := range kv {
			if pair[0] != "src" && pair[0] != "source" {
				continue
			}
			mapped, err := mapPath(pair[1], opts)
			if err != nil {
				return "", warns, err
			}
			kv[i][1] = mapped
		}
	}

	out := make([]string, 0, len(kv)+len(bare))
	for _, pair := range kv {
		out = append(out, pair[0]+"="+pair[1])
	}
	out = append(out, bare...)
	return strings.Join(out, ","), warns, nil
}

// mapVolumeSource rewrites the host path of a SRC:DST[:MODE] volume
// specification. Named and anonymous volumes have no host path and are left
// alone.
func mapVolumeSource(value string, opts Options) (string, error) {
	src, rest, ok := splitVolumeSource(value)
	if !ok || !looksLikeHostPath(src) {
		return value, nil
	}
	mapped, err := mapPath(src, opts)
	if err != nil {
		return "", fmt.Errorf("failed to map the source of volume %q: %w", value, err)
	}
	return mapped + rest, nil
}

// splitVolumeSource splits "SRC:DST[:MODE]" into the source and the remainder
// including the leading colon. A Windows drive prefix (C:\ or C:/) is not
// mistaken for the source/destination separator.
func splitVolumeSource(value string) (string, string, bool) {
	start := 0
	if len(value) >= 2 && isDriveLetter(value[0]) && value[1] == ':' &&
		len(value) >= 3 && (value[2] == '\\' || value[2] == '/') {
		start = 2
	}
	idx := strings.Index(value[start:], ":")
	if idx < 0 {
		return "", "", false
	}
	idx += start
	return value[:idx], value[idx:], true
}

func isDriveLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// looksLikeHostPath reports whether a volume source is a host path rather than
// a named volume. Docker treats a source containing a path separator, or one
// that starts with '.', as a path.
func looksLikeHostPath(src string) bool {
	switch {
	case src == "":
		return false
	case src == "." || src == "..":
		return true
	case strings.HasPrefix(src, "./") || strings.HasPrefix(src, "../"):
		return true
	case strings.HasPrefix(src, "~"):
		return true
	case strings.ContainsAny(src, "/\\"):
		return true
	}
	return false
}

// transformKeyValueList maps the named keys of a comma separated key=value
// list, keeping the original order.
func transformKeyValueList(value string, opts Options, keys ...string) (string, error) {
	want := make(map[string]bool, len(keys))
	for _, k := range keys {
		want[k] = true
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		key, val, ok := strings.Cut(part, "=")
		if !ok || !want[strings.TrimSpace(key)] {
			out = append(out, part)
			continue
		}
		mapped, err := mapPath(strings.TrimSpace(val), opts)
		if err != nil {
			return "", err
		}
		out = append(out, key+"="+mapped)
	}
	return strings.Join(out, ","), nil
}
