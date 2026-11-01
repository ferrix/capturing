/*
Copyright 2026 ferrix

SPDX-License-Identifier: MIT
*/

package fake_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ferrix/capturing/pkg/bmc/fake"
)

func TestUnknownPath(t *testing.T) {
	t.Parallel()
	board := fake.New()
	defer board.Close()
	resp, err := http.Get(board.URL() + "/nope")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestUnauthorizedBMC(t *testing.T) {
	t.Parallel()
	board := fake.New()
	defer board.Close()
	resp, err := http.Get(board.URL() + "/api/bmc?opt=get&type=power")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestSetPowerDirect(t *testing.T) {
	t.Parallel()
	board := fake.New()
	defer board.Close()
	board.SetPower(4, true)
	if !board.Power(4) {
		t.Fatal("expected on")
	}
	if board.Power(0) || board.Power(5) {
		t.Fatal("invalid slots must be off")
	}
	if board.LastFlashSHA(0) != "" || board.LastFlashBytes(9) != 0 {
		t.Fatal("invalid slot flash")
	}
}

func TestWithCredentials(t *testing.T) {
	t.Parallel()
	board := fake.New(fake.WithCredentials("admin", "secret"))
	defer board.Close()
	resp, err := http.Post(board.URL()+"/api/bmc/authenticate", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("empty body should fail auth, status=%d", resp.StatusCode)
	}
}

func TestUploadCancelAndUnknownHandle(t *testing.T) {
	t.Parallel()
	board := fake.New()
	defer board.Close()
	req, _ := http.NewRequest(http.MethodPost, board.URL()+"/api/bmc/upload/nope/cancel", nil)
	req.Header.Set("Authorization", "Bearer test-token-capturing")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel status = %d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodPost, board.URL()+"/api/bmc/upload/unknown", strings.NewReader("img"))
	req.Header.Set("Authorization", "Bearer test-token-capturing")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown handle status = %d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodPost, board.URL()+"/api/bmc/upload/x", strings.NewReader("img"))
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauth upload status = %d", resp.StatusCode)
	}
}

func TestPowerSetAltQueryAndFlashInvalidNode(t *testing.T) {
	t.Parallel()
	board := fake.New()
	defer board.Close()
	req, _ := http.NewRequest(http.MethodGet, board.URL()+"/api/bmc?opt=power&type=set&node2=1", nil)
	req.Header.Set("Authorization", "Bearer test-token-capturing")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !board.Power(2) {
		t.Fatal("alt power set")
	}

	req, _ = http.NewRequest(http.MethodGet, board.URL()+"/api/bmc?opt=set&type=flash&node=9&length=1", nil)
	req.Header.Set("Authorization", "Bearer test-token-capturing")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid flash node status = %d", resp.StatusCode)
	}
}

func TestUnknownBMCQuery(t *testing.T) {
	t.Parallel()
	board := fake.New()
	defer board.Close()
	req, _ := http.NewRequest(http.MethodGet, board.URL()+"/api/bmc?opt=get&type=uart", nil)
	req.Header.Set("Authorization", "Bearer test-token-capturing")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}
