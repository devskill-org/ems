package openmeteo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	defaultBaseURL      = "https://api.open-meteo.com/v1/forecast"
	defaultTimeout      = 30 * time.Second
	defaultForecastDays = 3

	solarVariables = "shortwave_radiation,direct_radiation,diffuse_radiation,direct_normal_irradiance"
)

// APIError represents an error returned by the Open-Meteo API for non-200 responses.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("open-meteo API error %d: %s", e.StatusCode, e.Message)
}

// Client is an HTTP client for the Open-Meteo solar forecast API.
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// NewClient creates a new Client with the default base URL and a 30-second timeout.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		baseURL: defaultBaseURL,
	}
}

// NewClientWithHTTPClient creates a new Client using the provided http.Client.
func NewClientWithHTTPClient(httpClient *http.Client) *Client {
	return &Client{
		httpClient: httpClient,
		baseURL:    defaultBaseURL,
	}
}

// SetBaseURL overrides the base URL used for API requests (useful for testing).
func (c *Client) SetBaseURL(baseURL string) {
	c.baseURL = baseURL
}

// GetSolarForecast fetches a solar irradiance forecast from the Open-Meteo API.
func (c *Client) GetSolarForecast(params QueryParams) (*SolarForecast, error) {
	reqURL, err := c.buildURL(params)
	if err != nil {
		return nil, fmt.Errorf("failed to build URL: %w", err)
	}

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to perform request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	var forecast SolarForecast
	if err := json.Unmarshal(body, &forecast); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &forecast, nil
}

// buildURL constructs the full request URL with query parameters.
func (c *Client) buildURL(params QueryParams) (string, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse base URL: %w", err)
	}

	query := u.Query()
	query.Set("latitude", strconv.FormatFloat(params.Location.Latitude, 'f', -1, 64))
	query.Set("longitude", strconv.FormatFloat(params.Location.Longitude, 'f', -1, 64))
	query.Set("minutely_15", solarVariables)
	query.Set("hourly", solarVariables)

	forecastDays := params.ForecastDays
	if forecastDays <= 0 {
		forecastDays = defaultForecastDays
	}
	query.Set("forecast_days", strconv.Itoa(forecastDays))

	tz := params.Timezone
	if tz == "" {
		tz = "UTC"
	}
	query.Set("timezone", tz)

	u.RawQuery = query.Encode()
	return u.String(), nil
}