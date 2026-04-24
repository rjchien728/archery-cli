package client

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
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

	"github.com/rjchien728/archery-cli/internal/config"
)

func TestDefaultCookiePath_NoHome(t *testing.T) {
	t.Setenv("HOME", "")
	_, err := defaultCookiePath()
	require.Error(t, err, "expected error when HOME is unavailable")
	assert.Contains(t, err.Error(), "cookie cache path")
}

func TestBuildTLSConfig(t *testing.T) {
	validPEM := filepath.Join(t.TempDir(), "ca.pem")
	writeSelfSignedCA(t, validPEM)

	garbagePEM := filepath.Join(t.TempDir(), "garbage.pem")
	require.NoError(t, os.WriteFile(garbagePEM, []byte("not a pem file"), 0o600))

	tests := []struct {
		desc      string
		cfg       *config.Config
		assertion func(t *testing.T, tlsCfg *tls.Config, err error)
	}{
		{
			desc: "insecure short-circuits and skips verification",
			cfg:  &config.Config{Insecure: true},
			assertion: func(t *testing.T, tlsCfg *tls.Config, err error) {
				require.NoError(t, err)
				require.NotNil(t, tlsCfg)
				assert.True(t, tlsCfg.InsecureSkipVerify)
				assert.Nil(t, tlsCfg.RootCAs)
			},
		},
		{
			desc: "insecure wins over cacert (cacert is ignored, not loaded)",
			cfg:  &config.Config{Insecure: true, CACertPath: "/nonexistent.pem"},
			assertion: func(t *testing.T, tlsCfg *tls.Config, err error) {
				require.NoError(t, err, "cacert must be skipped when insecure is set, even if the path is invalid")
				assert.True(t, tlsCfg.InsecureSkipVerify)
			},
		},
		{
			desc: "valid cacert populates RootCAs",
			cfg:  &config.Config{CACertPath: validPEM},
			assertion: func(t *testing.T, tlsCfg *tls.Config, err error) {
				require.NoError(t, err)
				require.NotNil(t, tlsCfg.RootCAs)
				assert.False(t, tlsCfg.InsecureSkipVerify)
			},
		},
		{
			desc: "missing cacert file returns error",
			cfg:  &config.Config{CACertPath: "/does/not/exist.pem"},
			assertion: func(t *testing.T, tlsCfg *tls.Config, err error) {
				require.Error(t, err)
				assert.Nil(t, tlsCfg)
				assert.Contains(t, err.Error(), "read --cacert file")
			},
		},
		{
			desc: "garbage cacert file returns 'no valid PEM' error",
			cfg:  &config.Config{CACertPath: garbagePEM},
			assertion: func(t *testing.T, tlsCfg *tls.Config, err error) {
				require.Error(t, err)
				assert.Nil(t, tlsCfg)
				assert.Contains(t, err.Error(), "no valid PEM")
			},
		},
		{
			desc: "no tls options returns default config",
			cfg:  &config.Config{},
			assertion: func(t *testing.T, tlsCfg *tls.Config, err error) {
				require.NoError(t, err)
				require.NotNil(t, tlsCfg)
				assert.False(t, tlsCfg.InsecureSkipVerify)
				assert.Nil(t, tlsCfg.RootCAs)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			tlsCfg, err := buildTLSConfig(tt.cfg)
			tt.assertion(t, tlsCfg, err)
		})
	}
}

// writeSelfSignedCA writes a short-lived self-signed CA cert to path so tests
// can exercise the RootCAs loading path without network / fixture files.
func writeSelfSignedCA(t *testing.T, path string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "archery-cli-test-ca"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	require.NoError(t, os.WriteFile(path, pemBytes, 0o600))
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
