package openmeteo

import (
	"fmt"
	"sort"
	"time"
)

// timeLayout is the time format used by the Open-Meteo API.
const timeLayout = "2006-01-02T15:04"

// Location represents geographic coordinates for a forecast request.
type Location struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// QueryParams holds the parameters for a solar forecast query.
type QueryParams struct {
	Location     Location `json:"location"`
	ForecastDays int      `json:"forecast_days,omitempty"`
	Timezone     string   `json:"timezone,omitempty"`
}

// TimeSeriesUnits contains the units for each field in the time series data.
type TimeSeriesUnits struct {
	Time                   string `json:"time"`
	ShortwaveRadiation     string `json:"shortwave_radiation"`
	DirectRadiation        string `json:"direct_radiation"`
	DiffuseRadiation       string `json:"diffuse_radiation"`
	DirectNormalIrradiance string `json:"direct_normal_irradiance"`
}

// TimeSeriesData contains the actual time series values returned by the API.
type TimeSeriesData struct {
	Time                   []string  `json:"time"`
	ShortwaveRadiation     []float64 `json:"shortwave_radiation"`
	DirectRadiation        []float64 `json:"direct_radiation"`
	DiffuseRadiation       []float64 `json:"diffuse_radiation"`
	DirectNormalIrradiance []float64 `json:"direct_normal_irradiance"`
}

// SolarDataPoint represents a single parsed data point with a proper time.Time value.
type SolarDataPoint struct {
	Time                   time.Time `json:"time"`
	ShortwaveRadiation     float64   `json:"shortwave_radiation"`
	DirectRadiation        float64   `json:"direct_radiation"`
	DiffuseRadiation       float64   `json:"diffuse_radiation"`
	DirectNormalIrradiance float64   `json:"direct_normal_irradiance"`
}

// SolarForecast is the root response struct from the Open-Meteo solar forecast API.
type SolarForecast struct {
	Latitude             float64          `json:"latitude"`
	Longitude            float64          `json:"longitude"`
	GenerationTimeMs     float64          `json:"generationtime_ms"`
	UTCOffsetSeconds     int              `json:"utc_offset_seconds"`
	Timezone             string           `json:"timezone"`
	TimezoneAbbreviation string           `json:"timezone_abbreviation"`
	Elevation            float64          `json:"elevation"`
	Minutely15Units      *TimeSeriesUnits `json:"minutely_15_units,omitempty"`
	Minutely15           *TimeSeriesData  `json:"minutely_15,omitempty"`
	HourlyUnits          *TimeSeriesUnits `json:"hourly_units,omitempty"`
	Hourly               *TimeSeriesData  `json:"hourly,omitempty"`
}

// parseTimeSeries converts a TimeSeriesData into a slice of SolarDataPoint.
// Time strings are parsed as UTC using the Open-Meteo time layout.
func parseTimeSeries(data *TimeSeriesData) ([]SolarDataPoint, error) {
	if data == nil {
		return nil, nil
	}

	n := len(data.Time)
	if n == 0 {
		return nil, nil
	}

	// Validate that all slices have the same length.
	if len(data.ShortwaveRadiation) != n ||
		len(data.DirectRadiation) != n ||
		len(data.DiffuseRadiation) != n ||
		len(data.DirectNormalIrradiance) != n {
		return nil, fmt.Errorf("time series length mismatch: time has %d entries but radiation arrays differ", n)
	}

	points := make([]SolarDataPoint, 0, n)
	for i := 0; i < n; i++ {
		t, err := time.Parse(timeLayout, data.Time[i])
		if err != nil {
			return nil, fmt.Errorf("failed to parse time %q at index %d: %w", data.Time[i], i, err)
		}
		points = append(points, SolarDataPoint{
			Time:                   t.UTC(),
			ShortwaveRadiation:     data.ShortwaveRadiation[i],
			DirectRadiation:        data.DirectRadiation[i],
			DiffuseRadiation:       data.DiffuseRadiation[i],
			DirectNormalIrradiance: data.DirectNormalIrradiance[i],
		})
	}
	return points, nil
}

// DataPoints merges minutely_15 and hourly data into a single sorted slice of
// SolarDataPoint. When both resolutions contain a data point for the same
// timestamp, the minutely_15 (higher-resolution) value is preferred.
func (f *SolarForecast) DataPoints() ([]SolarDataPoint, error) {
	minutelyPoints, err := parseTimeSeries(f.Minutely15)
	if err != nil {
		return nil, fmt.Errorf("parsing minutely_15 data: %w", err)
	}

	hourlyPoints, err := parseTimeSeries(f.Hourly)
	if err != nil {
		return nil, fmt.Errorf("parsing hourly data: %w", err)
	}

	// Index all minutely_15 timestamps so we can skip duplicates from hourly.
	minutelyTimes := make(map[time.Time]struct{}, len(minutelyPoints))
	for _, p := range minutelyPoints {
		minutelyTimes[p.Time] = struct{}{}
	}

	// Start with all minutely_15 points, then append hourly points whose
	// timestamps are not already covered by minutely_15.
	merged := make([]SolarDataPoint, 0, len(minutelyPoints)+len(hourlyPoints))
	merged = append(merged, minutelyPoints...)
	for _, p := range hourlyPoints {
		if _, exists := minutelyTimes[p.Time]; !exists {
			merged = append(merged, p)
		}
	}

	// Sort by time ascending.
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Time.Before(merged[j].Time)
	})

	return merged, nil
}