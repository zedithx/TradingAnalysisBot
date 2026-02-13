package yahoo

import (
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"time"
)

// client manages Yahoo Finance HTTP requests with cookie/crumb authentication.
// The quoteSummary endpoint (used for earnings) requires a valid crumb+cookie pair.
// The v8/finance/chart endpoint (used for validation) works without auth.
type client struct {
	mu         sync.Mutex
	httpClient *http.Client
	crumb      string
	lastAuth   time.Time
	userAgent  string
}

var (
	// defaultClient is the singleton Yahoo Finance client.
	defaultClient *client
	clientOnce    sync.Once
)

// getClient returns the singleton Yahoo Finance client, initialising it on first call.
func getClient() *client {
	clientOnce.Do(func() {
		jar, _ := cookiejar.New(nil)
		defaultClient = &client{
			httpClient: &http.Client{
				Timeout: 15 * time.Second,
				Jar:     jar,
			},
			userAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		}
	})
	return defaultClient
}

// authenticate fetches a fresh cookie and crumb from Yahoo Finance.
// Crumbs are cached and re-used for up to 30 minutes.
func (c *client) authenticate() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Re-use existing crumb if still fresh
	if c.crumb != "" && time.Since(c.lastAuth) < 30*time.Minute {
		return nil
	}

	// Step 1: Hit fc.yahoo.com to get session cookies
	req, err := http.NewRequest("GET", "https://fc.yahoo.com", nil)
	if err != nil {
		return fmt.Errorf("build cookie request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch cookies: %w", err)
	}
	resp.Body.Close()
	// 404 is expected here — we just need the cookies

	// Step 2: Get crumb using the cookies
	req, err = http.NewRequest("GET", "https://query2.finance.yahoo.com/v1/test/getcrumb", nil)
	if err != nil {
		return fmt.Errorf("build crumb request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err = c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch crumb: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read crumb response: %w", err)
	}

	crumb := strings.TrimSpace(string(body))
	if crumb == "" || resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to get crumb (HTTP %d)", resp.StatusCode)
	}

	c.crumb = crumb
	c.lastAuth = time.Now()
	return nil
}

// doRequest performs an authenticated GET request to the given URL.
// It automatically injects the crumb as a query parameter.
func (c *client) doAuthenticatedGet(baseURL string) ([]byte, error) {
	if err := c.authenticate(); err != nil {
		return nil, fmt.Errorf("auth failed: %w", err)
	}

	// Append crumb to URL
	sep := "?"
	if strings.Contains(baseURL, "?") {
		sep = "&"
	}
	fullURL := baseURL + sep + "crumb=" + c.crumb

	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		// Crumb may be stale — force re-auth on next call
		c.mu.Lock()
		c.crumb = ""
		c.mu.Unlock()
		return nil, fmt.Errorf("authentication expired (HTTP %d), please retry", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// doSimpleGet performs an unauthenticated GET request (for endpoints that don't need crumb).
func (c *client) doSimpleGet(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}
