// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

// Package argtranslate translates docker-style command line arguments into the
// argument list understood by the container runtime Finch drives (nerdctl).
//
// Translate is a pure function: it never executes a command and never reads a
// file. Everything OS-specific, such as converting a host path to the path the
// runtime sees inside the virtual machine, is injected through Options. That
// keeps the translation testable from any working directory and on any
// platform, and it keeps the boundary between translation and path conversion
// in one place.
//
// The matrix in matrix.go is the single source of truth for argument
// semantics. It is rendered into docs/arg-mapping.md by RenderMarkdown so the
// documented behaviour and the implemented behaviour cannot drift apart.
package argtranslate

//go:generate go run ./gen
