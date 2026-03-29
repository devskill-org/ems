package openmeteo

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	client := NewClient()

	if client == nil {
		t.Fatal("NewClient returned nil")
	}

	if client.baseURL != "https://api.open-meteo.com/v1/forecast" {
		t.Errorf("Expected default base URL, got %q", client.baseURL)
	}

	if client.httpClient == nil {
		t.Error("HTTP client is nil")
	}

	if client.httpClient.Timeout != 30*time.Second {
		t.Errorf("Expected 30s timeout, got %v", client.httpClient.Timeout)
	}
}

func TestNewClientWithHTTPClient(t *testing.T) {
	httpClient := &http.Client{Timeout: 5 * time.Second}
	client := NewClientWithHTTPClient(httpClient)

	if client.httpClient != httpClient {
		t.Error("Custom HTTP client was not set")
	}

	if client.baseURL != "https://api.open-meteo.com/v1/forecast" {
		t.Errorf("Expected default base URL, got %q", client.baseURL)
	}
}

func TestSetBaseURL(t *testing.T) {
	client := NewClient()
	newURL := "https://custom.example.com/api"

	client.SetBaseURL(newURL)

	if client.baseURL != newURL {
		t.Errorf("Expected base URL %q, got %q", newURL, client.baseURL)
	}
}

func TestBuildURL(t *testing.T) {
	client := NewClient()
	client.SetBaseURL("https://api.example.com/v1/forecast")

	tests := []struct {
		name   string
		params QueryParams
		checks func(t *testing.T, u *url.URL)
	}{
		{
			name: "basic location with defaults",
			params: QueryParams{
				Location: Location{
					Latitude:  56.95,
					Longitude: 24.11,
				},
			},
			checks: func(t *testing.T, u *url.URL) {
				q := u.Query()
				if q.Get("latitude") != "56.95" {
					t.Errorf("Expected latitude 56.95, got %q", q.Get("latitude"))
				}
				if q.Get("longitude") != "24.11" {
					t.Errorf("Expected longitude 24.11, got %q", q.Get("longitude"))
				}
				if q.Get("forecast_days") != "3" {
					t.Errorf("Expected default forecast_days 3, got %q", q.Get("forecast_days"))
				}
				if q.Get("timezone") != "UTC" {
					t.Errorf("Expected default timezone UTC, got %q", q.Get("timezone"))
				}
				if q.Get("minutely_15") == "" {
					t.Error("Expected minutely_15 parameter to be set")
				}
				if q.Get("hourly") == "" {
					t.Error("Expected hourly parameter to be set")
				}
			},
		},
		{
			name: "custom forecast days",
			params: QueryParams{
				Location: Location{
					Latitude:  59.91,
					Longitude: 10.75,
				},
				ForecastDays: 7,
			},
			checks: func(t *testing.T, u *url.URL) {
				q := u.Query()
				if q.Get("forecast_days") != "7" {
					t.Errorf("Expected forecast_days 7, got %q", q.Get("forecast_days"))
				}
			},
		},
		{
			name: "custom timezone",
			params: QueryParams{
				Location: Location{
					Latitude:  48.85,
					Longitude: 2.35,
				},
				Timezone: "Europe/Paris",
			},
			checks: func(t *testing.T, u *url.URL) {
				q := u.Query()
				if q.Get("timezone") != "Europe/Paris" {
					t.Errorf("Expected timezone Europe/Paris, got %q", q.Get("timezone"))
				}
			},
		},
		{
			name: "forecast_days=1 with custom timezone",
			params: QueryParams{
				Location: Location{
					Latitude:  35.68,
					Longitude: 139.69,
				},
				ForecastDays: 1,
				Timezone:     "Asia/Tokyo",
			},
			checks: func(t *testing.T, u *url.URL) {
				q := u.Query()
				if q.Get("latitude") != "35.68" {
					t.Errorf("Expected latitude 35.68, got %q", q.Get("latitude"))
				}
				if q.Get("longitude") != "139.69" {
					t.Errorf("Expected longitude 139.69, got %q", q.Get("longitude"))
				}
				if q.Get("forecast_days") != "1" {
					t.Errorf("Expected forecast_days 1, got %q", q.Get("forecast_days"))
				}
				if q.Get("timezone") != "Asia/Tokyo" {
					t.Errorf("Expected timezone Asia/Tokyo, got %q", q.Get("timezone"))
				}
			},
		},
		{
			name: "radiation variables are requested",
			params: QueryParams{
				Location: Location{
					Latitude:  50.0,
					Longitude: 10.0,
				},
			},
			checks: func(t *testing.T, u *url.URL) {
				q := u.Query()
				expectedVars := "shortwave_radiation,direct_radiation,diffuse_radiation,direct_normal_irradiance"
				if q.Get("minutely_15") != expectedVars {
					t.Errorf("Expected minutely_15 %q, got %q", expectedVars, q.Get("minutely_15"))
				}
				if q.Get("hourly") != expectedVars {
					t.Errorf("Expected hourly %q, got %q", expectedVars, q.Get("hourly"))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqURL, err := client.buildURL(tt.params)
			if err != nil {
				t.Fatalf("buildURL returned error: %v", err)
			}

			u, err := url.Parse(reqURL)
			if err != nil {
				t.Fatalf("Failed to parse built URL: %v", err)
			}

			if u.Scheme != "https" || u.Host != "api.example.com" {
				t.Errorf("Unexpected URL base: %s://%s", u.Scheme, u.Host)
			}

			tt.checks(t, u)
		})
	}
}

func TestGetSolarForecast_Success(t *testing.T) {
	testForecast := SolarForecast{
		Latitude:             56.95295,
		Longitude:            24.111404,
		GenerationTimeMs:     0.07,
		UTCOffsetSeconds:     0,
		Timezone:             "GMT",
		TimezoneAbbreviation: "GMT",
		Elevation:            17.0,
		Minutely15Units: &TimeSeriesUnits{
			Time:                   "iso8601",
			ShortwaveRadiation:     "W/m\u00b2",
			DirectRadiation:        "W/m\u00b2",
			DiffuseRadiation:       "W/m\u00b2",
			DirectNormalIrradiance: "W/m\u00b2",
		},
		Minutely15: &TimeSeriesData{
			Time:                   []string{"2026-03-29T00:00", "2026-03-29T00:15"},
			ShortwaveRadiation:     []float64{0.0, 0.0},
			DirectRadiation:        []float64{0.0, 0.0},
			DiffuseRadiation:       []float64{0.0, 0.0},
			DirectNormalIrradiance: []float64{0.0, 0.0},
		},
		HourlyUnits: &TimeSeriesUnits{
			Time:                   "iso8601",
			ShortwaveRadiation:     "W/m\u00b2",
			DirectRadiation:        "W/m\u00b2",
			DiffuseRadiation:       "W/m\u00b2",
			DirectNormalIrradiance: "W/m\u00b2",
		},
		Hourly: &TimeSeriesData{
			Time:                   []string{"2026-03-29T00:00", "2026-03-29T01:00"},
			ShortwaveRadiation:     []float64{0.0, 10.5},
			DirectRadiation:        []float64{0.0, 5.2},
			DiffuseRadiation:       []float64{0.0, 5.3},
			DirectNormalIrradiance: []float64{0.0, 8.1},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Expected GET method, got %s", r.Method)
		}

		q := r.URL.Query()
		if q.Get("latitude") == "" {
			t.Error("Missing latitude parameter")
		}
		if q.Get("longitude") == "" {
			t.Error("Missing longitude parameter")
		}
		if q.Get("minutely_15") == "" {
			t.Error("Missing minutely_15 parameter")
		}
		if q.Get("hourly") == "" {
			t.Error("Missing hourly parameter")
		}
		if q.Get("timezone") != "UTC" {
			t.Errorf("Expected timezone UTC, got %q", q.Get("timezone"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(testForecast)
	}))
	defer server.Close()

	client := NewClient()
	client.SetBaseURL(server.URL)

	params := QueryParams{
		Location: Location{
			Latitude:  56.95,
			Longitude: 24.11,
		},
	}

	forecast, err := client.GetSolarForecast(params)
	if err != nil {
		t.Fatalf("GetSolarForecast returned error: %v", err)
	}

	if forecast.Latitude != 56.95295 {
		t.Errorf("Expected latitude 56.95295, got %f", forecast.Latitude)
	}

	if forecast.Longitude != 24.111404 {
		t.Errorf("Expected longitude 24.111404, got %f", forecast.Longitude)
	}

	if forecast.Timezone != "GMT" {
		t.Errorf("Expected timezone GMT, got %s", forecast.Timezone)
	}

	if forecast.Elevation != 17.0 {
		t.Errorf("Expected elevation 17.0, got %f", forecast.Elevation)
	}

	if forecast.Minutely15 == nil {
		t.Fatal("Minutely15 is nil")
	}
	if len(forecast.Minutely15.Time) != 2 {
		t.Errorf("Expected 2 minutely_15 time entries, got %d", len(forecast.Minutely15.Time))
	}

	if forecast.Hourly == nil {
		t.Fatal("Hourly is nil")
	}
	if len(forecast.Hourly.Time) != 2 {
		t.Errorf("Expected 2 hourly time entries, got %d", len(forecast.Hourly.Time))
	}

	if forecast.Minutely15Units == nil {
		t.Fatal("Minutely15Units is nil")
	}
	if forecast.Minutely15Units.ShortwaveRadiation != "W/m\u00b2" {
		t.Errorf("Expected shortwave_radiation unit W/m², got %s", forecast.Minutely15Units.ShortwaveRadiation)
	}

	if forecast.HourlyUnits == nil {
		t.Fatal("HourlyUnits is nil")
	}
	if forecast.HourlyUnits.DirectRadiation != "W/m\u00b2" {
		t.Errorf("Expected direct_radiation unit W/m², got %s", forecast.HourlyUnits.DirectRadiation)
	}
}

func TestGetSolarForecast_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"reason":"Cannot initialize LocationNotFound"}`))
	}))
	defer server.Close()

	client := NewClient()
	client.SetBaseURL(server.URL)

	params := QueryParams{
		Location: Location{
			Latitude:  999.0,
			Longitude: 999.0,
		},
	}

	_, err := client.GetSolarForecast(params)
	if err == nil {
		t.Fatal("Expected API error, got nil")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("Expected *APIError, got %T: %v", err, err)
	}

	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status code %d, got %d", http.StatusBadRequest, apiErr.StatusCode)
	}

	if apiErr.Message == "" {
		t.Error("Expected non-empty error message")
	}
}

func TestGetSolarForecast_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	client := NewClient()
	client.SetBaseURL(server.URL)

	_, err := client.GetSolarForecast(QueryParams{
		Location: Location{Latitude: 56.95, Longitude: 24.11},
	})
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("Expected *APIError, got %T: %v", err, err)
	}

	if apiErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("Expected status code %d, got %d", http.StatusInternalServerError, apiErr.StatusCode)
	}

	if apiErr.Message != "Internal Server Error" {
		t.Errorf("Expected message %q, got %q", "Internal Server Error", apiErr.Message)
	}
}

func TestAPIError_Error(t *testing.T) {
	err := &APIError{
		StatusCode: 404,
		Message:    "Not Found",
	}
	expected := "open-meteo API error 404: Not Found"
	if err.Error() != expected {
		t.Errorf("Expected %q, got %q", expected, err.Error())
	}
}

func TestDataPoints_MinutelyOnly(t *testing.T) {
	forecast := &SolarForecast{
		Minutely15: &TimeSeriesData{
			Time:                   []string{"2026-03-29T12:00", "2026-03-29T12:15", "2026-03-29T12:30"},
			ShortwaveRadiation:     []float64{100.0, 120.0, 140.0},
			DirectRadiation:        []float64{80.0, 95.0, 110.0},
			DiffuseRadiation:       []float64{20.0, 25.0, 30.0},
			DirectNormalIrradiance: []float64{110.0, 130.0, 150.0},
		},
	}

	points, err := forecast.DataPoints()
	if err != nil {
		t.Fatalf("DataPoints returned error: %v", err)
	}

	if len(points) != 3 {
		t.Fatalf("Expected 3 data points, got %d", len(points))
	}

	expected := time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC)
	if !points[0].Time.Equal(expected) {
		t.Errorf("Expected first time %v, got %v", expected, points[0].Time)
	}

	if points[0].ShortwaveRadiation != 100.0 {
		t.Errorf("Expected shortwave 100.0, got %f", points[0].ShortwaveRadiation)
	}

	if points[1].DirectRadiation != 95.0 {
		t.Errorf("Expected direct radiation 95.0, got %f", points[1].DirectRadiation)
	}

	if points[2].DiffuseRadiation != 30.0 {
		t.Errorf("Expected diffuse radiation 30.0, got %f", points[2].DiffuseRadiation)
	}
}

func TestDataPoints_HourlyOnly(t *testing.T) {
	forecast := &SolarForecast{
		Hourly: &TimeSeriesData{
			Time:                   []string{"2026-03-29T10:00", "2026-03-29T11:00"},
			ShortwaveRadiation:     []float64{200.0, 300.0},
			DirectRadiation:        []float64{150.0, 230.0},
			DiffuseRadiation:       []float64{50.0, 70.0},
			DirectNormalIrradiance: []float64{220.0, 340.0},
		},
	}

	points, err := forecast.DataPoints()
	if err != nil {
		t.Fatalf("DataPoints returned error: %v", err)
	}

	if len(points) != 2 {
		t.Fatalf("Expected 2 data points, got %d", len(points))
	}

	if points[0].ShortwaveRadiation != 200.0 {
		t.Errorf("Expected shortwave 200.0, got %f", points[0].ShortwaveRadiation)
	}

	if points[1].DirectNormalIrradiance != 340.0 {
		t.Errorf("Expected DNI 340.0, got %f", points[1].DirectNormalIrradiance)
	}
}

func TestDataPoints_MergePreferMinutely(t *testing.T) {
	// minutely_15 covers 12:00, 12:15, 12:30, 12:45
	// hourly covers 12:00, 13:00
	// The 12:00 overlap should use minutely_15 values, 13:00 should come from hourly.
	forecast := &SolarForecast{
		Minutely15: &TimeSeriesData{
			Time:                   []string{"2026-03-29T12:00", "2026-03-29T12:15", "2026-03-29T12:30", "2026-03-29T12:45"},
			ShortwaveRadiation:     []float64{100.0, 110.0, 120.0, 130.0},
			DirectRadiation:        []float64{80.0, 85.0, 90.0, 95.0},
			DiffuseRadiation:       []float64{20.0, 25.0, 30.0, 35.0},
			DirectNormalIrradiance: []float64{105.0, 115.0, 125.0, 135.0},
		},
		Hourly: &TimeSeriesData{
			Time:                   []string{"2026-03-29T12:00", "2026-03-29T13:00"},
			ShortwaveRadiation:     []float64{999.0, 200.0},
			DirectRadiation:        []float64{999.0, 150.0},
			DiffuseRadiation:       []float64{999.0, 50.0},
			DirectNormalIrradiance: []float64{999.0, 220.0},
		},
	}

	points, err := forecast.DataPoints()
	if err != nil {
		t.Fatalf("DataPoints returned error: %v", err)
	}

	// Should have 5 points: 12:00, 12:15, 12:30, 12:45 from minutely + 13:00 from hourly.
	if len(points) != 5 {
		t.Fatalf("Expected 5 merged data points, got %d", len(points))
	}

	// Verify sorted order.
	for i := 1; i < len(points); i++ {
		if !points[i-1].Time.Before(points[i].Time) {
			t.Errorf("Points not sorted: %v >= %v at index %d", points[i-1].Time, points[i].Time, i)
		}
	}

	// The 12:00 point should have minutely_15 values (100.0), not hourly (999.0).
	if points[0].ShortwaveRadiation != 100.0 {
		t.Errorf("Expected 12:00 shortwave from minutely_15 (100.0), got %f", points[0].ShortwaveRadiation)
	}

	// The 13:00 point should come from hourly.
	lastPoint := points[4]
	expectedTime := time.Date(2026, 3, 29, 13, 0, 0, 0, time.UTC)
	if !lastPoint.Time.Equal(expectedTime) {
		t.Errorf("Expected last point at %v, got %v", expectedTime, lastPoint.Time)
	}
	if lastPoint.ShortwaveRadiation != 200.0 {
		t.Errorf("Expected 13:00 shortwave from hourly (200.0), got %f", lastPoint.ShortwaveRadiation)
	}
	if lastPoint.DirectNormalIrradiance != 220.0 {
		t.Errorf("Expected 13:00 DNI from hourly (220.0), got %f", lastPoint.DirectNormalIrradiance)
	}
}

func TestDataPoints_BothNil(t *testing.T) {
	forecast := &SolarForecast{}

	points, err := forecast.DataPoints()
	if err != nil {
		t.Fatalf("DataPoints returned error: %v", err)
	}

	if len(points) != 0 {
		t.Errorf("Expected 0 data points, got %d", len(points))
	}
}

func TestDataPoints_EmptyTimeSeries(t *testing.T) {
	forecast := &SolarForecast{
		Minutely15: &TimeSeriesData{
			Time:                   []string{},
			ShortwaveRadiation:     []float64{},
			DirectRadiation:        []float64{},
			DiffuseRadiation:       []float64{},
			DirectNormalIrradiance: []float64{},
		},
		Hourly: &TimeSeriesData{
			Time:                   []string{},
			ShortwaveRadiation:     []float64{},
			DirectRadiation:        []float64{},
			DiffuseRadiation:       []float64{},
			DirectNormalIrradiance: []float64{},
		},
	}

	points, err := forecast.DataPoints()
	if err != nil {
		t.Fatalf("DataPoints returned error: %v", err)
	}

	if len(points) != 0 {
		t.Errorf("Expected 0 data points, got %d", len(points))
	}
}

func TestDataPoints_TimeParsingUTC(t *testing.T) {
	forecast := &SolarForecast{
		Minutely15: &TimeSeriesData{
			Time:                   []string{"2026-06-15T14:30"},
			ShortwaveRadiation:     []float64{500.0},
			DirectRadiation:        []float64{400.0},
			DiffuseRadiation:       []float64{100.0},
			DirectNormalIrradiance: []float64{600.0},
		},
	}

	points, err := forecast.DataPoints()
	if err != nil {
		t.Fatalf("DataPoints returned error: %v", err)
	}

	if len(points) != 1 {
		t.Fatalf("Expected 1 data point, got %d", len(points))
	}

	expected := time.Date(2026, 6, 15, 14, 30, 0, 0, time.UTC)
	if !points[0].Time.Equal(expected) {
		t.Errorf("Expected time %v, got %v", expected, points[0].Time)
	}

	if points[0].Time.Location() != time.UTC {
		t.Errorf("Expected UTC location, got %v", points[0].Time.Location())
	}
}

func TestDataPoints_SortedOutput(t *testing.T) {
	// Provide data in non-chronological order across the two series.
	forecast := &SolarForecast{
		Minutely15: &TimeSeriesData{
			Time:                   []string{"2026-03-29T14:00", "2026-03-29T14:15"},
			ShortwaveRadiation:     []float64{300.0, 310.0},
			DirectRadiation:        []float64{200.0, 210.0},
			DiffuseRadiation:       []float64{100.0, 100.0},
			DirectNormalIrradiance: []float64{350.0, 360.0},
		},
		Hourly: &TimeSeriesData{
			Time:                   []string{"2026-03-29T10:00", "2026-03-29T11:00", "2026-03-29T15:00"},
			ShortwaveRadiation:     []float64{50.0, 100.0, 280.0},
			DirectRadiation:        []float64{30.0, 70.0, 200.0},
			DiffuseRadiation:       []float64{20.0, 30.0, 80.0},
			DirectNormalIrradiance: []float64{40.0, 90.0, 300.0},
		},
	}

	points, err := forecast.DataPoints()
	if err != nil {
		t.Fatalf("DataPoints returned error: %v", err)
	}

	// 2 minutely + 3 hourly (no overlap) = 5
	if len(points) != 5 {
		t.Fatalf("Expected 5 data points, got %d", len(points))
	}

	for i := 1; i < len(points); i++ {
		if !points[i-1].Time.Before(points[i].Time) {
			t.Errorf("Points not sorted at index %d: %v >= %v", i, points[i-1].Time, points[i].Time)
		}
	}

	// First point should be 10:00 (from hourly).
	expectedFirst := time.Date(2026, 3, 29, 10, 0, 0, 0, time.UTC)
	if !points[0].Time.Equal(expectedFirst) {
		t.Errorf("Expected first point at %v, got %v", expectedFirst, points[0].Time)
	}

	// Last point should be 15:00 (from hourly).
	expectedLast := time.Date(2026, 3, 29, 15, 0, 0, 0, time.UTC)
	if !points[4].Time.Equal(expectedLast) {
		t.Errorf("Expected last point at %v, got %v", expectedLast, points[4].Time)
	}
}

func TestGetSolarForecast_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{invalid json`))
	}))
	defer server.Close()

	client := NewClient()
	client.SetBaseURL(server.URL)

	_, err := client.GetSolarForecast(QueryParams{
		Location: Location{Latitude: 56.95, Longitude: 24.11},
	})
	if err == nil {
		t.Fatal("Expected error for invalid JSON, got nil")
	}
}