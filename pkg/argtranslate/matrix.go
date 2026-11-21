// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package argtranslate

import "fmt"

// Kind describes how a flag is written on the command line and how repeated
// occurrences are interpreted.
type Kind int

const (
	// KindBool is a flag that takes no value, optionally written as --flag=false.
	KindBool Kind = iota
	// KindValue is a value flag where the last occurrence wins, matching pflag.
	KindValue
	// KindValueRepeat is a value flag where every occurrence is forwarded in
	// order; the runtime applies last-wins precedence where it matters.
	KindValueRepeat
)

// String returns the documented spelling of the kind.
func (k Kind) String() string {
	switch k {
	case KindBool:
		return "bool"
	case KindValue:
		return "value"
	case KindValueRepeat:
		return "value, repeatable"
	default:
		return fmt.Sprintf("Kind(%d)", int(k))
	}
}

// Category is the translation class of a flag. The names are used verbatim in
// docs/arg-mapping.md.
type Category int

const (
	// CatPass means the flag is forwarded unchanged.
	CatPass Category = iota
	// CatRewrite means the flag is renamed or its value replaced before forwarding.
	CatRewrite
	// CatSpecial means the value needs sub-command aware surgery before forwarding.
	CatSpecial
	// CatIgnored means the flag is accepted but not forwarded, with a warning.
	CatIgnored
	// CatUnsupported means the flag is rejected (or, in lenient mode, dropped).
	CatUnsupported
)

// String returns the documented name of the category.
func (c Category) String() string {
	switch c {
	case CatPass:
		return "pass"
	case CatRewrite:
		return "rewrite"
	case CatSpecial:
		return "special"
	case CatIgnored:
		return "ignored"
	case CatUnsupported:
		return "unsupported"
	default:
		return fmt.Sprintf("Category(%d)", int(c))
	}
}

// Special identifies the value surgery a Special entry needs.
type Special string

const (
	// SpecialPath maps the whole value as a host path.
	SpecialPath Special = "path"
	// SpecialVolume maps the source of a SRC:DST[:MODE] volume specification.
	SpecialVolume Special = "volume"
	// SpecialMount maps bind sources inside a --mount key=value list and drops
	// the docker-only consistency key.
	SpecialMount Special = "mount"
	// SpecialEnv resolves the host value of a bare NAME environment variable.
	SpecialEnv Special = "env"
	// SpecialEnvFile maps the file path of an environment file.
	SpecialEnvFile Special = "env-file"
	// SpecialHostGateway resolves the docker-specific host-gateway pseudo IP.
	SpecialHostGateway Special = "host-gateway"
	// SpecialCPUQuota translates docker's "0 means unset" to the runtime's -1.
	SpecialCPUQuota Special = "cpu-quota"
	// SpecialOutput maps the destination of a build output specification.
	SpecialOutput Special = "output"
	// SpecialSecret maps the src key of a build secret.
	SpecialSecret Special = "secret"
	// SpecialCache maps the local src/dest keys of a build cache specification.
	SpecialCache Special = "cache"
	// SpecialBuildContext maps the path after the name= prefix of a build context.
	SpecialBuildContext Special = "build-context"
)

// String returns the special handler id.
func (s Special) String() string { return string(s) }

// Entry is one row of a sub-command matrix.
type Entry struct {
	// Name is the docker-style long flag name without the leading dashes.
	Name string
	// Short is the single-letter alias, if any.
	Short string
	// Kind describes whether a value is taken and how repeats behave.
	Kind Kind
	// Category is the translation class.
	Category Category
	// RuntimeName is the forwarded name for Rewrite entries (defaults to Name).
	RuntimeName string
	// RuntimeValue is a fixed replacement value for Rewrite entries.
	RuntimeValue string
	// Special identifies the value surgery for Special entries.
	Special Special
	// Syntax documents the expected value shape.
	Syntax string
	// Rationale is mandatory for Ignored entries and explains why dropping the
	// flag is harmless.
	Rationale string
	// Note is free-form context shown in the generated table.
	Note string
	// FinchOnly marks flags consumed by finch itself (such as --debug).
	FinchOnly bool
}

// Display returns the flag name including the leading dashes.
func (e Entry) Display() string { return "--" + e.Name }

// Matrix is the ordered translation table for one canonical sub-command.
type Matrix struct {
	// Subcommand is the canonical sub-command name used in finch, e.g. "run".
	Subcommand string
	// Entries are the rows of the table, ordered by flag name.
	Entries []Entry
}

// Subcommands lists the canonical sub-command names covered by the matrix, in
// documentation order.
func Subcommands() []string { return []string{"run", "create", "exec", "build"} }

// Lookup returns the matrix for a canonical sub-command name.
func Lookup(subcommand string) (Matrix, bool) {
	for _, m := range Matrices() {
		if m.Subcommand == subcommand {
			return m, true
		}
	}
	return Matrix{}, false
}

// Matrices returns all matrices. run and create share one definition; create
// only removes the flags the runtime does not accept for it.
func Matrices() []Matrix {
	return []Matrix{
		{Subcommand: "run", Entries: runEntries()},
		{Subcommand: "create", Entries: createEntries()},
		{Subcommand: "exec", Entries: execEntries()},
		{Subcommand: "build", Entries: buildEntries()},
	}
}

// Long returns the entry for a long flag name.
func (m Matrix) Long(name string) (Entry, bool) {
	for _, e := range m.Entries {
		if e.Name == name {
			return e, true
		}
	}
	return Entry{}, false
}

// Short returns the entry for a single-letter alias.
func (m Matrix) Short(short string) (Entry, bool) {
	for _, e := range m.Entries {
		if e.Short == short && e.Short != "" {
			return e, true
		}
	}
	return Entry{}, false
}

func pass(name, short string, kind Kind) Entry {
	return Entry{Name: name, Short: short, Kind: kind, Category: CatPass}
}

// extension marks a flag the runtime accepts but docker does not define.
func extension(name string, kind Kind) Entry {
	return Entry{
		Name:     name,
		Kind:     kind,
		Category: CatPass,
		Note:     "runtime extension; docker does not define it, forwarded unchanged",
	}
}

func special(name, short string, kind Kind, s Special, syntax string) Entry {
	return Entry{Name: name, Short: short, Kind: kind, Category: CatSpecial, Special: s, Syntax: syntax}
}

func ignored(name, short string, kind Kind, rationale string) Entry {
	return Entry{Name: name, Short: short, Kind: kind, Category: CatIgnored, Rationale: rationale}
}

func unsupported(name, short string, kind Kind, syntax, note string) Entry {
	return Entry{Name: name, Short: short, Kind: kind, Category: CatUnsupported, Syntax: syntax, Note: note}
}

func rewrite(name, short string, kind Kind, runtimeName, runtimeValue string) Entry {
	return Entry{
		Name:         name,
		Short:        short,
		Kind:         kind,
		Category:     CatRewrite,
		RuntimeName:  runtimeName,
		RuntimeValue: runtimeValue,
	}
}

// baseContainerEntries is shared by run and create. Docker's `run` and
// `create` accept the same container options; only the run-specific flags at
// the end differ, and createEntries removes the ones the runtime rejects.
func baseContainerEntries() []Entry {
	return []Entry{
		special("add-host", "", KindValueRepeat, SpecialHostGateway, "HOST:IP"),
		pass("annotation", "", KindValueRepeat),
		pass("attach", "a", KindValueRepeat),
		pass("blkio-weight", "", KindValue),
		pass("blkio-weight-device", "", KindValueRepeat),
		pass("cap-add", "", KindValueRepeat),
		pass("cap-drop", "", KindValueRepeat),
		extension("cgroup-conf", KindValueRepeat),
		pass("cgroup-parent", "", KindValue),
		pass("cgroupns", "", KindValue),
		special("cidfile", "", KindValue, SpecialPath, "PATH"),
		extension("cosign-certificate-identity", KindValue),
		extension("cosign-certificate-identity-regexp", KindValue),
		extension("cosign-certificate-oidc-issuer", KindValue),
		extension("cosign-certificate-oidc-issuer-regexp", KindValue),
		special("cosign-key", "", KindValue, SpecialPath, "PATH"),
		pass("cpu-period", "", KindValue),
		special("cpu-quota", "", KindValue, SpecialCPUQuota, "INT"),
		pass("cpu-rt-period", "", KindValue),
		pass("cpu-rt-runtime", "", KindValue),
		pass("cpu-shares", "c", KindValue),
		pass("cpus", "", KindValue),
		pass("cpuset-cpus", "", KindValue),
		pass("cpuset-mems", "", KindValue),
		unsupported("cpu-count", "", KindValue, "INT", "windows-only in docker; the runtime has no equivalent"),
		unsupported("cpu-percent", "", KindValue, "INT", "windows-only in docker; the runtime has no equivalent"),
		unsupported("io-maxbandwidth", "", KindValue, "BYTES", "windows-only in docker; the runtime has no equivalent"),
		unsupported("io-maxiops", "", KindValue, "IOPS", "windows-only in docker; the runtime has no equivalent"),
		pass("detach", "d", KindBool),
		pass("detach-keys", "", KindValue),
		pass("device", "", KindValueRepeat),
		unsupported("device-cgroup-rule", "", KindValueRepeat, "RULE",
			"accepted by docker only; the runtime has no equivalent"),
		pass("device-read-bps", "", KindValueRepeat),
		pass("device-read-iops", "", KindValueRepeat),
		pass("device-write-bps", "", KindValueRepeat),
		pass("device-write-iops", "", KindValueRepeat),
		ignored("disable-content-trust", "", KindBool,
			"docker deprecated the flag when it removed Docker Content Trust, so it is always true there too"),
		pass("dns", "", KindValueRepeat),
		pass("dns-opt", "", KindValueRepeat),
		pass("dns-option", "", KindValueRepeat),
		pass("dns-search", "", KindValueRepeat),
		pass("domainname", "", KindValue),
		pass("entrypoint", "", KindValueRepeat),
		special("env", "e", KindValueRepeat, SpecialEnv, "NAME[=VALUE]"),
		special("env-file", "", KindValueRepeat, SpecialEnvFile, "PATH"),
		unsupported("expose", "", KindValueRepeat, "PORT[/PROTO]", "the runtime has no equivalent; publish the port instead"),
		pass("gpus", "", KindValueRepeat),
		pass("group-add", "", KindValueRepeat),
		pass("health-cmd", "", KindValue),
		pass("health-interval", "", KindValue),
		pass("health-retries", "", KindValue),
		unsupported("health-start-interval", "", KindValue, "DURATION",
			"docker 1.44 flag; the runtime has no equivalent"),
		pass("health-start-period", "", KindValue),
		pass("health-timeout", "", KindValue),
		pass("help", "", KindBool),
		pass("hostname", "h", KindValue),
		pass("init", "", KindBool),
		extension("init-binary", KindValue),
		pass("interactive", "i", KindBool),
		pass("ip", "", KindValue),
		pass("ip6", "", KindValue),
		pass("ipc", "", KindValue),
		extension("ipfs-address", KindValue),
		pass("isolation", "", KindValue),
		ignored("kernel-memory", "", KindValue,
			"the kernel no longer supports it and the runtime documents the flag as a no-op"),
		pass("label", "l", KindValueRepeat),
		special("label-file", "", KindValueRepeat, SpecialPath, "PATH"),
		pass("log-driver", "", KindValue),
		pass("log-opt", "", KindValueRepeat),
		pass("mac-address", "", KindValue),
		pass("memory", "m", KindValue),
		pass("memory-reservation", "", KindValue),
		pass("memory-swap", "", KindValue),
		pass("memory-swappiness", "", KindValue),
		special("mount", "", KindValueRepeat, SpecialMount, "KEY=VALUE,..."),
		pass("name", "", KindValue),
		rewrite("net", "", KindValueRepeat, "network", ""),
		pass("network", "", KindValueRepeat),
		unsupported("link", "", KindValueRepeat, "CONTAINER[:ALIAS]", "docker's legacy container linking; the runtime has no equivalent"),
		unsupported("link-local-ip", "", KindValueRepeat, "CIDR", "the runtime has no equivalent"),
		unsupported("net-alias", "", KindValueRepeat, "ALIAS",
			"docker alias for --network-alias; the runtime has no network alias support"),
		unsupported("network-alias", "", KindValueRepeat, "ALIAS",
			"the runtime has no network alias support"),
		pass("no-healthcheck", "", KindBool),
		pass("oom-kill-disable", "", KindBool),
		pass("oom-score-adj", "", KindValue),
		pass("pid", "", KindValue),
		extension("pidfile", KindValue),
		pass("pids-limit", "", KindValue),
		pass("platform", "", KindValue),
		pass("privileged", "", KindBool),
		pass("publish", "p", KindValueRepeat),
		unsupported("publish-all", "P", KindBool, "", "the runtime has no equivalent; publish each port explicitly"),
		pass("pull", "", KindValue),
		pass("quiet", "q", KindBool),
		extension("rdt-class", KindValue),
		pass("read-only", "", KindBool),
		pass("restart", "", KindValue),
		pass("rm", "", KindBool),
		extension("rootfs", KindBool),
		pass("runtime", "", KindValue),
		pass("security-opt", "", KindValueRepeat),
		pass("shm-size", "", KindValue),
		pass("sig-proxy", "", KindBool),
		pass("stop-signal", "", KindValue),
		pass("stop-timeout", "", KindValue),
		unsupported("storage-opt", "", KindValueRepeat, "KEY=VALUE", "the runtime has no storage driver options"),
		pass("sysctl", "", KindValueRepeat),
		extension("systemd", KindValue),
		pass("tmpfs", "", KindValueRepeat),
		pass("tty", "t", KindBool),
		pass("ulimit", "", KindValueRepeat),
		pass("umask", "", KindValue),
		pass("user", "u", KindValue),
		pass("userns", "", KindValue),
		unsupported("use-api-socket", "", KindBool, "", "docker-specific API socket mount; the runtime has no equivalent"),
		pass("uts", "", KindValue),
		extension("verify", KindValue),
		special("volume", "v", KindValueRepeat, SpecialVolume, "SRC:DST[:MODE]"),
		pass("volumes-from", "", KindValueRepeat),
		unsupported("volume-driver", "", KindValue, "DRIVER", "the runtime has no volume drivers"),
		pass("workdir", "w", KindValue),
		finchDebug(),
	}
}

func runEntries() []Entry {
	return sorted(baseContainerEntries())
}

func createEntries() []Entry {
	// --detach is run-only. --attach is a docker create flag the runtime's
	// create does not accept, so it is reported as unsupported rather than
	// removed. Everything else in the shared container table applies to both.
	entries := omit(baseContainerEntries(), "detach")
	for i := range entries {
		if entries[i].Name == "attach" {
			entries[i] = unsupported("attach", "a", KindValueRepeat, "STDIN|STDOUT|STDERR",
				"docker create accepts it; the runtime's create does not")
		}
	}
	return sorted(entries)
}

func execEntries() []Entry {
	return sorted([]Entry{
		pass("detach", "d", KindBool),
		unsupported("detach-keys", "", KindValue, "KEYS", "docker exec accepts it; the runtime's exec does not"),
		special("env", "e", KindValueRepeat, SpecialEnv, "NAME[=VALUE]"),
		special("env-file", "", KindValueRepeat, SpecialEnvFile, "PATH"),
		pass("help", "", KindBool),
		pass("interactive", "i", KindBool),
		pass("privileged", "", KindBool),
		pass("tty", "t", KindBool),
		pass("user", "u", KindValue),
		pass("workdir", "w", KindValue),
		finchDebug(),
	})
}

func buildEntries() []Entry {
	return sorted([]Entry{
		special("add-host", "", KindValueRepeat, SpecialHostGateway, "HOST:IP"),
		extension("allow", KindValueRepeat),
		extension("attest", KindValueRepeat),
		pass("build-arg", "", KindValueRepeat),
		special("build-context", "", KindValueRepeat, SpecialBuildContext, "NAME=PATH"),
		extension("buildkit-host", KindValue),
		special("cache-from", "", KindValueRepeat, SpecialCache, "type=local,src=PATH"),
		special("cache-to", "", KindValueRepeat, SpecialCache, "type=local,dest=PATH"),
		unsupported("cgroup-parent", "", KindValue, "PATH", "docker legacy builder option; the runtime's build has no equivalent"),
		unsupported("compress", "", KindBool, "", "docker legacy builder option; the runtime's build has no equivalent"),
		unsupported("cpu-period", "", KindValue, "INT", "docker legacy builder option; the runtime's build has no equivalent"),
		unsupported("cpu-quota", "", KindValue, "INT", "docker legacy builder option; the runtime's build has no equivalent"),
		unsupported("cpu-shares", "c", KindValue, "INT", "docker legacy builder option; the runtime's build has no equivalent"),
		unsupported("cpuset-cpus", "", KindValue, "LIST", "docker legacy builder option; the runtime's build has no equivalent"),
		unsupported("cpuset-mems", "", KindValue, "LIST", "docker legacy builder option; the runtime's build has no equivalent"),
		ignored("disable-content-trust", "", KindBool,
			"docker deprecated the flag when it removed Docker Content Trust, so it is always true there too"),
		special("file", "f", KindValue, SpecialPath, "PATH"),
		unsupported("force-rm", "", KindBool, "", "docker legacy builder option; deletion is the runtime's default"),
		pass("help", "h", KindBool),
		special("iidfile", "", KindValue, SpecialPath, "PATH"),
		unsupported("isolation", "", KindValue, "MODE", "docker builder option; the runtime's build has no equivalent"),
		pass("label", "", KindValueRepeat),
		rewrite("load", "", KindBool, "output", "type=docker"),
		unsupported("memory", "m", KindValue, "BYTES", "docker legacy builder option; the runtime's build has no equivalent"),
		unsupported("memory-swap", "", KindValue, "BYTES", "docker legacy builder option; the runtime's build has no equivalent"),
		pass("network", "", KindValue),
		pass("no-cache", "", KindBool),
		special("output", "o", KindValue, SpecialOutput, "type=local,dest=PATH"),
		pass("platform", "", KindValueRepeat),
		pass("progress", "", KindValue),
		extension("provenance", KindValue),
		pass("pull", "", KindBool),
		unsupported("push", "", KindBool, "", "docker buildx only; use --output type=registry instead"),
		pass("quiet", "q", KindBool),
		pass("rm", "", KindBool),
		extension("sbom", KindValue),
		special("secret", "", KindValueRepeat, SpecialSecret, "id=ID,src=PATH"),
		unsupported("security-opt", "", KindValueRepeat, "OPT", "docker legacy builder option; the runtime's build has no equivalent"),
		unsupported("shm-size", "", KindValue, "BYTES", "docker legacy builder option; the runtime's build has no equivalent"),
		extension("source-policy-file", KindValue),
		unsupported("squash", "", KindBool, "", "docker legacy builder option; the runtime's build has no equivalent"),
		extension("ssh", KindValueRepeat),
		pass("tag", "t", KindValueRepeat),
		pass("target", "", KindValue),
		unsupported("ulimit", "", KindValueRepeat, "TYPE=SOFT[:HARD]", "docker legacy builder option; the runtime's build has no equivalent"),
		finchDebug(),
	})
}

func sorted(entries []Entry) []Entry {
	// Insertion sort keeps the output stable without importing sort for a
	// couple of dozen rows, and makes the ordering rule obvious.
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entries[j].Name < entries[j-1].Name; j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}
	return entries
}

func omit(entries []Entry, names ...string) []Entry {
	drop := make(map[string]bool, len(names))
	for _, n := range names {
		drop[n] = true
	}
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if drop[e.Name] {
			continue
		}
		out = append(out, e)
	}
	return out
}

// finchDebug is a finch-level flag that never reaches the runtime.
func finchDebug() Entry {
	return Entry{
		Name: "debug", Kind: KindBool, Category: CatIgnored, FinchOnly: true,
		Rationale: "finch consumes it to raise its own log level; the runtime never sees it",
	}
}
