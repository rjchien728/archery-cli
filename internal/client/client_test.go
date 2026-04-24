package client

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/publicsuffix"
)

func TestDefaultCookiePath_NoHome(t *testing.T) {
	t.Setenv("HOME", "")
	_, err := defaultCookiePath()
	require.Error(t, err, "expected error when HOME is unavailable")
	assert.Contains(t, err.Error(), "cookie cache path")
}

func TestClient_SaveCookies_Permissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	path := filepath.Join(dir, "cookies.json")

	endpoint, err := url.Parse("https://archery.example.com")
	require.NoError(t, err)

	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	require.NoError(t, err)
	jar.SetCookies(endpoint, []*http.Cookie{
		{Name: "sessionid", Value: "abc123", Path: "/", Expires: time.Now().Add(time.Hour)},
		{Name: "csrftoken", Value: "xyz789", Path: "/"},
	})

	c := &Client{
		jar:        jar,
		endpoint:   endpoint,
		cookiePath: path,
	}

	c.saveCookies()

	fi, err := os.Stat(path)
	require.NoError(t, err, "cookie file should exist after saveCookies")
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm(), "cookie file must be 0600")

	di, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), di.Mode().Perm(), "cookie parent dir must be 0700")
}
