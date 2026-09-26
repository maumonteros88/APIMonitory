// Package checker performs the actual HTTP health probes against monitored
// URLs, guarding against SSRF toward cloud metadata endpoints and other
// disallowed destinations.
package checker

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"

	"apimonitory/internal/monitor"
)

// errBlocked is returned by the dial guard when a connection targets a
// disallowed address (cloud metadata endpoints, link-local, unspecified).
var errBlocked = errors.New("checker: destination address is blocked")

// ec2MetadataAddr is the IPv6 metadata endpoint used by some cloud providers.
var ec2MetadataAddr = netip.MustParseAddr("fd00:ec2::254")

// Checker performs HTTP GET probes and classifies their outcome. It reuses
// one http.Client (and its connection pool) across calls to Probe.
type Checker struct {
	client *http.Client
}

// New builds a Checker whose probes time out after timeout.
func New(timeout time.Duration) *Checker {
	dialer := &net.Dialer{
		Timeout: timeout,
		Control: guardControl,
	}
	transport := &http.Transport{
		DialContext: dialer.DialContext,
	}
	return &Checker{
		client: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
	}
}

// Probe issues a GET request against target and classifies the result.
// It never logs or returns the response body, only status code and latency.
func (c *Checker) Probe(ctx context.Context, target string) monitor.ProbeResult {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return monitor.ProbeResult{ErrorKind: "other"}
	}

	start := time.Now()
	resp, err := c.client.Do(req)
	latencyMs := int(time.Since(start).Milliseconds())

	if err != nil {
		return monitor.ProbeResult{
			LatencyMs: latencyMs,
			ErrorKind: classifyError(ctx, err),
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return monitor.ProbeResult{
			Success:    true,
			HTTPStatus: resp.StatusCode,
			LatencyMs:  latencyMs,
		}
	}
	return monitor.ProbeResult{
		HTTPStatus: resp.StatusCode,
		LatencyMs:  latencyMs,
		ErrorKind:  "http_status",
	}
}

// classifyError maps a request error to one of the documented ErrorKind
// values. Order matters: the SSRF guard and DNS failures are unambiguous and
// checked first, then timeouts, then connection refusal, then TLS errors.
func classifyError(ctx context.Context, err error) string {
	if errors.Is(err, errBlocked) {
		return "blocked"
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "dns"
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "timeout"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}

	if errors.Is(err, syscall.ECONNREFUSED) {
		return "connection_refused"
	}

	if isTLSError(err) {
		return "tls"
	}

	return "other"
}

func isTLSError(err error) bool {
	var certVerifyErr *tls.CertificateVerificationError
	if errors.As(err, &certVerifyErr) {
		return true
	}
	var recordHeaderErr tls.RecordHeaderError
	if errors.As(err, &recordHeaderErr) {
		return true
	}
	var certInvalidErr x509.CertificateInvalidError
	if errors.As(err, &certInvalidErr) {
		return true
	}
	var unknownAuthorityErr x509.UnknownAuthorityError
	if errors.As(err, &unknownAuthorityErr) {
		return true
	}
	var hostnameErr x509.HostnameError
	if errors.As(err, &hostnameErr) {
		return true
	}
	return false
}

// guardControl is a net.Dialer.Control hook: it runs after DNS resolution,
// on the concrete address about to be dialed, so it also covers redirects
// and DNS-rebinding attempts (a hostname re-resolving to a blocked address
// on a later dial).
func guardControl(_ string, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return err
	}
	addr = addr.Unmap()

	if addr.IsLinkLocalUnicast() || addr.IsUnspecified() {
		return errBlocked
	}
	if addr == ec2MetadataAddr {
		return errBlocked
	}
	return nil
}
