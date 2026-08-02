// Package client implements a read-only Go client for the remote API of a
// legacy PHP ISPConfig3 panel, speaking to its JSON handler:
// POST <base_url>/remote/json.php?<method> with a JSON object of named
// parameters, decoding the {"code","message","response"} envelope.
//
// Only login, logout and *_get methods are implemented — the client can
// never modify the legacy panel.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Fault is a legacy API fault: any response whose envelope code is not
// "ok". Code carries the legacy fault code (for example "remote_fault",
// "permission_denied", "invalid_method") and Message the legacy message
// text. Transport failures (unreachable host, non-2xx status, invalid
// JSON) are returned as plain errors, never as a Fault.
type Fault struct {
	// Code is the legacy fault code from the response envelope.
	Code string
	// Message is the legacy fault message text.
	Message string
}

// Error implements the error interface.
func (f *Fault) Error() string {
	return "legacy fault " + f.Code + ": " + f.Message
}

// ErrNotLoggedIn is returned when a method requiring a session is called
// before a successful Login.
var ErrNotLoggedIn = errors.New("legacy: not logged in")

// Options configures a Client.
type Options struct {
	// URL is the base URL of the legacy panel, e.g. "https://panel.example.com:8080".
	URL string
	// Username is the legacy remote_user name.
	Username string
	// Password is the legacy remote_user password. It is kept only in
	// memory and never appears in errors or logs.
	Password string
}

// Client is a read-only client for one legacy ISPConfig3 panel.
// Credentials and the session id live only in memory and are never
// included in error messages or logs.
type Client struct {
	endpoint  string
	username  string
	password  string
	sessionID string
	hc        *http.Client
}

// New validates the panel base URL and returns a Client for its JSON
// remote API endpoint.
func New(opts Options) (*Client, error) {
	u, err := url.Parse(opts.URL)
	if err != nil {
		return nil, fmt.Errorf("legacy: invalid panel URL: %w", err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("legacy: panel URL must be http(s)://host[:port], got %q", opts.URL)
	}
	return &Client{
		endpoint: strings.TrimRight(opts.URL, "/") + "/remote/json.php",
		username: opts.Username,
		password: opts.Password,
		hc:       &http.Client{},
	}, nil
}

// Login authenticates against the legacy panel with the remote_user
// credentials and stores the returned remote_session id; every subsequent
// call sends it as session_id. Login failures return the legacy fault
// without retrying, and never include the password.
func (c *Client) Login(ctx context.Context) error {
	var sessionID string
	err := c.call(ctx, "login", map[string]any{
		"username": c.username,
		"password": c.password,
	}, &sessionID)
	if err != nil {
		return err
	}
	if sessionID == "" {
		return errors.New("legacy: login returned an empty session id")
	}
	c.sessionID = sessionID
	return nil
}

// Logout ends the remote session on the legacy panel and forgets the
// stored session id. It is a no-op when not logged in.
func (c *Client) Logout(ctx context.Context) error {
	if c.sessionID == "" {
		return nil
	}
	err := c.call(ctx, "logout", nil, nil)
	c.sessionID = ""
	return err
}

// Close implements io.Closer by logging out the stored session, if any.
func (c *Client) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return c.Logout(ctx)
}

// envelope is the wire shape of every JSON handler response.
type envelope struct {
	Code     string          `json:"code"`
	Message  string          `json:"message"`
	Response json.RawMessage `json:"response"`
}

// call invokes one legacy method: POST <endpoint>?<method> with params as
// the JSON body, decoding the response envelope into out (skipped when out
// is nil or the legacy panel returned false/null). Every method except
// login gets the stored session id injected as session_id. A non-"ok"
// envelope code is returned as *Fault; everything else is a transport
// error.
func (c *Client) call(ctx context.Context, method string, params map[string]any, out any) error {
	if params == nil {
		params = map[string]any{}
	}
	if method != "login" {
		if c.sessionID == "" {
			return ErrNotLoggedIn
		}
		params["session_id"] = c.sessionID
	}
	body, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("legacy: %s: encoding request: %w", method, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"?"+method, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("legacy: %s: building request: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("legacy: %s: %w", method, err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close on a read

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("legacy: %s: unexpected HTTP status %d", method, resp.StatusCode)
	}

	var env envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("legacy: %s: decoding response: %w", method, err)
	}
	if env.Code != "ok" {
		return &Fault{Code: env.Code, Message: env.Message}
	}
	if out == nil {
		return nil
	}
	// PHP returns false (and sometimes null) instead of an empty result.
	if raw := string(env.Response); raw == "" || raw == "false" || raw == "null" {
		return nil
	}
	if err := json.Unmarshal(env.Response, out); err != nil {
		return fmt.Errorf("legacy: %s: decoding response payload: %w", method, err)
	}
	return nil
}

// IsFault reports whether err is a legacy API fault (as opposed to a
// transport failure) and returns it.
func IsFault(err error) (*Fault, bool) {
	var f *Fault
	ok := errors.As(err, &f)
	return f, ok
}
