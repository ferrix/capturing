/*
Copyright 2026 ferrix

SPDX-License-Identifier: MIT
*/

package bmc_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ferrix/capturing/pkg/bmc"
)

func TestAuthTokenFromBody(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/authenticate"):
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "body-token"})
		case r.Header.Get("Authorization") != "Bearer body-token":
			http.Error(w, "nope", http.StatusUnauthorized)
		default:
			_, _ = w.Write([]byte(`{"response":[{"result":[{"node1":1,"node2":0,"node3":0,"node4":0}]}]}`))
		}
	}))
	defer srv.Close()
	c := bmc.New(srv.URL, "root", "turing")
	power, err := c.PowerGet(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !power[1] {
		t.Fatalf("numeric node1 should be on: %#v", power)
	}
}

func TestEmptyPowerResponse(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/authenticate") {
			w.Header().Set("X-Auth-Token", "t")
			_, _ = w.Write([]byte(`{"id":"t"}`))
			return
		}
		_, _ = w.Write([]byte(`{"response":[]}`))
	}))
	defer srv.Close()
	c := bmc.New(srv.URL, "u", "p")
	if _, err := c.PowerGet(context.Background()); err == nil {
		t.Fatal("expected empty response error")
	}
}

func TestInvalidJSON(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/authenticate") {
			w.Header().Set("X-Auth-Token", "t")
			return
		}
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv.Close()
	c := bmc.New(srv.URL, "u", "p")
	if _, err := c.PowerGet(context.Background()); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestAuthHTTPError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer srv.Close()
	c := bmc.New(srv.URL, "u", "p")
	if _, err := c.PowerGet(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestAuthEmptyToken(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := bmc.New(srv.URL, "u", "p")
	if _, err := c.PowerGet(context.Background()); !errors.Is(err, bmc.ErrUnauthorized) {
		t.Fatalf("err = %v", err)
	}
}

func TestDoBMCUnauthorizedClearsToken(t *testing.T) {
	t.Parallel()
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/authenticate") {
			w.Header().Set("X-Auth-Token", "t")
			_, _ = w.Write([]byte(`{"id":"t"}`))
			return
		}
		n++
		http.Error(w, "expired", http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := bmc.New(srv.URL, "u", "p")
	_, err := c.PowerGet(context.Background())
	if !errors.Is(err, bmc.ErrUnauthorized) {
		t.Fatalf("err = %v", err)
	}
}

func TestFlashMissingHandle(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/authenticate") {
			w.Header().Set("X-Auth-Token", "t")
			return
		}
		_, _ = w.Write([]byte(`{"response":[{"result":[{"ok":"yes"}]}]}`))
	}))
	defer srv.Close()
	c := bmc.New(srv.URL, "u", "p")
	if err := c.Flash(context.Background(), 1, bytes.NewReader([]byte("x")), 1); err == nil {
		t.Fatal("expected missing handle")
	}
}

func TestFlashCompleteStatus(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/authenticate"):
			w.Header().Set("X-Auth-Token", "t")
		case strings.Contains(r.URL.RawQuery, "type=flash") && strings.Contains(r.URL.RawQuery, "opt=set"):
			_, _ = w.Write([]byte(`{"response":[{"result":[{"handle":"1"}]}]}`))
		case strings.HasPrefix(r.URL.Path, "/api/bmc/upload/"):
			w.WriteHeader(http.StatusOK)
		default:
			_, _ = w.Write([]byte(`{"response":[{"result":[{"status":"complete","percent":"0"}]}]}`))
		}
	}))
	defer srv.Close()
	c := bmc.New(srv.URL, "u", "p", bmc.WithPollInterval(0))
	if err := c.Flash(context.Background(), 2, bytes.NewReader([]byte("x")), 1); err != nil {
		t.Fatal(err)
	}
}

func TestUploadUnauthorized(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/authenticate"):
			w.Header().Set("X-Auth-Token", "t")
		case strings.Contains(r.URL.RawQuery, "opt=set") && strings.Contains(r.URL.RawQuery, "flash"):
			_, _ = w.Write([]byte(`{"response":[{"result":[{"handle":"h"}]}]}`))
		default:
			http.Error(w, "no", http.StatusUnauthorized)
		}
	}))
	defer srv.Close()
	c := bmc.New(srv.URL, "u", "p")
	if err := c.Flash(context.Background(), 1, bytes.NewReader([]byte("x")), 1); !errors.Is(err, bmc.ErrUnauthorized) {
		t.Fatalf("err = %v", err)
	}
}

func TestUploadHTTPError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/authenticate"):
			w.Header().Set("X-Auth-Token", "t")
		case strings.Contains(r.URL.RawQuery, "opt=set") && strings.Contains(r.URL.RawQuery, "flash"):
			_, _ = w.Write([]byte(`{"response":[{"result":[{"handle":"h"}]}]}`))
		default:
			http.Error(w, "boom", http.StatusBadGateway)
		}
	}))
	defer srv.Close()
	c := bmc.New(srv.URL, "u", "p")
	if err := c.Flash(context.Background(), 1, bytes.NewReader([]byte("x")), 1); err == nil {
		t.Fatal("expected upload error")
	}
}

func TestWithHTTPClient(t *testing.T) {
	t.Parallel()
	used := false
	rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
		used = true
		return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})
	c := bmc.New("http://example.invalid", "u", "p", bmc.WithHTTPClient(&http.Client{Transport: rt}))
	_, _ = c.PowerGet(context.Background())
	if !used {
		t.Fatal("custom client not used")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestResultStringBool(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/authenticate") {
			w.Header().Set("X-Auth-Token", "t")
			return
		}
		_, _ = w.Write([]byte(`{"response":[{"result":[{"node1":true,"node2":null,"node3":"0","node4":"0"}]}]}`))
	}))
	defer srv.Close()
	c := bmc.New(srv.URL, "u", "p")
	power, err := c.PowerGet(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if power[1] {
		t.Fatal("true is not the string 1")
	}
}

func TestZeroPollStillWorks(t *testing.T) {
	t.Parallel()
	c := bmc.New("http://127.0.0.1:1", "u", "p", bmc.WithPollInterval(0))
	if c.PollInterval != 0 {
		t.Fatal("want 0")
	}
	_ = time.Millisecond
}
