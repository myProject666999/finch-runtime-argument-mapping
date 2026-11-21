// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"strings"

	"github.com/runfinch/finch/pkg/argtranslate"
	"github.com/runfinch/finch/pkg/command"
	"github.com/runfinch/finch/pkg/config"
	"github.com/runfinch/finch/pkg/flog"
)

// translatedSubcommands maps the finch command names that have a translation
// matrix to the canonical sub-command name the matrix is keyed by. Both the
// bare docker name and the management form resolve to the same key, so the
// tables are shared rather than duplicated.
var translatedSubcommands = map[string]string{
	"run":           "run",
	"container run": "run",
	"create":        "create",
	"exec":          "exec",
	"build":         "build",
	"image build":   "build",
}

// dockerCompat is true when the user asked finch to accept docker-style
// argument semantics.
func dockerCompat(fc *config.Finch) bool { return fc != nil && fc.DockerCompat }

// translationHandles reports whether the translation matrix owns this command's
// arguments, so its legacy command handler must not run.
func translationHandles(cmdName string) bool {
	if _, ok := translatedSubcommands[cmdName]; ok {
		return true
	}
	// The management form is resolved from the sub-command word, which is only
	// known inside translateArgs; the concrete pairs are listed here so the
	// legacy handlers can be skipped before that word is examined.
	switch cmdName {
	case "container", "image":
		return true
	default:
		return false
	}
}

// translateArgs runs the docker-style argument translator for the commands that
// have a matrix. It reports whether the caller must skip its legacy argument
// handlers, so a path is never converted twice.
//
// The OS-specific path mapper and host-gateway resolver are injected here, so
// the translator itself never performs OS calls.
func (nc *nerdctlCommand) translateArgs(cmdName string, args []string) ([]string, bool, error) {
	if !dockerCompat(nc.fc) {
		return args, false, nil
	}

	sub, ok := translatedSubcommands[cmdName]
	prefix := 0
	if !ok && len(args) > 0 {
		// The management form, e.g. `finch container run …` or
		// `finch image build …`. The sub-command word stays in front of the
		// translated arguments.
		sub, ok = translatedSubcommands[cmdName+" "+args[0]]
		prefix = 1
	}
	if !ok {
		return args, false, nil
	}

	res := argtranslate.Translate(args[prefix:], argtranslate.Options{
		Subcommand:  sub,
		Lenient:     nc.fc.DockerCompatLenient,
		PathMapper:  hostPathMapper{systemDeps: nc.systemDeps},
		HostGateway: hostGatewayResolver{systemDeps: nc.systemDeps, logger: nc.logger, ecc: nc.ecc},
		LookupEnv:   nc.systemDeps.LookupEnv,
	})

	for _, warning := range res.Warnings {
		nc.logger.Warnf("%s", warning)
	}
	if len(res.FinchFlags) > 0 {
		// The only finch-level flag in the matrix is --debug.
		nc.logger.SetLevel(flog.Debug)
	}
	if len(res.Errors) > 0 {
		return args, true, joinTranslateErrors(res.Errors)
	}
	out := res.Args
	if len(out) == 0 && (sub == "run" || sub == "exec") {
		// Preserve the legacy behaviour of showing help for a bare
		// `finch run` or `finch exec`, which the runtime would otherwise
		// reject as a missing image or container.
		out = []string{"--help"}
	}
	return append(args[:prefix:prefix], out...), true, nil
}

// joinTranslateErrors renders every problem on its own line, so a command line
// with both a usage error and unsupported flags shows all of them at once.
func joinTranslateErrors(errs []error) error {
	if len(errs) == 1 {
		return errs[0]
	}
	var b strings.Builder
	for i, err := range errs {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(err.Error())
	}
	return fmt.Errorf("%s", b.String())
}

// hostPathMapper adapts the OS-specific path conversion into the translator's
// PathMapper. On macOS the conversion is the identity because the host home
// directory is visible in the VM at the same path; on Windows it maps a drive
// letter to /mnt/<drive>.
type hostPathMapper struct {
	systemDeps NerdctlCommandSystemDeps
}

// MapHostPath converts one host path. The per-OS file provides the
// implementation: the identity on macOS, a drive-letter mapping on Windows.
func (m hostPathMapper) MapHostPath(path string) (string, error) {
	return convertArgPath(m.systemDeps, path)
}

// hostGatewayResolver adapts the OS-specific "host-gateway" resolution into the
// translator's HostGatewayResolver.
type hostGatewayResolver struct {
	systemDeps NerdctlCommandSystemDeps
	logger     flog.Logger
	ecc        command.Creator
}

// ResolveHostGateway resolves the container-visible address of the host via the
// per-OS implementation.
func (r hostGatewayResolver) ResolveHostGateway(host string) (string, error) {
	return resolveHostGatewayAddress(r.systemDeps, host, r.logger, r.ecc)
}
