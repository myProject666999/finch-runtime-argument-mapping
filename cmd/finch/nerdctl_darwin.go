// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package main

import (
	"fmt"
	"strings"

	dockerops "github.com/docker/docker/opts"
	"github.com/lima-vm/lima/pkg/networks"

	"github.com/runfinch/finch/pkg/command"
	"github.com/runfinch/finch/pkg/flog"
)

func convertToWSLPath(_ NerdctlCommandSystemDeps, _ string) (string, error) {
	return "", nil
}

// convertArgPath is the translator's host path mapping. macOS mounts the host
// home directory into the VM at the same path, so no conversion is needed.
func convertArgPath(_ NerdctlCommandSystemDeps, path string) (string, error) {
	return path, nil
}

// resolveHostGatewayAddress resolves docker's "host-gateway" pseudo address to
// an address reachable from containers.
func resolveHostGatewayAddress(_ NerdctlCommandSystemDeps, host string, logger flog.Logger, ecc command.Creator) (string, error) {
	return resolveIP(host+":host-gateway", logger, ecc)
}

var osAliasMap = map[string]string{}

var osArgHandlerMap = map[string]map[string]argHandler{}

var osCommandHandlerMap = map[string]commandHandler{}

func (nc *nerdctlCommand) GetCmdArgs() []string {
	return []string{"shell", limaInstanceName, "sudo", "-E"}
}

func resolveIP(host string, logger flog.Logger, _ command.Creator) (string, error) {
	parts := strings.SplitN(host, ":", 2)
	// If the IP Address is a string called "host-gateway", replace this value with the IP address that can be used to
	// access host from the containers.
	// TODO: make the host gateway ip configurable.
	var resolvedIP string
	if parts[1] == dockerops.HostGatewayName {
		resolvedIP = networks.SlirpGateway

		logger.Debugf(`Resolving special IP "host-gateway" to %q for host %q`, resolvedIP, parts[0])
		return fmt.Sprintf("%s:%s", parts[0], resolvedIP), nil
	}
	return host, nil
}

func handleBindMountPath(_ NerdctlCommandSystemDeps, _ map[string]string) error {
	// Do nothing by default
	return nil
}

func mapToString(m map[string]string) string {
	var parts []string
	for k, v := range m {
		part := fmt.Sprintf("%s=%s", k, v)
		parts = append(parts, part)
	}
	return strings.Join(parts, ",")
}
