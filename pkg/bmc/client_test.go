/*
Copyright 2026 ferrix

SPDX-License-Identifier: MIT
*/

package bmc_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/ferrix/capturing/pkg/bmc"
	"github.com/ferrix/capturing/pkg/bmc/fake"
)

func TestProviderID(t *testing.T) {
	t.Parallel()
	got := bmc.ProviderID("https://10.88.0.11", 3)
	want := "turingpi://10.88.0.11/node/3"
	if got != want {
		t.Fatalf("ProviderID = %q, want %q", got, want)
	}
	got = bmc.ProviderID("not-a-url", 1)
	if got != "turingpi://not-a-url/node/1" {
		t.Fatalf("ProviderID fallback = %q", got)
	}
}

func TestPowerGetSet(t *testing.T) {
	t.Parallel()
	board := fake.New()
	defer board.Close()

	c := bmc.New(board.URL(), "root", "turing", bmc.WithPollInterval(time.Millisecond))
	ctx := context.Background()

	power, err := c.PowerGet(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 4; i++ {
		if power[i] {
			t.Fatalf("node %d started on", i)
		}
	}

	if err := c.PowerSet(ctx, 2, true); err != nil {
		t.Fatal(err)
	}
	if !board.Power(2) {
		t.Fatal("fake did not record power on")
	}
	power, err = c.PowerGet(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !power[2] || power[1] {
		t.Fatalf("power = %#v", power)
	}

	if err := c.PowerSet(ctx, 2, false); err != nil {
		t.Fatal(err)
	}
	if board.Power(2) {
		t.Fatal("expected off")
	}
}

func TestPowerSetInvalidNode(t *testing.T) {
	t.Parallel()
	c := bmc.New("http://127.0.0.1:1", "root", "turing")
	if err := c.PowerSet(context.Background(), 0, true); !errors.Is(err, bmc.ErrInvalidNode) {
		t.Fatalf("err = %v", err)
	}
	if err := c.PowerSet(context.Background(), 5, true); !errors.Is(err, bmc.ErrInvalidNode) {
		t.Fatalf("err = %v", err)
	}
}

func TestAuthFailure(t *testing.T) {
	t.Parallel()
	board := fake.New()
	defer board.Close()
	c := bmc.New(board.URL(), "root", "wrong")
	_, err := c.PowerGet(context.Background())
	if !errors.Is(err, bmc.ErrUnauthorized) {
		t.Fatalf("err = %v", err)
	}
}

func TestAuthFailFlag(t *testing.T) {
	t.Parallel()
	board := fake.New()
	board.FailAuth = true
	defer board.Close()
	c := bmc.New(board.URL(), "root", "turing")
	_, err := c.PowerGet(context.Background())
	if !errors.Is(err, bmc.ErrUnauthorized) {
		t.Fatalf("err = %v", err)
	}
}

func TestPowerGetFailure(t *testing.T) {
	t.Parallel()
	board := fake.New()
	board.FailPowerGet = true
	defer board.Close()
	c := bmc.New(board.URL(), "root", "turing")
	_, err := c.PowerGet(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPowerSetFailure(t *testing.T) {
	t.Parallel()
	board := fake.New()
	board.FailPowerSet = true
	defer board.Close()
	c := bmc.New(board.URL(), "root", "turing")
	if err := c.PowerSet(context.Background(), 1, true); err == nil {
		t.Fatal("expected error")
	}
}

func TestFlash(t *testing.T) {
	t.Parallel()
	board := fake.New()
	defer board.Close()
	c := bmc.New(board.URL(), "root", "turing", bmc.WithPollInterval(time.Millisecond))

	image := []byte("talos-installer-image")
	if err := c.Flash(context.Background(), 3, bytes.NewReader(image), int64(len(image))); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(image)
	if board.LastFlashSHA(3) != hex.EncodeToString(sum[:]) {
		t.Fatalf("sha = %s", board.LastFlashSHA(3))
	}
	if board.LastFlashBytes(3) != int64(len(image)) {
		t.Fatalf("bytes = %d", board.LastFlashBytes(3))
	}
	if board.FlashCalls() != 1 {
		t.Fatalf("flash calls = %d", board.FlashCalls())
	}
}

func TestFlashInvalidNode(t *testing.T) {
	t.Parallel()
	c := bmc.New("http://127.0.0.1:1", "a", "b")
	if err := c.Flash(context.Background(), 9, bytes.NewReader(nil), 0); !errors.Is(err, bmc.ErrInvalidNode) {
		t.Fatalf("err = %v", err)
	}
}

func TestFlashNegativeSize(t *testing.T) {
	t.Parallel()
	c := bmc.New("http://127.0.0.1:1", "a", "b")
	if err := c.Flash(context.Background(), 1, bytes.NewReader(nil), -1); err == nil {
		t.Fatal("expected size error")
	}
}

func TestFlashStartFailure(t *testing.T) {
	t.Parallel()
	board := fake.New()
	board.FailFlash = true
	defer board.Close()
	c := bmc.New(board.URL(), "root", "turing")
	if err := c.Flash(context.Background(), 1, bytes.NewReader([]byte("x")), 1); err == nil {
		t.Fatal("expected error")
	}
}

func TestFlashErrorStatus(t *testing.T) {
	t.Parallel()
	board := fake.New()
	board.FlashError = true
	defer board.Close()
	c := bmc.New(board.URL(), "root", "turing", bmc.WithPollInterval(time.Millisecond))
	err := c.Flash(context.Background(), 1, bytes.NewReader([]byte("x")), 1)
	if err == nil {
		t.Fatal("expected flash error")
	}
}

func TestFlashTimeout(t *testing.T) {
	t.Parallel()
	board := fake.New()
	board.FlashStuck = true
	defer board.Close()
	c := bmc.New(board.URL(), "root", "turing", bmc.WithPollInterval(time.Millisecond))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := c.Flash(ctx, 1, bytes.NewReader([]byte("x")), 1)
	if !errors.Is(err, bmc.ErrFlashTimeout) {
		t.Fatalf("err = %v", err)
	}
}

func TestTokenReuse(t *testing.T) {
	t.Parallel()
	board := fake.New()
	defer board.Close()
	c := bmc.New(board.URL(), "root", "turing")
	ctx := context.Background()
	if _, err := c.PowerGet(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.PowerGet(ctx); err != nil {
		t.Fatal(err)
	}
	if board.AuthCalls() != 1 {
		t.Fatalf("auth calls = %d, want 1", board.AuthCalls())
	}
}

func TestWithInsecureSkipVerify(t *testing.T) {
	t.Parallel()
	c := bmc.New("https://example.invalid", "root", "turing", bmc.WithInsecureSkipVerify())
	if c.HTTP == nil || c.HTTP.Transport == nil {
		t.Fatal("expected transport")
	}
	tr, ok := c.HTTP.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected *http.Transport")
	}
	if tr.ForceAttemptHTTP2 {
		t.Fatal("BMC client must not attempt HTTP/2")
	}
	if len(tr.TLSNextProto) != 0 {
		t.Fatal("TLSNextProto must be empty to disable HTTP/2")
	}
	if tr.TLSClientConfig == nil || !tr.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("expected InsecureSkipVerify")
	}
}

func TestUnreachable(t *testing.T) {
	t.Parallel()
	c := bmc.New("http://127.0.0.1:1", "root", "turing")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := c.PowerGet(ctx)
	if err == nil {
		t.Fatal("expected dial error")
	}
}
