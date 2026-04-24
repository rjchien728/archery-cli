package client

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// Login establishes an authenticated session with Archery.
//
// hhyo/Archery's login form is JS-driven: the page POSTs to /authenticate/
// (not /login/) over AJAX with username + password. We mirror that:
//
//  1. GET /login/ — primes the csrftoken cookie (the page itself sets it).
//  2. POST /authenticate/ with X-CSRFToken header + form (username, password).
//     Response is {"status": 0, "msg": "ok", "data": null} on success and the
//     server sets a sessionid cookie.
func (c *Client) Login() error {
	c.verbosef("login start")
	if _, _, err := c.request(reqSpec{
		method:    "GET",
		path:      "/login/",
		autoLogin: false,
	}); err != nil {
		return fmt.Errorf("login: GET /login/: %w", err)
	}
	if c.csrfFromJar() == "" {
		return fmt.Errorf("login: no csrftoken cookie set by /login/; endpoint may not be a hhyo/Archery instance")
	}

	form := url.Values{
		"username": {c.cfg.Username},
		"password": {c.cfg.Password},
	}
	status, body, err := c.request(reqSpec{
		method:    "POST",
		path:      "/authenticate/",
		form:      form,
		autoLogin: false,
	})
	if err != nil {
		return fmt.Errorf("login: POST /authenticate/: %w", err)
	}
	if status >= 500 {
		return fmt.Errorf("login: archery server error HTTP %d", status)
	}
	if status >= 400 {
		return fmt.Errorf("login: HTTP %d: %s", status, snippet(body))
	}

	var env struct {
		Status int    `json:"status"`
		Msg    string `json:"msg"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("login: decode /authenticate/ response: %w (body: %s)", err, snippet(body))
	}
	if env.Status != 0 {
		// Don't echo the password; surface server's message verbatim (typically
		// "用户名或密码错误").
		c.verbosef("authenticate rejected (status=%d msg=%q)", env.Status, env.Msg)
		return ErrAuthFailed
	}
	if !c.hasSessionCookie() {
		return fmt.Errorf("login: server returned ok but no sessionid cookie")
	}
	c.verbosef("login ok")
	return nil
}
