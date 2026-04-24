package main

import (
	"errors"
	"fmt"
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
