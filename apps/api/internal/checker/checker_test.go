package checker

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProbe_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(2 * time.Second)
	result := c.Probe(context.Background(), srv.URL)

	if !result.Success {
		t.Fatalf("Success = false, want true (errorKind=%q)", result.ErrorKind)
	}
	if result.HTTPStatus != http.StatusOK {
		t.Errorf("HTTPStatus = %d, want 200", result.HTTPStatus)
	}
}

func TestProbe_SuccessWithCommit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"Healthy","version":"1.0.0","commit":"25c92cadf3b2f0820dff3f7ba99eaa6cc1d37eda"}`))
	}))
	defer srv.Close()

	c := New(2 * time.Second)
	result := c.Probe(context.Background(), srv.URL)

	if result.Commit != "25c92cadf3b2f0820dff3f7ba99eaa6cc1d37eda" {
		t.Errorf("Commit = %q, want the commit field from the body", result.Commit)
	}
}

func TestProbe_SuccessWithoutCommit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer srv.Close()

	c := New(2 * time.Second)
	result := c.Probe(context.Background(), srv.URL)

	if !result.Success {
		t.Fatalf("Success = false, want true")
	}
	if result.Commit != "" {
		t.Errorf("Commit = %q, want empty for a non-JSON body", result.Commit)
	}
}

func TestProbe_HTTPStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := New(2 * time.Second)
	result := c.Probe(context.Background(), srv.URL)

	if result.Success {
		t.Fatalf("Success = true, want false")
	}
	if result.ErrorKind != "http_status" {
		t.Errorf("ErrorKind = %q, want http_status", result.ErrorKind)
	}
	if result.HTTPStatus != http.StatusServiceUnavailable {
		t.Errorf("HTTPStatus = %d, want 503", result.HTTPStatus)
	}
}

func TestProbe_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(3 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	c := New(300 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	start := time.Now()
	result := c.Probe(ctx, srv.URL)
	elapsed := time.Since(start)

	if result.ErrorKind != "timeout" {
		t.Fatalf("ErrorKind = %q, want timeout", result.ErrorKind)
	}
	if elapsed > 2*time.Second {
		t.Errorf("Probe took %s, want well under 2s (client timeout should abort the 3s handler)", elapsed)
	}
}

func TestProbe_ConnectionRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close() // nothing listens here anymore; dialing it must be refused

	c := New(2 * time.Second)
	result := c.Probe(context.Background(), "http://"+addr+"/")

	if result.ErrorKind != "connection_refused" {
		t.Fatalf("ErrorKind = %q, want connection_refused", result.ErrorKind)
	}
}

func TestProbe_BlockedMetadataEndpoint(t *testing.T) {
	c := New(2 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result := c.Probe(ctx, "http://169.254.169.254/")

	if result.ErrorKind != "blocked" {
		t.Fatalf("ErrorKind = %q, want blocked", result.ErrorKind)
	}
	if result.Success {
		t.Fatalf("Success = true, want false")
	}
}

func TestProbe_BlockedUnspecifiedAddress(t *testing.T) {
	c := New(2 * time.Second)
	result := c.Probe(context.Background(), "http://0.0.0.0:1/")

	if result.ErrorKind != "blocked" {
		t.Fatalf("ErrorKind = %q, want blocked", result.ErrorKind)
	}
}
