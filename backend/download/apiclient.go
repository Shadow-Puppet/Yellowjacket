package download

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Every remote provider here talks to a self-hosted service over the
// same shape of API: a base URL, one header carrying an API key, JSON
// in and out, and a handful of status codes that mean the same thing
// everywhere.  This is that, once.
//
// Each adapter supplies its own sentinel errors so callers can still
// distinguish "slskd is down" from "Prowlarr is down" with errors.Is.

// apiClient is a small JSON-over-HTTP client for a self-hosted service.
type apiClient struct {
	http *http.Client

	baseURL   string
	authKey   string
	authValue string

	// errUnreachable and errAuth are the adapter's sentinels, returned
	// for transport failures and rejected credentials respectively.
	errUnreachable error
	errAuth        error
}

// newAPIClient builds a client for one service.
func newAPIClient(
	baseURL, authHeader, authValue string,
	timeout time.Duration,
	errUnreachable, errAuth error,
) *apiClient {
	return &apiClient{
		http:           &http.Client{Timeout: timeout},
		baseURL:        strings.TrimRight(baseURL, "/"),
		authKey:        authHeader,
		authValue:      authValue,
		errUnreachable: errUnreachable,
		errAuth:        errAuth,
	}
}

// get performs a GET, decoding the response into out when non-nil.
func (c *apiClient) get(ctx context.Context, endpoint string, out any) error {
	return c.do(ctx, http.MethodGet, endpoint, nil, out)
}

// post performs a POST with a JSON body.
func (c *apiClient) post(
	ctx context.Context,
	endpoint string,
	body, out any,
) error {
	return c.do(ctx, http.MethodPost, endpoint, body, out)
}

// put performs a PUT with a JSON body.
func (c *apiClient) put(
	ctx context.Context,
	endpoint string,
	body, out any,
) error {
	return c.do(ctx, http.MethodPut, endpoint, body, out)
}

// delete performs a DELETE.
func (c *apiClient) delete(ctx context.Context, endpoint string) error {
	return c.do(ctx, http.MethodDelete, endpoint, nil, nil)
}

// do performs one request.
func (c *apiClient) do(
	ctx context.Context,
	method, endpoint string,
	body, out any,
) error {
	var reader io.Reader

	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request body: %w", err)
		}

		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(
		ctx, method, c.baseURL+endpoint, reader,
	)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	if c.authKey != "" {
		req.Header.Set(c.authKey, c.authValue)
	}

	req.Header.Set("Accept", "application/json")

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", c.errUnreachable, err)
	}

	defer func() { _ = resp.Body.Close() }()

	if err := c.checkStatus(resp); err != nil {
		return err
	}

	if out == nil {
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}

// checkStatus maps HTTP status onto the adapter's sentinel errors,
// including a snippet of the body — self-hosted services put the useful
// part of a failure there, not in the status line.
func (c *apiClient) checkStatus(resp *http.Response) error {
	switch {
	case resp.StatusCode == http.StatusUnauthorized,
		resp.StatusCode == http.StatusForbidden:
		return c.errAuth
	case resp.StatusCode >= 400:
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

		return fmt.Errorf(
			"%w: HTTP %d: %s",
			c.errUnreachable,
			resp.StatusCode,
			strings.TrimSpace(string(snippet)),
		)
	default:
		return nil
	}
}

// decodeJSON decodes a JSON string into out.  Providers whose auth or
// response handling does not fit apiClient still parse bodies the same
// way, so the helper lives here rather than being repeated.
func decodeJSON(body string, out any) error {
	if err := json.Unmarshal([]byte(body), out); err != nil {
		return fmt.Errorf("decode json: %w", err)
	}

	return nil
}
