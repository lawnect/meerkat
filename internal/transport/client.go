package transport

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
)

type HTTPError struct{ Status int }

func (e *HTTPError) Error() string { return fmt.Sprintf("outlet returned HTTP %d", e.Status) }

type Client struct {
	origin string
	http   *http.Client
}

func New(origin, rootCA string) (*Client, error) {
	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = x509.NewCertPool()
	}
	if rootCA != "" {
		pem, err := os.ReadFile(rootCA)
		if err != nil {
			return nil, errors.New("cannot read outlet root CA")
		}
		if !roots.AppendCertsFromPEM(pem) {
			return nil, errors.New("invalid outlet root CA")
		}
	}
	t := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}, Proxy: nil}
	return &Client{strings.TrimRight(origin, "/"), &http.Client{Transport: t, Timeout: 8 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *Client) Close() { c.http.CloseIdleConnections() }

func (c *Client) request(ctx context.Context, method, path, token string, body, result any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.origin+path, reader)
	if err != nil {
		return errors.New("invalid API request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return errors.New("outlet request failed")
	} // redact URL and transport payloads
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &HTTPError{res.StatusCode}
	}
	if result == nil {
		_, err := io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
		return err
	}
	d := json.NewDecoder(io.LimitReader(res.Body, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(result); err != nil {
		return errors.New("invalid outlet response")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("trailing outlet response data")
	}
	return nil
}

func (c *Client) Enroll(ctx context.Context, token string, req contract.EnrollmentRequest) (contract.EnrollmentResponse, error) {
	var response contract.EnrollmentResponse
	err := c.request(ctx, "POST", "/connector/v1/enrollment", token, req, &response)
	return response, err
}

func (c *Client) Configuration(ctx context.Context, credential string) (contract.Configuration, error) {
	var response contract.Configuration
	err := c.request(ctx, "GET", "/connector/v1/config", credential, nil, &response)
	return response, err
}

func (c *Client) Heartbeat(ctx context.Context, credential string, hb contract.Heartbeat) error {
	return c.request(ctx, "POST", "/connector/v1/heartbeat", credential, hb, nil)
}
