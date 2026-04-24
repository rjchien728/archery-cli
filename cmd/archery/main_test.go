package main

import (
	"errors"
	"fmt"
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rjchien728/archery-cli/internal/client"
)

func TestExitCodeFor(t *testing.T) {
	tests := []struct {
		desc     string
		input    error
		expected int
	}{
		{
			desc:     "usage error returns 2",
			input:    usageError("missing db"),
			expected: 2,
		},
		{
			desc:     "ErrAuthFailed returns 5",
			input:    client.ErrAuthFailed,
			expected: 5,
		},
		{
			desc:     "wrapped ErrAuthFailed returns 5",
			input:    fmt.Errorf("login step: %w", client.ErrAuthFailed),
			expected: 5,
		},
		{
			desc:     "ServerError returns 1",
			input:    &client.ServerError{Status: 1, Msg: "rejected"},
			expected: 1,
		},
		{
			desc:     "network error message returns 3",
			input:    errors.New("network error: dial tcp: connection refused"),
			expected: 3,
		},
		{
			desc:     "server error HTTP message returns 4",
			input:    errors.New("archery server error HTTP 502"),
			expected: 4,
		},
		{
			desc:     "generic error returns 1",
			input:    errors.New("something unexpected"),
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got := exitCodeFor(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestFormatVersion(t *testing.T) {
	vcsInfo := &debug.BuildInfo{
		Main: debug.Module{Version: "v0.2.1"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "abcdef1234567890"},
			{Key: "vcs.time", Value: "2026-04-25T10:30:00Z"},
		},
	}
	develInfo := &debug.BuildInfo{
		Main: debug.Module{Version: "(devel)"},
	}

	tests := []struct {
		desc     string
		ldV      string
		ldC      string
		ldD      string
		info     *debug.BuildInfo
		ok       bool
		expected string
	}{
		{
			desc: "ldflags values win over build info",
			ldV:  "v1.0.0", ldC: "deadbee", ldD: "2026-01-01",
			info:     vcsInfo,
			ok:       true,
			expected: "v1.0.0 (commit deadbee, built 2026-01-01)",
		},
		{
			desc: "all defaults with build info falls back to build info",
			ldV:  "dev", ldC: "none", ldD: "unknown",
			info:     vcsInfo,
			ok:       true,
			expected: "v0.2.1 (commit abcdef1, built 2026-04-25T10:30:00Z)",
		},
		{
			desc: "all defaults without build info stays dev/none/unknown",
			ldV:  "dev", ldC: "none", ldD: "unknown",
			info:     nil,
			ok:       false,
			expected: "dev (commit none, built unknown)",
		},
		{
			desc: "devel main version does not override dev",
			ldV:  "dev", ldC: "none", ldD: "unknown",
			info:     develInfo,
			ok:       true,
			expected: "dev (commit none, built unknown)",
		},
		{
			desc: "partial ldflags: version set, commit/date fill from build info",
			ldV:  "v1.0.0", ldC: "none", ldD: "unknown",
			info:     vcsInfo,
			ok:       true,
			expected: "v1.0.0 (commit abcdef1, built 2026-04-25T10:30:00Z)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got := formatVersion(tt.ldV, tt.ldC, tt.ldD, tt.info, tt.ok)
			assert.Equal(t, tt.expected, got)
		})
	}
}
