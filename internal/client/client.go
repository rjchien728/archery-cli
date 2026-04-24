package client

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/net/proxy"
	"golang.org/x/net/publicsuffix"

	"github.com/rjchien728/archery-cli/internal/config"
)

const (
	defaultTimeout = 30 * time.Second
)

var ErrAuthFailed = errors.New("auth failed; check ARCHERY_USERNAME / ARCHERY_PASSWORD")

type Client struct {
	cfg        *config.Config
	httpc      *http.Client
	jar        *cookiejar.Jar
	endpoint   *url.URL
	cookiePath string
	verbose    io.Writer
}

type Option func(*Client)

func WithVerbose(w io.Writer) Option {
	return func(c *Client) { c.verbose = w }
}

func WithCookiePath(p string) Option {
	return func(c *Client) { c.cookiePath = p }
}

func New(cfg *config.Config, opts ...Option) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	endpoint, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("bad ARCHERY_URL: %w", err)
	}

	tr := &http.Transport{
		TLSClientConfig:       &tls.Config{},
		ResponseHeaderTimeout: defaultTimeout,
	}
	if err := setProxy(tr); err != nil {
		return nil, err
	}

	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return nil, err
	}

	c := &Client{
		cfg:      cfg,
		jar:      jar,
		endpoint: endpoint,
		httpc: &http.Client{
			Transport: tr,
			Jar:       jar,
			Timeout:   defaultTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
	for _, o := range opts {
		o(c)
	}
	if c.cookiePath == "" {
		c.cookiePath = defaultCookiePath()
	}
	c.loadCookies()
	return c, nil
}

func defaultCookiePath() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".cache", "archery", "cookies.json")
	}
	return filepath.Join(os.TempDir(), "archery-cookies.json")
}

func setProxy(tr *http.Transport) error {
	raw := firstEnv("HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy")
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("bad proxy URL %q: %w", raw, err)
	}
	switch strings.ToLower(u.Scheme) {
	case "socks5", "socks5h":
		var auth *proxy.Auth
		if u.User != nil {
			pw, _ := u.User.Password()
			auth = &proxy.Auth{User: u.User.Username(), Password: pw}
		}
		d, err := proxy.SOCKS5("tcp", u.Host, auth, &net.Dialer{Timeout: 10 * time.Second})
		if err != nil {
			return fmt.Errorf("socks5 dialer: %w", err)
		}
		ctxd, ok := d.(proxy.ContextDialer)
		if !ok {
			return errors.New("socks5 dialer is not a ContextDialer")
		}
		tr.DialContext = ctxd.DialContext
	default:
		tr.Proxy = http.ProxyURL(u)
	}
	return nil
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

func (c *Client) verbosef(format string, args ...any) {
	if c.verbose != nil {
		fmt.Fprintf(c.verbose, "[archery] "+format+"\n", args...)
	}
}

// reqSpec describes one HTTP call.
type reqSpec struct {
	method    string
	path      string
	query     url.Values
	form      url.Values
	autoLogin bool
}

func (c *Client) request(rs reqSpec) (status int, body []byte, err error) {
	const maxAttempts = 2
	for attempt := 0; attempt < maxAttempts; attempt++ {
		req, rerr := c.buildRequest(rs)
		if rerr != nil {
			return 0, nil, rerr
		}
		started := time.Now()
		resp, herr := c.httpc.Do(req)
		if herr != nil {
			return 0, nil, fmt.Errorf("network error: %w (is HTTPS_PROXY set / viaproxy used?)", herr)
		}
		body, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		c.saveCookies()
		c.verbosef("%s %s -> %d (%s, %d bytes)", rs.method, rs.path, resp.StatusCode, time.Since(started).Truncate(time.Millisecond), len(body))

		if rs.autoLogin && attempt == 0 && needsLogin(resp) {
			c.verbosef("session expired, logging in")
			if err := c.Login(); err != nil {
				return 0, nil, err
			}
			continue
		}
		return resp.StatusCode, body, nil
	}
	return 0, nil, errors.New("request: exceeded retries")
}

func (c *Client) buildRequest(rs reqSpec) (*http.Request, error) {
	u := *c.endpoint
	u.Path = rs.path
	if rs.query != nil {
		u.RawQuery = rs.query.Encode()
	}
	var body io.Reader
	if rs.form != nil {
		body = strings.NewReader(rs.form.Encode())
	}
	req, err := http.NewRequest(rs.method, u.String(), body)
	if err != nil {
		return nil, err
	}
	if rs.form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	}
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Referer", c.endpoint.String()+"/sqlquery/")
	req.Header.Set("Origin", c.endpoint.String())
	if csrf := c.csrfFromJar(); csrf != "" {
		req.Header.Set("X-CSRFToken", csrf)
	}
	return req, nil
}

func needsLogin(resp *http.Response) bool {
	if resp.StatusCode == 403 {
		return true
	}
	if resp.StatusCode == 302 || resp.StatusCode == 301 {
		loc := resp.Header.Get("Location")
		return strings.Contains(loc, "/login") || strings.Contains(loc, "/accounts/login")
	}
	return false
}

func (c *Client) csrfFromJar() string {
	for _, ck := range c.jar.Cookies(c.endpoint) {
		if ck.Name == "csrftoken" {
			return ck.Value
		}
	}
	return ""
}

func (c *Client) hasSessionCookie() bool {
	for _, ck := range c.jar.Cookies(c.endpoint) {
		if ck.Name == "sessionid" {
			return true
		}
	}
	return false
}

type savedCookie struct {
	Name    string    `json:"name"`
	Value   string    `json:"value"`
	Path    string    `json:"path,omitempty"`
	Expires time.Time `json:"expires,omitempty"`
	Secure  bool      `json:"secure,omitempty"`
}

func (c *Client) saveCookies() {
	cookies := c.jar.Cookies(c.endpoint)
	if len(cookies) == 0 {
		return
	}
	saved := make([]savedCookie, 0, len(cookies))
	for _, ck := range cookies {
		saved = append(saved, savedCookie{
			Name: ck.Name, Value: ck.Value,
			Path: ck.Path, Expires: ck.Expires, Secure: ck.Secure,
		})
	}
	if err := os.MkdirAll(filepath.Dir(c.cookiePath), 0o700); err != nil {
		return
	}
	tmp := c.cookiePath + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	if err := json.NewEncoder(f).Encode(saved); err != nil {
		f.Close()
		os.Remove(tmp)
		return
	}
	f.Close()
	_ = os.Rename(tmp, c.cookiePath)
}

func (c *Client) loadCookies() {
	f, err := os.Open(c.cookiePath)
	if err != nil {
		return
	}
	defer f.Close()
	var saved []savedCookie
	if err := json.NewDecoder(f).Decode(&saved); err != nil {
		return
	}
	cookies := make([]*http.Cookie, 0, len(saved))
	for _, s := range saved {
		cookies = append(cookies, &http.Cookie{
			Name: s.Name, Value: s.Value,
			Path: s.Path, Expires: s.Expires, Secure: s.Secure,
		})
	}
	c.jar.SetCookies(c.endpoint, cookies)
}
