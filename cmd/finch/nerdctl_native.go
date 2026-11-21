// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package main

import (
	"fmt"
	"strings"

	"golang.org/x/exp/slices"

	"github.com/runfinch/finch/pkg/command"
	"github.com/runfinch/finch/pkg/flog"
)

func (nc *nerdctlCommand) run(cmdName string, args []string) error {
	var (
		hasCmdHandler, hasArgHandler bool
		cmdHandler                   commandHandler
		aMap                         map[string]argHandler
		err                          error
		inspectType                  string
	)

	// eat the debug arg, and set the log level to avoid nerdctl parsing this flag
	dbgIdx := slices.Index(args, "--debug")
	if dbgIdx >= 0 {
		args = append(args[:dbgIdx], args[dbgIdx+1:]...)
		nc.logger.SetLevel(flog.Debug)
	}

	alias, hasAlias := aliasMap[cmdName]
	if hasAlias {
		cmdHandler, hasCmdHandler = commandHandlerMap[alias]
		aMap, hasArgHandler = argHandlerMap[alias]
	} else {
		cmdHandler, hasCmdHandler = commandHandlerMap[cmdName]
		aMap, hasArgHandler = argHandlerMap[cmdName]

		if !hasArgHandler && len(args) > 0 {
			// for commands like image build, container run
			key := fmt.Sprintf("%s %s", cmdName, args[0])
			cmdHandler, hasCmdHandler = commandHandlerMap[key]
			aMap, hasArgHandler = argHandlerMap[key]
		}
	}

	// A command with a translation matrix owns all of its arguments when
	// dockercompat is enabled, so its legacy command handler is not run: it
	// would convert paths a second time.
	skipLegacyForTranslation := dockerCompat(nc.fc) && translationHandles(cmdName)

	// First check if the command has a command handler
	if hasCmdHandler && !skipLegacyForTranslation {
		err := cmdHandler(nc.systemDeps, nc.fc, &cmdName, &args, &inspectType)
		if err != nil {
			return err
		}
	}

	// With dockercompat enabled, the commands covered by the translation matrix
	// are translated here and the legacy in-place handlers are skipped, so a
	// path is never converted twice.
	translatedArgs, isTranslated, tErr := nc.translateArgs(cmdName, args)
	if tErr != nil {
		return tErr
	}
	if isTranslated {
		args = translatedArgs
	}

	if !isTranslated {
		for i, arg := range args {
			// Check if individual argument (and possibly following value) requires manipulation-in-place handling
			if hasArgHandler {
				// Check if argument for the command needs handling, sometimes it can be --file=<filename>
				b, _, _ := strings.Cut(arg, "=")
				h, ok := aMap[b]
				if ok {
					err = h(nc.systemDeps, nc.fc, args, i)
					if err != nil {
						return err
					}
				}
			}
		}
	}

	// Extra manipulation for cases that overwrite cmdName with alias
	splitName := strings.Split(cmdName, " ")
	cmdArgs := append([]string{splitName[0]}, splitName[1:]...)
	cmdArgs = append(cmdArgs, args...)

	if nc.shouldReplaceForHelp(splitName[0], args) {
		return nc.ncc.RunWithReplacingStdout(
			[]command.Replacement{{Source: "nerdctl", Target: "finch"}},
			cmdArgs...,
		)
	}

	if inspectType == "container" && nc.fc.DockerCompat && !slices.Contains(cmdArgs, "--format") {
		cmdArgs = append(cmdArgs, "--format", "{{json .}}")
		cmd := nc.ncc.Create(cmdArgs...)
		return inspectContainerOutputHandler(cmd)
	}

	if err := handleDockerCompatComposeVersion(cmdName, *nc, cmdArgs); err == nil {
		return nil
	}

	return nc.ncc.Create(cmdArgs...).Run()
}

// convertArgPath is the translator's host path mapping. On Linux there is no VM,
// so the host path is already the runtime path.
func convertArgPath(_ NerdctlCommandSystemDeps, path string) (string, error) {
	return path, nil
}

// resolveHostGatewayAddress resolves docker's "host-gateway" pseudo address.
// On Linux the host is the machine the runtime already runs on, so there is no
// translation to do and the value is passed through unchanged.
func resolveHostGatewayAddress(_ NerdctlCommandSystemDeps, _ string, _ flog.Logger, _ command.Creator) (string, error) {
	return "host-gateway", nil
}

var osAliasMap = map[string]string{}

var osArgHandlerMap = map[string]map[string]argHandler{}

var osCommandHandlerMap = map[string]commandHandler{}

func mapToString(m map[string]string) string {
	var parts []string
	for k, v := range m {
		part := fmt.Sprintf("%s=%s", k, v)
		parts = append(parts, part)
	}
	return strings.Join(parts, ",")
}

func handleBindMountPath(_ NerdctlCommandSystemDeps, _ map[string]string) error {
	// Do nothing by default
	return nil
}
