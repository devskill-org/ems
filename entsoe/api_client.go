// Package entsoe provides a client for the ENTSO-E Transparency Platform API.
package entsoe

import (
	"context"
	"fmt"
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
func DownloadPublicationMarketData(ctx context.Context, securityToken string, urlFormat string, location *time.Location, fetchNextDay bool) (*PublicationMarketData, error) {

	now := time.Now().In(location)
	url := buildPublicationMarketDataURL(securityToken, urlFormat, now)
	fmt.Println(url)

	client := NewAPIClient()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	marketDocument, err := client.DownloadPublicationMarketData(ctx, url)
	if err != nil {
		return nil, err
	}

	// If instructed, also download data for the next day
	if fetchNextDay {
		tomorrow := now.AddDate(0, 0, 1)
		urlNextDay := buildPublicationMarketDataURL(securityToken, urlFormat, tomorrow)

		marketDocumentNextDay, err := client.DownloadPublicationMarketData(ctx, urlNextDay)
		if err != nil {
			return nil, err
		}

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
	if apiURL == "" {
		return nil, fmt.Errorf("API URL cannot be empty")
	}

	client := &http.Client{}

	// Create HTTP request with context
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set default headers
	userAgent := "entsoe-go-client/1.0"
	if opts.UserAgent != "" {
		userAgent = opts.UserAgent
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/xml, text/xml")

	// Set custom headers
	for key, value := range opts.Headers {
		req.Header.Set(key, value)
	}

	// Execute the request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute HTTP request: %w", err)
	}
	defer resp.Body.Close()

	// Check HTTP status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP request failed with status %d: %s", resp.StatusCode, resp.Status)
	}

	// Decode the XML response using the existing decoder
	doc, err := DecodeEnergyPricesXML(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to decode XML response: %w", err)
	}

	return doc, nil
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
