package config

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

type Config struct {
	Endpoint string
	Instance string
	Username string
	Password string
	Aliases  map[string]string
}

func Load() (*Config, error) {
	cfg := &Config{
		Endpoint: os.Getenv("ARCHERY_URL"),
		Instance: os.Getenv("ARCHERY_INSTANCE"),
		Username: os.Getenv("ARCHERY_USERNAME"),
		Password: os.Getenv("ARCHERY_PASSWORD"),
		Aliases:  parseAliases(os.Getenv("ARCHERY_ALIASES")),
	}
	return cfg, nil
}

func (c *Config) Validate() error {
	var missing []string
	if c.Endpoint == "" {
		missing = append(missing, "ARCHERY_URL")
	}
	if c.Instance == "" {
		missing = append(missing, "ARCHERY_INSTANCE")
	}
	if c.Username == "" {
		missing = append(missing, "ARCHERY_USERNAME")
	}
	if c.Password == "" {
		missing = append(missing, "ARCHERY_PASSWORD")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required config: %s\n\nExample:\n  export ARCHERY_URL=https://archery.example.com\n  export ARCHERY_INSTANCE=my-instance\n  export ARCHERY_USERNAME=alice\n  export ARCHERY_PASSWORD=secret",
			strings.Join(missing, ", "))
	}
	c.Endpoint = strings.TrimRight(c.Endpoint, "/")
	return nil
}

// ResolveDB returns the full db name. If short matches an alias, returns the
// aliased value; otherwise returns short unchanged. The bool is true when an
// alias hit; false when the value passed through.
func (c *Config) ResolveDB(short string) (string, bool) {
	if v, ok := c.Aliases[short]; ok {
		return v, true
	}
	return short, false
}

// AliasNames returns sorted list of configured aliases (for error messages).
func (c *Config) AliasNames() []string {
	out := make([]string, 0, len(c.Aliases))
	for k := range c.Aliases {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func parseAliases(raw string) map[string]string {
	out := map[string]string{}
	if raw == "" {
		return out
	}
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		eq := strings.IndexByte(pair, '=')
		if eq <= 0 || eq == len(pair)-1 {
			continue
		}
		k := strings.TrimSpace(pair[:eq])
		v := strings.TrimSpace(pair[eq+1:])
		if k != "" && v != "" {
			out[k] = v
		}
	}
	return out
}
