package site_icons

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestParseHeadDetachesAtHeadEnd verifies the deadlock fix: parseHead must
// deliver its result once </head> is seen, without waiting for the rest of
// the body stream. (Before the fix, a wg.Wait() inside the tokenizer loop
// back-pressured the shared body reader and could hang the whole target.)
func TestParseHeadDetachesAtHeadEnd(t *testing.T) {
	head := `<html><head><link rel="icon" href="/favicon.ico"></head>`
	body := strings.NewReader(head + strings.Repeat("x", 1<<20))

	pr, pw := io.Pipe()
	go func() {
		if _, err := io.Copy(pw, body); err != nil {
			pw.CloseWithError(err)
		} else {
			pw.Close()
		}
	}()

	pageURL, _ := url.Parse("https://example.com/")

	done := make(chan struct{})
	var got int
	go func() {
		defer close(done)
		for range parseHead(pageURL, pr) {
			got++
		}
	}()

	select {
	case <-done:
		// Parse finished although the pipe writer had not sent EOF yet —
		// or drained it promptly after </head>.
	case <-time.After(2 * time.Second):
		t.Fatal("parseHead did not return after </head> while body kept streaming")
	}
	_ = got
}

// TestLoadWebsiteBoundedOnSlowIcon verifies per-target bounding: a page whose
// icon URL never responds must not hang LoadWebsite forever. Every request is
// capped by the client timeout, so the call returns with what it has.
func TestLoadWebsiteBoundedOnSlowIcon(t *testing.T) {
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".ico") || strings.HasSuffix(r.URL.Path, "manifest") {
			// Hang until the test ends — simulates the stuck hosts that
			// caused the 602s subprocess timeouts in production.
			<-release
			return
		}
		fmt.Fprintf(w, `<html><head><link rel="icon" href="/slow.ico"></head><body><img src="/logo.png"></body></html>`)
	}))
	// LIFO: release handlers first, then wait for server close.
	defer slow.Close()
	defer close(release)

	s := NewSiteIcons()
	timeoutCh := time.After(30 * time.Second) // generous; client timeout is 12s
	resCh := make(chan struct{})
	go func() {
		s.LoadWebsite(slow.URL, false)
		close(resCh)
	}()

	select {
	case <-resCh:
	case <-timeoutCh:
		t.Fatal("LoadWebsite hung on a never-responding icon host")
	}
}

// TestIsDialError verifies connection-level error classification used to
// short-circuit remaining same-host candidate URLs.
func TestIsDialError(t *testing.T) {
	dnsErr := &net.DNSError{Err: "no such host", Name: "gone.example"}
	if !isDialError(fmt.Errorf("wrapped: %w", dnsErr)) {
		t.Error("DNS error should classify as dial error")
	}
	if !isDialError(fmt.Errorf("fetch failed: %w", errFakeTimeout{})) {
		t.Error("timeout net.Error should classify as dial error")
	}
	if isDialError(fmt.Errorf("failed to fetch icon: HTTP 404")) {
		t.Error("HTTP-level error must not classify as dial error")
	}
	if isDialError(nil) {
		t.Error("nil must not classify as dial error")
	}
}

type errFakeTimeout struct{}

func (errFakeTimeout) Error() string   { return "fake timeout" }
func (errFakeTimeout) Timeout() bool   { return true }
func (errFakeTimeout) Temporary() bool { return true }
