package asmrone

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const baseURL = "https://api.asmr.one"

// Client is an HTTP client for the asmr.one API.
type Client struct {
	token      string
	httpClient *http.Client
}

// NewClient creates a Client authenticated with the given JWT token.
func NewClient(jwtToken string) *Client {
	return &Client{
		token:      jwtToken,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// normalizeID strips the "RJ" prefix and returns just the numeric portion.
func normalizeID(workno string) string {
	return strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(workno)), "RJ")
}

func (c *Client) newRequest(ctx context.Context, method, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return req, nil
}

func (c *Client) do(req *http.Request, dst any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return json.NewDecoder(resp.Body).Decode(dst)
	case http.StatusUnauthorized:
		return fmt.Errorf("unauthorized – please run /asmr_bind with a valid token")
	case http.StatusNotFound:
		return fmt.Errorf("work not found on asmr.one")
	default:
		return fmt.Errorf("asmr.one API returned HTTP %d", resp.StatusCode)
	}
}

// GetWorkInfo fetches metadata for the given RJ work (no auth required).
func (c *Client) GetWorkInfo(ctx context.Context, workno string) (*WorkInfo, error) {
	id := normalizeID(workno)
	url := fmt.Sprintf("%s/api/workInfo/%s", baseURL, id)
	req, err := c.newRequest(ctx, http.MethodGet, url)
	if err != nil {
		return nil, err
	}
	var info WorkInfo
	if err := c.do(req, &info); err != nil {
		return nil, fmt.Errorf("get work info: %w", err)
	}
	return &info, nil
}

// GetTracks fetches the full file tree for the given RJ work. Requires a valid token.
func (c *Client) GetTracks(ctx context.Context, workno string) ([]Track, error) {
	id := normalizeID(workno)
	url := fmt.Sprintf("%s/api/tracks/%s", baseURL, id)
	req, err := c.newRequest(ctx, http.MethodGet, url)
	if err != nil {
		return nil, err
	}
	var tracks []Track
	if err := c.do(req, &tracks); err != nil {
		return nil, fmt.Errorf("get tracks: %w", err)
	}
	return tracks, nil
}
