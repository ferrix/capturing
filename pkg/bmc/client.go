/*
Copyright 2026 ferrix

SPDX-License-Identifier: MIT
*/

// Package bmc is an HTTP client for the Turing Pi 2 BMC API (firmware ≥ 2.0).
package bmc

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	// ErrUnauthorized is returned when BMC authentication fails.
	ErrUnauthorized = errors.New("bmc: unauthorized")
	// ErrInvalidNode is returned when the slot is outside 1–4.
	ErrInvalidNode = errors.New("bmc: node must be 1-4")
	// ErrFlashTimeout is returned when a flash session never reaches completion.
	ErrFlashTimeout = errors.New("bmc: flash did not complete")
)

// Power is per-slot power state. Keys are 1–4.
type Power map[int]bool

// Client talks to a Turing Pi BMC.
type Client interface {
	PowerGet(ctx context.Context) (Power, error)
	PowerSet(ctx context.Context, node int, on bool) error
	Flash(ctx context.Context, node int, image io.Reader, size int64) error
}

// HTTPClient implements Client against the BMC REST API.
type HTTPClient struct {
	BaseURL      string
	Username     string
	Password     string
	HTTP         *http.Client
	PollInterval time.Duration

	mu    sync.Mutex
	token string
}

// Option configures HTTPClient.
type Option func(*HTTPClient)

// WithHTTPClient sets the underlying HTTP client.
func WithHTTPClient(c *http.Client) Option {
	return func(h *HTTPClient) { h.HTTP = c }
}

// http1Transport is a fresh Transport that never enables HTTP/2.
// Cloning http.DefaultTransport keeps HTTP/2 from once.Do configure.
// The Turing Pi BMC TLS stack has no ALPN and still sends HTTP/2 SETTINGS.
func http1Transport() *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     false,
		TLSHandshakeTimeout:   10 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSNextProto:          map[string]func(authority string, c *tls.Conn) http.RoundTripper{},
	}
}

func applyHTTP1(tr *http.Transport) *http.Transport {
	tr.ForceAttemptHTTP2 = false
	tr.TLSNextProto = map[string]func(authority string, c *tls.Conn) http.RoundTripper{}
	return tr
}

// WithInsecureSkipVerify skips TLS verification.
func WithInsecureSkipVerify() Option {
	return func(h *HTTPClient) {
		if h.HTTP == nil {
			h.HTTP = &http.Client{Timeout: 30 * time.Second, Transport: http1Transport()}
		}
		transport, ok := h.HTTP.Transport.(*http.Transport)
		if !ok || transport == nil {
			transport = http1Transport()
		} else {
			transport = transport.Clone()
			applyHTTP1(transport)
		}
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{}
		}
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
		transport.TLSClientConfig.InsecureSkipVerify = true
		h.HTTP.Transport = transport
	}
}

// WithPollInterval sets flash progress poll interval.
func WithPollInterval(d time.Duration) Option {
	return func(h *HTTPClient) { h.PollInterval = d }
}

// New returns an HTTP BMC client.
func New(baseURL, username, password string, opts ...Option) *HTTPClient {
	c := &HTTPClient{
		BaseURL:      strings.TrimRight(baseURL, "/"),
		Username:     username,
		Password:     password,
		HTTP:         &http.Client{Timeout: 30 * time.Second, Transport: http1Transport()},
		PollInterval: time.Second,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ProviderID returns the CAPI provider ID for a BMC host and slot.
func ProviderID(bmcURL string, node int) string {
	host := bmcURL
	if u, err := url.Parse(bmcURL); err == nil && u.Host != "" {
		host = u.Host
	}
	return fmt.Sprintf("turingpi://%s/node/%d", host, node)
}

func validateNode(node int) error {
	if node < 1 || node > 4 {
		return ErrInvalidNode
	}
	return nil
}

type authRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type authResponse struct {
	ID string `json:"id"`
}

func (c *HTTPClient) ensureAuth(ctx context.Context) error {
	c.mu.Lock()
	if c.token != "" {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()
	return c.authenticate(ctx)
}

func (c *HTTPClient) authenticate(ctx context.Context) error {
	body, err := json.Marshal(authRequest{Username: c.Username, Password: c.Password})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/bmc/authenticate", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return ErrUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slurp, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("bmc: authenticate: status %d: %s", resp.StatusCode, slurp)
	}
	token := resp.Header.Get("X-Auth-Token")
	var parsed authResponse
	_ = json.NewDecoder(resp.Body).Decode(&parsed)
	if token == "" {
		token = parsed.ID
	}
	if token == "" {
		return ErrUnauthorized
	}
	c.mu.Lock()
	c.token = token
	c.mu.Unlock()
	return nil
}

func (c *HTTPClient) doBMC(ctx context.Context, method, rawQuery string, body io.Reader) ([]byte, error) {
	if err := c.ensureAuth(ctx); err != nil {
		return nil, err
	}
	u := c.BaseURL + "/api/bmc"
	if rawQuery != "" {
		u += "?" + rawQuery
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	token := c.token
	c.mu.Unlock()
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		c.mu.Lock()
		c.token = ""
		c.mu.Unlock()
		return nil, ErrUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("bmc: %s %s: status %d: %s", method, rawQuery, resp.StatusCode, payload)
	}
	return payload, nil
}

type envelope struct {
	Response []struct {
		Result []map[string]any `json:"result"`
	} `json:"response"`
}

func firstResult(payload []byte) (map[string]any, error) {
	var env envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return nil, fmt.Errorf("bmc: decode: %w", err)
	}
	if len(env.Response) == 0 || len(env.Response[0].Result) == 0 {
		return nil, fmt.Errorf("bmc: empty response")
	}
	return env.Response[0].Result[0], nil
}

func resultString(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatInt(int64(t), 10)
	case json.Number:
		return t.String()
	default:
		return fmt.Sprint(t)
	}
}

// PowerGet returns power state for nodes 1–4.
func (c *HTTPClient) PowerGet(ctx context.Context) (Power, error) {
	payload, err := c.doBMC(ctx, http.MethodGet, "opt=get&type=power", nil)
	if err != nil {
		return nil, err
	}
	result, err := firstResult(payload)
	if err != nil {
		return nil, err
	}
	out := Power{}
	for i := 1; i <= 4; i++ {
		out[i] = resultString(result, fmt.Sprintf("node%d", i)) == "1"
	}
	return out, nil
}

// PowerSet turns a slot on or off. node is 1–4.
func (c *HTTPClient) PowerSet(ctx context.Context, node int, on bool) error {
	if err := validateNode(node); err != nil {
		return err
	}
	val := "0"
	if on {
		val = "1"
	}
	q := fmt.Sprintf("opt=set&type=power&node%d=%s", node, val)
	_, err := c.doBMC(ctx, http.MethodGet, q, nil)
	return err
}

// Flash streams an image to a slot. node is 1–4. BMC flash API uses 0-based node.
func (c *HTTPClient) Flash(ctx context.Context, node int, image io.Reader, size int64) error {
	if err := validateNode(node); err != nil {
		return err
	}
	if size < 0 {
		return fmt.Errorf("bmc: flash size must be >= 0")
	}
	q := fmt.Sprintf("opt=set&type=flash&node=%d&length=%d", node-1, size)
	payload, err := c.doBMC(ctx, http.MethodGet, q, nil)
	if err != nil {
		return err
	}
	result, err := firstResult(payload)
	if err != nil {
		return err
	}
	handle := resultString(result, "handle")
	if handle == "" {
		return fmt.Errorf("bmc: flash: missing handle")
	}
	if err := c.upload(ctx, handle, image); err != nil {
		return err
	}
	return c.waitFlash(ctx)
}

func (c *HTTPClient) upload(ctx context.Context, handle string, image io.Reader) error {
	if err := c.ensureAuth(ctx); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/bmc/upload/"+url.PathEscape(handle), image)
	if err != nil {
		return err
	}
	c.mu.Lock()
	token := c.token
	c.mu.Unlock()
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slurp, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("bmc: upload: status %d: %s", resp.StatusCode, slurp)
	}
	return nil
}

func (c *HTTPClient) waitFlash(ctx context.Context) error {
	interval := c.PollInterval
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		done, err := c.flashDone(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ErrFlashTimeout
			}
			return err
		}
		if done {
			return nil
		}
		select {
		case <-ctx.Done():
			return ErrFlashTimeout
		case <-ticker.C:
		}
	}
}

func (c *HTTPClient) flashDone(ctx context.Context) (bool, error) {
	payload, err := c.doBMC(ctx, http.MethodGet, "opt=get&type=flash", nil)
	if err != nil {
		return false, err
	}
	result, err := firstResult(payload)
	if err != nil {
		return false, err
	}
	status := strings.ToLower(resultString(result, "status"))
	percent := resultString(result, "percent")
	if status == "done" || status == "complete" || percent == "100" {
		return true, nil
	}
	if status == "error" || status == "failed" {
		return false, fmt.Errorf("bmc: flash failed: %s", resultString(result, "message"))
	}
	return false, nil
}
