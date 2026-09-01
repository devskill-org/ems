// Package entsoe provides a client for the ENTSO-E Transparency Platform API.
package entsoe

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/devskill-org/ems/utils"
)

// APIClient represents an HTTP client for the ENTSO-E API
type APIClient struct {
	httpClient *http.Client
	userAgent  string
}

// NewAPIClient creates a new ENTSO-E API client with default settings
func NewAPIClient() *APIClient {
	return &APIClient{
		httpClient: &http.Client{},
		userAgent:  "entsoe-go-client/1.0",
	}
}

// SetUserAgent sets a custom user agent for the API client
func (c *APIClient) SetUserAgent(userAgent string) {
	c.userAgent = userAgent
}

// DownloadPublicationMarketData downloads and decodes a PublicationMarketData from the given API URL
func (c *APIClient) DownloadPublicationMarketData(ctx context.Context, apiURL string) (*PublicationMarketData, error) {
	opts := &DownloadOptions{
		UserAgent: c.userAgent,
	}

	return DownloadPublicationMarketDataWithOptions(ctx, apiURL, opts)
}

// DownloadOptions contains options for downloading publication market data with additional options.
type DownloadOptions struct {
	UserAgent string
	Headers   map[string]string
}

// DownloadPublicationMarketData downloads and decodes publication market data for the current and next day if needed.
// fetchNextDay indicates whether to also download data for the next day (e.g. when it is past 13:30).
// cache is an optional XMLDocumentCache; when non-nil, cached entries are returned instead of fetching from the network.
func DownloadPublicationMarketData(ctx context.Context, securityToken string, urlFormat string, location *time.Location, fetchNextDay bool, cache *XMLDocumentCache) (*PublicationMarketData, error) {

	now := time.Now().In(location)
	client := NewAPIClient()
	opts := &DownloadOptions{
		UserAgent: client.userAgent,
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Retrieve today's market data – use cache when available.
	todayKey := now.Format("2006-01-02")
	var marketDocument *PublicationMarketData
	if cache != nil {
		if cached, ok := cache.Get(todayKey); ok {
			fmt.Printf("Using cached market data for %s\n", todayKey)
			marketDocument = cached
		}
	}

	if marketDocument == nil {
		url := buildPublicationMarketDataURL(securityToken, urlFormat, now)
		fmt.Println(url)
		var err error
		var rawXML []byte
		marketDocument, rawXML, err = DownloadPublicationMarketDataWithRaw(ctx, url, opts)
		if err != nil {
			return nil, err
		}
		if cache != nil {
			cache.StoreDocumentWithRaw(todayKey, marketDocument, rawXML, CacheSourceDownload)
		}
	}

	// Retrieve data for the next day – use cache when available, or fetch if fetchNextDay is true.
	tomorrow := now.AddDate(0, 0, 1)
	tomorrowKey := tomorrow.Format("2006-01-02")

	var marketDocumentNextDay *PublicationMarketData
	if cache != nil {
		if cached, ok := cache.Get(tomorrowKey); ok {
			fmt.Printf("Using cached market data for %s\n", tomorrowKey)
			marketDocumentNextDay = cached
		}
	}

	if marketDocumentNextDay == nil && fetchNextDay {
		urlNextDay := buildPublicationMarketDataURL(securityToken, urlFormat, tomorrow)
		var err error
		var rawXMLNextDay []byte
		marketDocumentNextDay, rawXMLNextDay, err = DownloadPublicationMarketDataWithRaw(ctx, urlNextDay, opts)
		if err != nil {
			return nil, err
		}
		if cache != nil {
			cache.StoreDocumentWithRaw(tomorrowKey, marketDocumentNextDay, rawXMLNextDay, CacheSourceDownload)
		}
	}

	if marketDocumentNextDay != nil {
		// Merge the data from both days
		marketDocument = mergePublicationMarketData(marketDocument, marketDocumentNextDay)
	}

	return marketDocument, nil
}

// buildPublicationMarketDataURL extracts the URL assignment logic for DownloadPublicationMarketData.
func buildPublicationMarketDataURL(securityToken string, urlFormat string, now time.Time) string {
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	periodStart := utils.GetUTCString(start)
	periodEnd := utils.GetUTCString(start.AddDate(0, 0, 1))

	return fmt.Sprintf(urlFormat, periodStart, periodEnd, securityToken)
}

// mergePublicationMarketData merges two PublicationMarketData objects by combining their TimeSeries
func mergePublicationMarketData(first *PublicationMarketData, second *PublicationMarketData) *PublicationMarketData {
	if first == nil {
		return second
	}
	if second == nil {
		return first
	}

	// Create a copy of the first document
	merged := *first

	// Append all TimeSeries from the second document
	merged.TimeSeries = append(merged.TimeSeries, second.TimeSeries...)

	// Update the period time interval to span both documents
	if len(second.TimeSeries) > 0 && second.PeriodTimeInterval.End.After(merged.PeriodTimeInterval.End) {
		merged.PeriodTimeInterval.End = second.PeriodTimeInterval.End
	}

	return &merged
}

// DownloadPublicationMarketDataWithOptions downloads and decodes a PublicationMarketData with custom options
func DownloadPublicationMarketDataWithOptions(ctx context.Context, apiURL string, opts *DownloadOptions) (*PublicationMarketData, error) {
	doc, _, err := DownloadPublicationMarketDataWithRaw(ctx, apiURL, opts)
	return doc, err
}

// DownloadPublicationMarketDataWithRaw downloads and decodes a PublicationMarketData along with its raw XML bytes
func DownloadPublicationMarketDataWithRaw(ctx context.Context, apiURL string, opts *DownloadOptions) (*PublicationMarketData, []byte, error) {
	if apiURL == "" {
		return nil, nil, fmt.Errorf("API URL cannot be empty")
	}

	client := &http.Client{}

	// Create HTTP request with context
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set default headers
	userAgent := "entsoe-go-client/1.0"
	if opts != nil && opts.UserAgent != "" {
		userAgent = opts.UserAgent
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/xml, text/xml")

	// Set custom headers
	if opts != nil {
		for key, value := range opts.Headers {
			req.Header.Set(key, value)
		}
	}

	// Execute the request
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute HTTP request: %w", err)
	}
	defer resp.Body.Close()

	// Check HTTP status code
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("HTTP request failed with status %d: %s", resp.StatusCode, resp.Status)
	}

	rawBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Decode the XML response using the existing decoder
	doc, err := DecodeEnergyPricesXML(bytes.NewReader(rawBytes))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode XML response: %w", err)
	}

	return doc, rawBytes, nil
}

// ValidateAPIURL performs basic validation on the API URL
func ValidateAPIURL(apiURL string) error {
	if apiURL == "" {
		return fmt.Errorf("API URL cannot be empty")
	}

	// Basic URL validation - in production you might want more sophisticated validation
	if len(apiURL) < 7 { // minimum: http://
		return fmt.Errorf("API URL appears to be too short")
	}

	if apiURL[:7] != "http://" && apiURL[:8] != "https://" {
		return fmt.Errorf("API URL must start with http:// or https://")
	}

	return nil
}
