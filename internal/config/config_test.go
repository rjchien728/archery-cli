package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfig_ResolveDB(t *testing.T) {
	tests := []struct {
		desc       string
		aliases    map[string]string
		input      string
		expectedDB string
		expectedOK bool
	}{
		{
			desc:       "alias hit returns full name",
			aliases:    map[string]string{"prod": "db_orders_prod", "stg": "db_orders_stg"},
			input:      "prod",
			expectedDB: "db_orders_prod",
			expectedOK: true,
		},
		{
			desc:       "unknown short name passes through",
			aliases:    map[string]string{"prod": "db_orders_prod"},
			input:      "db_orders_dev",
			expectedDB: "db_orders_dev",
			expectedOK: false,
		},
		{
			desc:       "empty alias map passes through",
			aliases:    map[string]string{},
			input:      "mydb",
			expectedDB: "mydb",
			expectedOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			cfg := &Config{Aliases: tt.aliases}
			gotDB, gotOK := cfg.ResolveDB(tt.input)
			assert.Equal(t, tt.expectedDB, gotDB)
			assert.Equal(t, tt.expectedOK, gotOK)
		})
	}
}

func TestParseAliases(t *testing.T) {
	tests := []struct {
		desc     string
		input    string
		expected map[string]string
	}{
		{
			desc:     "empty string returns empty map",
			input:    "",
			expected: map[string]string{},
		},
		{
			desc:     "single pair",
			input:    "prod=db_orders_prod",
			expected: map[string]string{"prod": "db_orders_prod"},
		},
		{
			desc:     "multiple pairs with surrounding whitespace",
			input:    " prod = db_orders_prod , stg = db_orders_stg ",
			expected: map[string]string{"prod": "db_orders_prod", "stg": "db_orders_stg"},
		},
		{
			desc:     "skips token without equals",
			input:    "prod=db_prod,garbage,stg=db_stg",
			expected: map[string]string{"prod": "db_prod", "stg": "db_stg"},
		},
		{
			desc:     "skips trailing equals (empty value)",
			input:    "prod=db_prod,orphan=",
			expected: map[string]string{"prod": "db_prod"},
		},
		{
			desc:     "skips leading equals (empty key)",
			input:    "prod=db_prod,=orphan",
			expected: map[string]string{"prod": "db_prod"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got := parseAliases(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}
