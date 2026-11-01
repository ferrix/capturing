/*
Copyright 2026 ferrix

SPDX-License-Identifier: MIT
*/

// Package fake is a wire-compatible Turing Pi BMC for tests.
package fake

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
)

// BMC is an in-memory Turing Pi BMC with the firmware ≥ 2.0 HTTP API.
type BMC struct {
	Username string
	Password string

	FailAuth     bool
	FailPowerGet bool
	FailPowerSet bool
	FailFlash    bool
	FlashStuck   bool
	FlashError   bool

	mu           sync.Mutex
	token        string
	nodes        [5]nodeState // index 1–4
	handleSeq    int
	flashHandle  string
	flashNode    int // 1–4
	flashPercent int
	flashStatus  string
	powerGets    int
	powerSets    int
	flashes      int
	auths        int

	Server *httptest.Server
}

type nodeState struct {
	On         bool
	FlashSHA   string
	FlashBytes int64
	Image      []byte
}

// Option configures a BMC.
type Option func(*BMC)

// WithCredentials sets BMC login.
func WithCredentials(user, pass string) Option {
	return func(b *BMC) {
		b.Username = user
		b.Password = pass
	}
}

// New starts an httptest BMC. Call Close when finished.
func New(opts ...Option) *BMC {
	b := &BMC{
		Username: "root",
		Password: "turing",
		token:    "test-token-capturing",
	}
	for _, opt := range opts {
		opt(b)
	}
	b.Server = httptest.NewServer(http.HandlerFunc(b.serve))
	return b
}

// Close shuts down the test server.
func (b *BMC) Close() {
	if b.Server != nil {
		b.Server.Close()
	}
}

// URL is the BMC base URL.
func (b *BMC) URL() string {
	return b.Server.URL
}

// Power returns whether slot n (1–4) is on.
func (b *BMC) Power(n int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n < 1 || n > 4 {
		return false
	}
	return b.nodes[n].On
}

// SetPower sets slot n (1–4).
func (b *BMC) SetPower(n int, on bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n >= 1 && n <= 4 {
		b.nodes[n].On = on
	}
}

// LastFlashSHA is the SHA-256 of the last image flashed to slot n.
func (b *BMC) LastFlashSHA(n int) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n < 1 || n > 4 {
		return ""
	}
	return b.nodes[n].FlashSHA
}

// LastFlashBytes is the size of the last image flashed to slot n.
func (b *BMC) LastFlashBytes(n int) int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n < 1 || n > 4 {
		return 0
	}
	return b.nodes[n].FlashBytes
}

// PowerGetCalls is how many power GET requests succeeded past auth.
func (b *BMC) PowerGetCalls() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.powerGets
}

// PowerSetCalls is how many power SET requests were handled.
func (b *BMC) PowerSetCalls() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.powerSets
}

// FlashCalls is how many flash sessions were started.
func (b *BMC) FlashCalls() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.flashes
}

// AuthCalls is how many authenticate requests were received.
func (b *BMC) AuthCalls() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.auths
}

func (b *BMC) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/bmc/authenticate" && r.Method == http.MethodPost:
		b.handleAuth(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/bmc/upload/"):
		b.handleUpload(w, r)
	case r.URL.Path == "/api/bmc":
		b.handleBMC(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (b *BMC) handleAuth(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	b.auths++
	fail := b.FailAuth
	user, pass, token := b.Username, b.Password, b.token
	b.mu.Unlock()

	var cred struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	_ = json.NewDecoder(r.Body).Decode(&cred)
	if fail || cred.Username != user || cred.Password != pass {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	w.Header().Set("X-Auth-Token", token)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"id":          token,
		"name":        "User Session",
		"description": "User Session",
		"username":    cred.Username,
	})
}

func (b *BMC) authorized(r *http.Request) bool {
	got := r.Header.Get("Authorization")
	b.mu.Lock()
	defer b.mu.Unlock()
	return got == "Bearer "+b.token
}

func (b *BMC) handleBMC(w http.ResponseWriter, r *http.Request) {
	if !b.authorized(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	q := r.URL.Query()
	opt, typ := q.Get("opt"), q.Get("type")
	switch {
	case opt == "get" && typ == "power":
		b.handlePowerGet(w)
	case (opt == "set" && typ == "power") || (opt == "power" && typ == "set"):
		b.handlePowerSet(w, q)
	case opt == "set" && typ == "flash":
		b.handleFlashStart(w, q)
	case opt == "get" && typ == "flash":
		b.handleFlashGet(w)
	default:
		http.Error(w, `{"error":"unknown"}`, http.StatusBadRequest)
	}
}

func envelope(result map[string]any) []byte {
	payload := map[string]any{
		"response": []any{
			map[string]any{
				"result": []any{result},
			},
		},
	}
	raw, _ := json.Marshal(payload)
	return raw
}

func (b *BMC) handlePowerGet(w http.ResponseWriter) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.powerGets++
	if b.FailPowerGet {
		http.Error(w, `{"error":"power get failed"}`, http.StatusInternalServerError)
		return
	}
	result := map[string]any{}
	for i := 1; i <= 4; i++ {
		v := "0"
		if b.nodes[i].On {
			v = "1"
		}
		result[fmt.Sprintf("node%d", i)] = v
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(envelope(result))
}

func (b *BMC) handlePowerSet(w http.ResponseWriter, q map[string][]string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.powerSets++
	if b.FailPowerSet {
		http.Error(w, `{"error":"power set failed"}`, http.StatusInternalServerError)
		return
	}
	for i := 1; i <= 4; i++ {
		key := fmt.Sprintf("node%d", i)
		vals, ok := q[key]
		if !ok || len(vals) == 0 {
			continue
		}
		b.nodes[i].On = vals[0] == "1"
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(envelope(map[string]any{"result": "ok"}))
}

func (b *BMC) handleFlashStart(w http.ResponseWriter, q map[string][]string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.flashes++
	if b.FailFlash {
		http.Error(w, `{"error":"flash failed"}`, http.StatusInternalServerError)
		return
	}
	node0, err := strconv.Atoi(first(q["node"]))
	if err != nil || node0 < 0 || node0 > 3 {
		http.Error(w, `{"error":"invalid node"}`, http.StatusBadRequest)
		return
	}
	b.handleSeq++
	b.flashHandle = fmt.Sprintf("h%d", b.handleSeq)
	b.flashNode = node0 + 1
	b.flashPercent = 0
	b.flashStatus = "pending"
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(envelope(map[string]any{"handle": b.flashHandle}))
}

func (b *BMC) handleUpload(w http.ResponseWriter, r *http.Request) {
	if !b.authorized(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	handle := strings.TrimPrefix(r.URL.Path, "/api/bmc/upload/")
	if strings.HasSuffix(handle, "/cancel") {
		w.WriteHeader(http.StatusOK)
		return
	}
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if handle != b.flashHandle {
		http.Error(w, `{"error":"unknown handle"}`, http.StatusNotFound)
		return
	}
	sum := sha256.Sum256(data)
	node := b.flashNode
	if node < 1 || node > 4 {
		node = 1
	}
	b.nodes[node].FlashSHA = hex.EncodeToString(sum[:])
	b.nodes[node].FlashBytes = int64(len(data))
	b.nodes[node].Image = data
	if b.FlashError {
		b.flashStatus = "error"
		b.flashPercent = 0
	} else if b.FlashStuck {
		b.flashStatus = "writing"
		b.flashPercent = 50
	} else {
		b.flashStatus = "done"
		b.flashPercent = 100
	}
	w.WriteHeader(http.StatusOK)
}

func first(vals []string) string {
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

func (b *BMC) handleFlashGet(w http.ResponseWriter) {
	b.mu.Lock()
	defer b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(envelope(map[string]any{
		"percent": fmt.Sprintf("%d", b.flashPercent),
		"status":  b.flashStatus,
		"message": "",
	}))
}
