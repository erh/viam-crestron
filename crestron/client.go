// Package crestron is a client for the Crestron Home (Pyng) REST API exposed
// by 4-Series processors at https://<host>/cws/api.
//
// Auth is a two step flow: a long lived API token (generated in the Crestron
// Home setup app under Installer Settings > System Control Options > Web API)
// is exchanged for a short lived session "authkey" that must accompany every
// other request. Sessions expire after about ten minutes of inactivity, so the
// client re-logs in transparently when the server rejects the key.
package crestron

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	headerToken   = "Crestron-RestAPI-AuthToken"
	headerAuthKey = "Crestron-RestAPI-AuthKey"

	// MaxLevel is the fully on value for a dimmer level.
	MaxLevel = 65535
)

// Client talks to one Crestron Home processor.
type Client struct {
	host  string
	token string
	http  *http.Client

	mu      sync.Mutex
	authKey string

	// sem caps how many requests are in flight at once. The processor's web
	// server has very few connection slots, so a burst of per-light requests
	// can exhaust it and hang the whole API; this keeps us well under that.
	sem chan struct{}
}

// maxConcurrent bounds simultaneous requests to the processor.
const maxConcurrent = 2

// Option customizes a Client.
type Option func(*Client)

// WithHTTPClient replaces the underlying http client. The default one skips
// certificate verification, since processors ship with a self signed cert.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// New returns a client for the processor at host (an IP or hostname, with an
// optional https:// prefix) using the given long lived API token.
func New(host, token string, opts ...Option) *Client {
	host = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://"), "/")
	c := &Client{
		host:  host,
		token: token,
		http: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
				MaxConnsPerHost:     maxConcurrent,
				MaxIdleConns:        maxConcurrent,
				MaxIdleConnsPerHost: maxConcurrent,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		sem: make(chan struct{}, maxConcurrent),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// apiError is the error envelope every endpoint returns on failure.
type apiError struct {
	Source  int    `json:"errorSource"`
	Message string `json:"errorMessage"`
	Version string `json:"version"`
}

func (e *apiError) Error() string {
	return fmt.Sprintf("crestron api error %d: %s", e.Source, e.Message)
}

// errNoAuth reports whether the error is the processor telling us our session
// key is missing or stale, which is our cue to log in again.
func errNoAuth(err error) bool {
	ae, ok := err.(*apiError)
	return ok && ae.Source == 5002
}

// Login exchanges the API token for a session key. Do returns to it on its
// own, so callers only need it to validate credentials up front.
func (c *Client) Login(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url("/login"), nil)
	if err != nil {
		return err
	}
	req.Header.Set(headerToken, c.token)

	var out struct {
		Version string `json:"version"`
		AuthKey string `json:"authkey"`
	}
	if err := c.send(req, &out); err != nil {
		return err
	}
	if out.AuthKey == "" {
		return fmt.Errorf("login succeeded but returned no authkey")
	}

	c.mu.Lock()
	c.authKey = out.AuthKey
	c.mu.Unlock()
	return nil
}

func (c *Client) url(path string) string {
	return "https://" + c.host + "/cws/api" + path
}

func (c *Client) key() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.authKey
}

// send performs a request and decodes the body into out, turning the API's
// error envelope into an *apiError. It holds a concurrency slot for the whole
// round trip so the processor never sees more than maxConcurrent connections.
func (c *Client) send(req *http.Request, out any) error {
	if c.sem != nil {
		select {
		case c.sem <- struct{}{}:
			defer func() { <-c.sem }()
		case <-req.Context().Done():
			return req.Context().Err()
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// Errors come back with a 200 as often as not, so sniff the body rather
	// than trusting the status code.
	var ae apiError
	if json.Unmarshal(body, &ae) == nil && ae.Source != 0 {
		return &ae
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if out == nil {
		return nil
	}
	if raw, ok := out.(*json.RawMessage); ok {
		*raw = json.RawMessage(body)
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decoding %s: %w (body: %s)", req.URL.Path, err, truncate(string(body), 400))
	}
	return nil
}

// Do issues an authenticated request against an /cws/api path, logging in
// first if needed and once more if the session key has expired.
func (c *Client) Do(ctx context.Context, method, path string, in, out any) error {
	if c.key() == "" {
		if err := c.Login(ctx); err != nil {
			return err
		}
	}
	err := c.do(ctx, method, path, in, out)
	if errNoAuth(err) {
		if lerr := c.Login(ctx); lerr != nil {
			return lerr
		}
		err = c.do(ctx, method, path, in, out)
	}
	return err
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.url(path), body)
	if err != nil {
		return err
	}
	req.Header.Set(headerAuthKey, c.key())
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.send(req, out)
}

// Raw fetches a path and returns the undecoded JSON, for exploring endpoints
// this package does not model yet.
func (c *Client) Raw(ctx context.Context, path string) (json.RawMessage, error) {
	var raw json.RawMessage
	err := c.Do(ctx, http.MethodGet, path, nil, &raw)
	return raw, err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
