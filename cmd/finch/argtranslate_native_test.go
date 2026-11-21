// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package main

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/runfinch/finch/pkg/config"
	"github.com/runfinch/finch/pkg/mocks"
)

// TestNerdctlCommand_run_dockerCompat covers the integration between the
// translation matrix and the CLI: with dockercompat on, the matrix decides what
// reaches the runtime; with it off, the legacy path is unchanged.
func TestNerdctlCommand_run_dockerCompat(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		cmdName string
		fc      *config.Finch
		args    []string
		want    []string
		wantErr string
	}{
		{
			name:    "dockercompat off leaves arguments untouched",
			cmdName: "run",
			fc:      &config.Finch{},
			args:    []string{"-it", "--expose", "80", "alpine"},
			want:    []string{"run", "-it", "--expose", "80", "alpine"},
		},
		{
			name:    "dockercompat on translates supported flags",
			cmdName: "run",
			fc:      &config.Finch{SharedSettings: config.SharedSettings{DockerCompat: true}},
			args:    []string{"-it", "--rm", "-dp", "8080:80", "alpine", "sh"},
			want:    []string{"run", "--interactive", "--tty", "--rm", "--detach", "--publish", "8080:80", "alpine", "sh"},
		},
		{
			name:    "dockercompat on reports every unsupported flag at once",
			cmdName: "run",
			fc:      &config.Finch{SharedSettings: config.SharedSettings{DockerCompat: true}},
			args:    []string{"--expose", "80", "--publish-all", "alpine"},
			wantErr: "--expose, --publish-all",
		},
		{
			name:    "lenient mode drops unsupported flags instead",
			cmdName: "run",
			fc: &config.Finch{SharedSettings: config.SharedSettings{
				DockerCompat: true, DockerCompatLenient: true,
			}},
			args: []string{"--expose", "80", "alpine"},
			want: []string{"run", "alpine"},
		},
		{
			name:    "dockercompat on does not affect commands without a matrix",
			cmdName: "pull",
			fc:      &config.Finch{SharedSettings: config.SharedSettings{DockerCompat: true}},
			args:    []string{"--expose", "80", "alpine"},
			want:    []string{"pull", "--expose", "80", "alpine"},
		},
		{
			name:    "the management form is translated in place",
			cmdName: "container",
			fc:      &config.Finch{SharedSettings: config.SharedSettings{DockerCompat: true}},
			args:    []string{"run", "--kernel-memory", "1G", "-it", "alpine"},
			want:    []string{"container", "run", "--interactive", "--tty", "alpine"},
		},
		{
			name:    "a management sub-command without a matrix keeps the legacy path",
			cmdName: "container",
			fc:      &config.Finch{SharedSettings: config.SharedSettings{DockerCompat: true}},
			args:    []string{"logs", "-f", "web"},
			want:    []string{"container", "logs", "-f", "web"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			ncc := mocks.NewNerdctlCmdCreator(ctrl)
			ecc := mocks.NewCommandCreator(ctrl)
			ncsd := mocks.NewNerdctlCommandSystemDeps(ctrl)
			logger := mocks.NewLogger(ctrl)

			// The translator asks for the docker platform default; none is set here.
			ncsd.EXPECT().LookupEnv("DOCKER_DEFAULT_PLATFORM").Return("", false).AnyTimes()
			logger.EXPECT().Warnf(gomock.Any(), gomock.Any()).AnyTimes()

			var created bool
			c := mocks.NewCommand(ctrl)
			ncc.EXPECT().Create(gomock.Any()).DoAndReturn(func(args ...string) *mocks.Command {
				created = true
				assert.Equal(t, tc.want, args)
				return c
			}).AnyTimes()
			c.EXPECT().Run().Return(nil).AnyTimes()

			err := newNerdctlCommand(ncc, ecc, ncsd, logger, afero.NewMemMapFs(), tc.fc, nil).
				run(tc.cmdName, tc.args)

			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				assert.Contains(t, err.Error(), "finch run")
				assert.False(t, created, "nothing must reach the runtime when translation fails")
				return
			}
			require.NoError(t, err)
			assert.True(t, created, "expected a runtime command to be created")
		})
	}
}
