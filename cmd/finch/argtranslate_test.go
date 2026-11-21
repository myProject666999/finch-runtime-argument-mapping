// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/runfinch/finch/pkg/config"
	"github.com/runfinch/finch/pkg/flog"
	"github.com/runfinch/finch/pkg/mocks"
)

func TestTranslateArgs(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		cmdName   string
		fc        *config.Finch
		args      []string
		platform  string
		want      []string
		handled   bool
		wantErr   string
		wantDebug bool
	}{
		{
			name:      "dockercompat disabled is a no-op",
			cmdName:   "run",
			fc:        &config.Finch{},
			args:      []string{"--expose", "80"},
			want:      []string{"--expose", "80"},
			handled:   false,
			wantDebug: false,
		},
		{
			name:    "nil config is a no-op",
			cmdName: "run",
			fc:      nil,
			args:    []string{"-it"},
			want:    []string{"-it"},
			handled: false,
		},
		{
			name:    "subcommand without a matrix is a no-op",
			cmdName: "pull",
			fc:      &config.Finch{SharedSettings: config.SharedSettings{DockerCompat: true}},
			args:    []string{"--expose", "80"},
			want:    []string{"--expose", "80"},
			handled: false,
		},
		{
			name:    "run is translated through its canonical name",
			cmdName: "run",
			fc:      &config.Finch{SharedSettings: config.SharedSettings{DockerCompat: true}},
			args:    []string{"-it", "alpine"},
			want:    []string{"--interactive", "--tty", "alpine"},
			handled: true,
		},
		{
			name:    "container run and run share one table",
			cmdName: "container run",
			fc:      &config.Finch{SharedSettings: config.SharedSettings{DockerCompat: true}},
			args:    []string{"-it", "alpine"},
			want:    []string{"--interactive", "--tty", "alpine"},
			handled: true,
		},
		{
			name:    "image build and build share one table",
			cmdName: "image build",
			fc:      &config.Finch{SharedSettings: config.SharedSettings{DockerCompat: true}},
			args:    []string{"--load", "-t", "x", "."},
			want:    []string{"--output=type=docker", "--tag", "x", "."},
			handled: true,
		},
		{
			name:    "the management form keeps its sub-command word",
			cmdName: "container",
			fc:      &config.Finch{SharedSettings: config.SharedSettings{DockerCompat: true}},
			args:    []string{"run", "-it", "alpine"},
			want:    []string{"run", "--interactive", "--tty", "alpine"},
			handled: true,
		},
		{
			name:    "a management form without a matrix sub-command is untouched",
			cmdName: "container",
			fc:      &config.Finch{SharedSettings: config.SharedSettings{DockerCompat: true}},
			args:    []string{"logs", "--expose", "80"},
			want:    []string{"logs", "--expose", "80"},
			handled: false,
		},
		{
			name:    "the image build management form is translated",
			cmdName: "image",
			fc:      &config.Finch{SharedSettings: config.SharedSettings{DockerCompat: true}},
			args:    []string{"build", "--load", "."},
			want:    []string{"build", "--output=type=docker", "."},
			handled: true,
		},
		{
			name:     "an explicit platform is not overridden by the environment",
			cmdName:  "run",
			fc:       &config.Finch{SharedSettings: config.SharedSettings{DockerCompat: true}},
			args:     []string{"--platform", "amd64", "alpine"},
			platform: "arm64",
			want:     []string{"--platform", "amd64", "alpine"},
			handled:  true,
		},
		{
			name:     "the platform environment default is injected",
			cmdName:  "run",
			fc:       &config.Finch{SharedSettings: config.SharedSettings{DockerCompat: true}},
			args:     []string{"alpine"},
			platform: "linux/arm64",
			want:     []string{"--platform", "linux/arm64", "alpine"},
			handled:  true,
		},
		{
			name:    "unsupported flags fail the translation",
			cmdName: "create",
			fc:      &config.Finch{SharedSettings: config.SharedSettings{DockerCompat: true}},
			args:    []string{"--link", "a:b", "alpine"},
			wantErr: "--link",
			handled: true,
		},
		{
			name:    "lenient mode reports a warning and continues",
			cmdName: "create",
			fc: &config.Finch{SharedSettings: config.SharedSettings{
				DockerCompat: true, DockerCompatLenient: true,
			}},
			args:    []string{"--link", "a:b", "alpine"},
			want:    []string{"alpine"},
			handled: true,
		},
		{
			name:    "a bare run still shows help",
			cmdName: "run",
			fc:      &config.Finch{SharedSettings: config.SharedSettings{DockerCompat: true}},
			args:    nil,
			want:    []string{"--help"},
			handled: true,
		},
		{
			name:    "a bare create is left alone",
			cmdName: "create",
			fc:      &config.Finch{SharedSettings: config.SharedSettings{DockerCompat: true}},
			args:    nil,
			want:    nil,
			handled: true,
		},
		{
			name:      "debug is consumed and raises the log level",
			cmdName:   "run",
			fc:        &config.Finch{SharedSettings: config.SharedSettings{DockerCompat: true}},
			args:      []string{"--debug", "alpine"},
			want:      []string{"alpine"},
			handled:   true,
			wantDebug: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			ncsd := mocks.NewNerdctlCommandSystemDeps(ctrl)
			logger := mocks.NewLogger(ctrl)
			ncsd.EXPECT().LookupEnv("DOCKER_DEFAULT_PLATFORM").Return(tc.platform, tc.platform != "").AnyTimes()
			logger.EXPECT().Warnf(gomock.Any(), gomock.Any()).AnyTimes()
			if tc.wantDebug {
				logger.EXPECT().SetLevel(flog.Debug)
			}

			nc := newNerdctlCommand(nil, nil, ncsd, logger, nil, tc.fc, nil)
			got, handled, err := nc.translateArgs(tc.cmdName, tc.args)
			assert.Equal(t, tc.handled, handled)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
