package entsoe

import (
	"os"
	"testing"
	"time"
)

func TestParseISO8601Duration(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected time.Duration
		wantErr  bool
	}{
		{
			name:     "1 hour",
			input:    "PT1H",
			expected: time.Hour,
			wantErr:  false,
		},
		{
			name:     "60 minutes",
			input:    "PT60M",
			expected: 60 * time.Minute,
			wantErr:  false,
		},
		{
			name:     "30 seconds",
			input:    "PT30S",
			expected: 30 * time.Second,
			wantErr:  false,
		},
		{
			name:     "1.5 hours (1 hour 30 minutes)",
			input:    "PT1H30M",
			expected: time.Hour + 30*time.Minute,
			wantErr:  false,
		},
		{
			name:     "90 minutes",
			input:    "PT90M",
			expected: 90 * time.Minute,
			wantErr:  false,
		},
		{
			name:     "1 day",
			input:    "P1D",
			expected: 24 * time.Hour,
			wantErr:  false,
		},
		{
			name:     "1 week (7 days)",
			input:    "P7D",
			expected: 7 * 24 * time.Hour,
			wantErr:  false,
		},
		{
			name:     "15 minutes",
			input:    "PT15M",
			expected: 15 * time.Minute,
			wantErr:  false,
		},
		{
			name:     "2.5 seconds",
			input:    "PT2.5S",
			expected: time.Duration(2.5 * float64(time.Second)),
			wantErr:  false,
		},
		{
			name:     "1 day 2 hours",
			input:    "P1DT2H",
			expected: 24*time.Hour + 2*time.Hour,
			wantErr:  false,
		},
		{
			name:     "complex duration with all components",
			input:    "P1DT2H30M45S",
			expected: 24*time.Hour + 2*time.Hour + 30*time.Minute + 45*time.Second,
			wantErr:  false,
		},
		{
			name:     "only seconds with decimal",
			input:    "PT0.5S",
			expected: 500 * time.Millisecond,
			wantErr:  false,
		},
		{
			name:     "invalid format - missing P",
			input:    "T1H",
			expected: 0,
			wantErr:  true,
		},
		{
			name:     "invalid format - empty string",
			input:    "",
			expected: 0,
			wantErr:  true,
		},
		{
			name:     "empty duration - only P",
			input:    "P",
			expected: 0,
			wantErr:  false, // Should return 0 duration but no error
		},
		{
			name:     "invalid unit",
			input:    "PT1X",
			expected: 0,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseISO8601Duration(tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("parseISO8601Duration() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && result != tt.expected {
				t.Errorf("parseISO8601Duration() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestParseISO8601Duration_CommonENTSOEFormats(t *testing.T) {
	// Test specific formats commonly found in ENTSO-E XML files
	tests := []struct {
		name     string
		input    string
		expected time.Duration
	}{
		{
			name:     "hourly resolution",
			input:    "PT60M",
			expected: time.Hour,
		},
		{
			name:     "15-minute resolution",
			input:    "PT15M",
			expected: 15 * time.Minute,
		},
		{
			name:     "30-minute resolution",
			input:    "PT30M",
			expected: 30 * time.Minute,
		},
		{
			name:     "daily resolution",
			input:    "P1D",
			expected: 24 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseISO8601Duration(tt.input)
			if err != nil {
				t.Errorf("parseISO8601Duration() unexpected error = %v", err)
				return
			}
			if result != tt.expected {
				t.Errorf("parseISO8601Duration() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestParseDatePart(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected time.Duration
		wantErr  bool
	}{
		{
			name:     "1 day",
			input:    "1D",
			expected: 24 * time.Hour,
			wantErr:  false,
		},
		{
			name:     "7 days",
			input:    "7D",
			expected: 7 * 24 * time.Hour,
			wantErr:  false,
		},
		{
			name:     "1 month (approximate)",
			input:    "1M",
			expected: 30 * 24 * time.Hour,
			wantErr:  false,
		},
		{
			name:     "1 year (approximate)",
			input:    "1Y",
			expected: 365 * 24 * time.Hour,
			wantErr:  false,
		},
		{
			name:     "combined 1 year 1 month 1 day",
			input:    "1Y1M1D",
			expected: 365*24*time.Hour + 30*24*time.Hour + 24*time.Hour,
			wantErr:  false,
		},
		{
			name:     "invalid unit",
			input:    "1X",
			expected: 0,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseDatePart(tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("parseDatePart() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && result != tt.expected {
				t.Errorf("parseDatePart() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestParseTimePart(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected time.Duration
		wantErr  bool
	}{
		{
			name:     "1 hour",
			input:    "1H",
			expected: time.Hour,
			wantErr:  false,
		},
		{
			name:     "30 minutes",
			input:    "30M",
			expected: 30 * time.Minute,
			wantErr:  false,
		},
		{
			name:     "45 seconds",
			input:    "45S",
			expected: 45 * time.Second,
			wantErr:  false,
		},
		{
			name:     "2.5 seconds",
			input:    "2.5S",
			expected: time.Duration(2.5 * float64(time.Second)),
			wantErr:  false,
		},
		{
			name:     "combined 1 hour 30 minutes 45 seconds",
			input:    "1H30M45S",
			expected: time.Hour + 30*time.Minute + 45*time.Second,
			wantErr:  false,
		},
		{
			name:     "invalid unit",
			input:    "1X",
			expected: 0,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseTimePart(tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("parseTimePart() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && result != tt.expected {
				t.Errorf("parseTimePart() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestParseFloat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected float64
		wantErr  bool
	}{
		{
			name:     "integer",
			input:    "42",
			expected: 42.0,
			wantErr:  false,
		},
		{
			name:     "decimal",
			input:    "3.14",
			expected: 3.14,
			wantErr:  false,
		},
		{
			name:     "zero",
			input:    "0",
			expected: 0.0,
			wantErr:  false,
		},
		{
			name:     "decimal zero",
			input:    "0.0",
			expected: 0.0,
			wantErr:  false,
		},
		{
			name:     "decimal with trailing zeros",
			input:    "2.50",
			expected: 2.5,
			wantErr:  false,
		},
		{
			name:     "invalid character",
			input:    "1.2a3",
			expected: 0,
			wantErr:  true,
		},
		{
			name:     "multiple dots",
			input:    "1.2.3",
			expected: 0,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseFloat(tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("parseFloat() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && result != tt.expected {
				t.Errorf("parseFloat() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetPriceByHour_TimeParameter(t *testing.T) {
	// Create a test period with known time interval and resolution
	period := &Period{
		TimeInterval: TimeInterval{
			Start: time.Date(2025, 9, 4, 22, 0, 0, 0, time.UTC),
			End:   time.Date(2025, 9, 5, 22, 0, 0, 0, time.UTC),
		},
		Resolution: time.Hour,
		Points: []Point{
			{Position: 1, PriceAmount: 100.0},
			{Position: 2, PriceAmount: 200.0},
			{Position: 3, PriceAmount: 300.0},
		},
	}

	tests := []struct {
		name          string
		queryTime     time.Time
		expectedPrice float64
		shouldFind    bool
	}{
		{
			name:          "exact start time",
			queryTime:     time.Date(2025, 9, 4, 22, 0, 0, 0, time.UTC),
			expectedPrice: 100.0,
			shouldFind:    true,
		},
		{
			name:          "middle of first hour",
			queryTime:     time.Date(2025, 9, 4, 22, 30, 0, 0, time.UTC),
			expectedPrice: 100.0,
			shouldFind:    true,
		},
		{
			name:          "start of second hour",
			queryTime:     time.Date(2025, 9, 4, 23, 0, 0, 0, time.UTC),
			expectedPrice: 200.0,
			shouldFind:    true,
		},
		{
			name:          "middle of third hour",
			queryTime:     time.Date(2025, 9, 5, 0, 15, 0, 0, time.UTC),
			expectedPrice: 300.0,
			shouldFind:    true,
		},
		{
			name:          "before period start",
			queryTime:     time.Date(2025, 9, 4, 21, 30, 0, 0, time.UTC),
			expectedPrice: 0,
			shouldFind:    false,
		},
		{
			name:          "after period end",
			queryTime:     time.Date(2025, 9, 5, 22, 30, 0, 0, time.UTC),
			expectedPrice: 0,
			shouldFind:    false,
		},
		{
			name:          "exact period end",
			queryTime:     time.Date(2025, 9, 5, 22, 0, 0, 0, time.UTC),
			expectedPrice: 0,
			shouldFind:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			price, found := period.GetPriceByTime(tt.queryTime)
			if found != tt.shouldFind {
				t.Errorf("GetPriceByHour() found = %v, want %v", found, tt.shouldFind)
			}
			if found && price != tt.expectedPrice {
				t.Errorf("GetPriceByHour() price = %v, want %v", price, tt.expectedPrice)
			}
		})
	}
}

func BenchmarkGetPriceByHour_TimeParameter(b *testing.B) {
	period := &Period{
		TimeInterval: TimeInterval{
			Start: time.Date(2025, 9, 4, 22, 0, 0, 0, time.UTC),
			End:   time.Date(2025, 9, 5, 22, 0, 0, 0, time.UTC),
		},
		Resolution: time.Hour,
		Points: []Point{
			{Position: 1, PriceAmount: 100.0},
			{Position: 2, PriceAmount: 200.0},
			{Position: 3, PriceAmount: 300.0},
			{Position: 12, PriceAmount: 120.0},
		},
	}

	queryTime := time.Date(2025, 9, 4, 22, 30, 0, 0, time.UTC)

	for b.Loop() {
		_, _ = period.GetPriceByTime(queryTime)
	}
}

func BenchmarkCalculatePosition(b *testing.B) {
	period := &Period{
		TimeInterval: TimeInterval{
			Start: time.Date(2025, 9, 4, 22, 0, 0, 0, time.UTC),
			End:   time.Date(2025, 9, 5, 22, 0, 0, 0, time.UTC),
		},
		Resolution: time.Hour,
	}

	queryTime := time.Date(2025, 9, 4, 23, 15, 0, 0, time.UTC)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = period.calculatePosition(queryTime)
	}
}

func BenchmarkGetTimeRangeForPosition(b *testing.B) {
	period := &Period{
		TimeInterval: TimeInterval{
			Start: time.Date(2025, 9, 4, 22, 0, 0, 0, time.UTC),
			End:   time.Date(2025, 9, 5, 22, 0, 0, 0, time.UTC),
		},
		Resolution: time.Hour,
	}

	for b.Loop() {
		_, _, _ = period.GetTimeRangeForPosition(5)
	}
}

func TestCalculatePosition(t *testing.T) {
	period := &Period{
		TimeInterval: TimeInterval{
			Start: time.Date(2025, 9, 4, 22, 0, 0, 0, time.UTC),
			End:   time.Date(2025, 9, 5, 22, 0, 0, 0, time.UTC),
		},
		Resolution: time.Hour,
	}

	tests := []struct {
		name             string
		queryTime        time.Time
		expectedPosition int
	}{
		{
			name:             "start time - position 1",
			queryTime:        time.Date(2025, 9, 4, 22, 0, 0, 0, time.UTC),
			expectedPosition: 1,
		},
		{
			name:             "30 minutes later - still position 1",
			queryTime:        time.Date(2025, 9, 4, 22, 30, 0, 0, time.UTC),
			expectedPosition: 1,
		},
		{
			name:             "1 hour later - position 2",
			queryTime:        time.Date(2025, 9, 4, 23, 0, 0, 0, time.UTC),
			expectedPosition: 2,
		},
		{
			name:             "2 hours later - position 3",
			queryTime:        time.Date(2025, 9, 5, 0, 0, 0, 0, time.UTC),
			expectedPosition: 3,
		},
		{
			name:             "before start - position 0",
			queryTime:        time.Date(2025, 9, 4, 21, 0, 0, 0, time.UTC),
			expectedPosition: 0,
		},
		{
			name:             "at end time - position 0",
			queryTime:        time.Date(2025, 9, 5, 22, 0, 0, 0, time.UTC),
			expectedPosition: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			position := period.calculatePosition(tt.queryTime)
			if position != tt.expectedPosition {
				t.Errorf("calculatePosition() = %v, want %v", position, tt.expectedPosition)
			}
		})
	}
}

func TestGetTimeRangeForPosition(t *testing.T) {
	period := &Period{
		TimeInterval: TimeInterval{
			Start: time.Date(2025, 9, 4, 22, 0, 0, 0, time.UTC),
			End:   time.Date(2025, 9, 5, 22, 0, 0, 0, time.UTC),
		},
		Resolution: time.Hour,
	}

	tests := []struct {
		name          string
		position      int
		expectedStart time.Time
		expectedEnd   time.Time
		expectedValid bool
	}{
		{
			name:          "position 1",
			position:      1,
			expectedStart: time.Date(2025, 9, 4, 22, 0, 0, 0, time.UTC),
			expectedEnd:   time.Date(2025, 9, 4, 23, 0, 0, 0, time.UTC),
			expectedValid: true,
		},
		{
			name:          "position 2",
			position:      2,
			expectedStart: time.Date(2025, 9, 4, 23, 0, 0, 0, time.UTC),
			expectedEnd:   time.Date(2025, 9, 5, 0, 0, 0, 0, time.UTC),
			expectedValid: true,
		},
		{
			name:          "position 0 - invalid",
			position:      0,
			expectedStart: time.Time{},
			expectedEnd:   time.Time{},
			expectedValid: false,
		},
		{
			name:          "position beyond period",
			position:      25,
			expectedStart: time.Time{},
			expectedEnd:   time.Time{},
			expectedValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, valid := period.GetTimeRangeForPosition(tt.position)
			if valid != tt.expectedValid {
				t.Errorf("GetTimeRangeForPosition() valid = %v, want %v", valid, tt.expectedValid)
			}
			if valid {
				if !start.Equal(tt.expectedStart) {
					t.Errorf("GetTimeRangeForPosition() start = %v, want %v", start, tt.expectedStart)
				}
				if !end.Equal(tt.expectedEnd) {
					t.Errorf("GetTimeRangeForPosition() end = %v, want %v", end, tt.expectedEnd)
				}
			}
		})
	}
}

func TestDocumentDecode(t *testing.T) {
	file, err := os.Open("../test_data/Energy_Prices_202509112200-202509122200.xml")
	if err != nil {
		t.Fatal(err)
	}
	prices, err := DecodeEnergyPricesXML(file)
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Date(2025, 9, 12, 12, 0, 11, 0, time.UTC)
	price, found := prices.LookupPriceByTime(ts)
	if !found {
		t.Errorf("Price not found for %s", ts)
	}
	if price != 57.73 {
		t.Errorf("Returned price: %f, want %f", price, 57.73)
	}
}

func TestDocumentDecode_20260902(t *testing.T) {
	file, err := os.Open("../test_data/Energy_Prices_202609012200-202609022200.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	doc, err := DecodeEnergyPricesXML(file)
	if err != nil {
		t.Fatal(err)
	}

	if len(doc.TimeSeries) != 1 {
		t.Fatalf("expected 1 time series, got %d", len(doc.TimeSeries))
	}

	period := doc.TimeSeries[0].Period
	if len(period.Points) != 96 {
		t.Fatalf("expected 96 points, got %d", len(period.Points))
	}

	// Verify position 1 (01:00-01:15) = 139.05
	if period.Points[0].PriceAmount != 139.05 {
		t.Errorf("pos 1 price = %f, want 139.05", period.Points[0].PriceAmount)
	}

	// Verify position 96 (00:45-01:00) = 26.08
	if period.Points[95].PriceAmount != 26.08 {
		t.Errorf("pos 96 price = %f, want 26.08", period.Points[95].PriceAmount)
	}
}

func TestDocumentDecode_20260901(t *testing.T) {
	file, err := os.Open("../test_data/Energy_Prices_202608312200-202609012200.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	doc, err := DecodeEnergyPricesXML(file)
	if err != nil {
		t.Fatal(err)
	}

	if len(doc.TimeSeries) != 1 {
		t.Fatalf("expected 1 time series, got %d", len(doc.TimeSeries))
	}

	period := doc.TimeSeries[0].Period
	if len(period.Points) != 96 {
		t.Fatalf("expected 96 points, got %d", len(period.Points))
	}

	// Verify position 1 (01:00-01:15) = 180.41
	if period.Points[0].PriceAmount != 180.41 {
		t.Errorf("pos 1 price = %f, want 180.41", period.Points[0].PriceAmount)
	}

	// Verify position 96 (00:45-01:00) = 73.23
	if period.Points[95].PriceAmount != 73.23 {
		t.Errorf("pos 96 price = %f, want 73.23", period.Points[95].PriceAmount)
	}
}
