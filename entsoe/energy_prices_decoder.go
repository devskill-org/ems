package entsoe

import (
	"encoding/xml"
	"fmt"
	"io"
	"time"
)

// PublicationMarketData represents the root element of the XML
type PublicationMarketData struct {
	XMLName                           xml.Name              `xml:"Publication_MarketDocument"`
	Xmlns                             string                `xml:"xmlns,attr"`
	MRID                              string                `xml:"mRID"`
	RevisionNumber                    int                   `xml:"revisionNumber"`
	Type                              string                `xml:"type"`
	SenderMarketParticipantMRID       MarketParticipantMRID `xml:"sender_MarketParticipant.mRID"`
	SenderMarketParticipantRoleType   string                `xml:"sender_MarketParticipant.marketRole.type"`
	ReceiverMarketParticipantMRID     MarketParticipantMRID `xml:"receiver_MarketParticipant.mRID"`
	ReceiverMarketParticipantRoleType string                `xml:"receiver_MarketParticipant.marketRole.type"`
	CreatedDateTime                   string                `xml:"createdDateTime"`
	PeriodTimeInterval                TimeInterval          `xml:"period.timeInterval"`
	TimeSeries                        []TimeSeries          `xml:"TimeSeries"`
}

// MarketParticipantMRID represents market participant with coding scheme
type MarketParticipantMRID struct {
	CodingScheme string `xml:"codingScheme,attr"`
	Value        string `xml:",chardata"`
}

// TimeInterval represents a time interval with start and end
type TimeInterval struct {
	Start time.Time `xml:"start"`
	End   time.Time `xml:"end"`
}

// UnmarshalXML implements custom XML unmarshaling for TimeInterval
func (ti *TimeInterval) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var aux struct {
		Start string `xml:"start"`
		End   string `xml:"end"`
	}

	if err := d.DecodeElement(&aux, &start); err != nil {
		return err
	}

	var err error
	ti.Start, err = parseTimeString(aux.Start)
	if err != nil {
		return fmt.Errorf("error parsing start time: %v", err)
	}

	ti.End, err = parseTimeString(aux.End)
	if err != nil {
		return fmt.Errorf("error parsing end time: %v", err)
	}

	return nil
}

// parseTimeString parses time strings in the format used by ENTSO-E XML
func parseTimeString(timeStr string) (time.Time, error) {
	// Try RFC3339 format first (2006-01-02T15:04:05Z07:00)
	if t, err := time.Parse(time.RFC3339, timeStr); err == nil {
		return t, nil
	}

	// Try simplified format without seconds (2025-09-04T22:00Z)
	if t, err := time.Parse("2006-01-02T15:04Z", timeStr); err == nil {
		return t, nil
	}

	// Try format with timezone offset but no seconds (2025-09-04T22:00+02:00)
	if t, err := time.Parse("2006-01-02T15:04Z07:00", timeStr); err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("unable to parse time string: %s", timeStr)
}

// TimeSeries represents the time series data
type TimeSeries struct {
	MRID                        string                `xml:"mRID"`
	AuctionType                 string                `xml:"auction.type"`
	BusinessType                string                `xml:"businessType"`
	InDomainMRID                MarketParticipantMRID `xml:"in_Domain.mRID"`
	OutDomainMRID               MarketParticipantMRID `xml:"out_Domain.mRID"`
	ContractMarketAgreementType string                `xml:"contract_MarketAgreement.type"`
	CurrencyUnitName            string                `xml:"currency_Unit.name"`
	PriceMeasureUnitName        string                `xml:"price_Measure_Unit.name"`
	CurveType                   string                `xml:"curveType"`
	Period                      Period                `xml:"Period"`
}

// Period represents a period with time interval, resolution and points
type Period struct {
	TimeInterval TimeInterval  `xml:"timeInterval"`
	Resolution   time.Duration `xml:"resolution"`
	Points       []Point       `xml:"Point"`
}

// UnmarshalXML implements custom XML unmarshaling for Period
func (p *Period) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var aux struct {
		TimeInterval TimeInterval `xml:"timeInterval"`
		Resolution   string       `xml:"resolution"`
		Points       []Point      `xml:"Point"`
	}

	if err := d.DecodeElement(&aux, &start); err != nil {
		return err
	}

	p.TimeInterval = aux.TimeInterval
	p.Points = aux.Points

	var err error
	p.Resolution, err = parseISO8601Duration(aux.Resolution)
	if err != nil {
		return fmt.Errorf("error parsing resolution: %v", err)
	}

	return nil
}

// parseISO8601Duration parses ISO 8601 duration format to time.Duration
func parseISO8601Duration(duration string) (time.Duration, error) {
	// Handle common ISO 8601 duration formats used in ENTSO-E
	// Format: P[n]Y[n]M[n]DT[n]H[n]M[n]S or PT[n]H[n]M[n]S

	if len(duration) < 1 || duration[0] != 'P' {
		return 0, fmt.Errorf("invalid ISO 8601 duration format: %s", duration)
	}

	duration = duration[1:] // Remove 'P'

	// Handle empty duration after P
	if len(duration) == 0 {
		return 0, nil
	}

	var totalDuration time.Duration

	// Check if we have time component (starts with 'T')
	timeIndex := -1
	for i, char := range duration {
		if char == 'T' {
			timeIndex = i
			break
		}
	}

	// Parse date part if exists (before 'T')
	if timeIndex > 0 {
		datePart := duration[:timeIndex]
		var err error
		totalDuration, err = parseDatePart(datePart)
		if err != nil {
			return 0, err
		}
		duration = duration[timeIndex+1:] // Remove date part and 'T'
	} else if timeIndex == 0 {
		duration = duration[1:] // Remove 'T'
	} else if timeIndex == -1 && len(duration) > 0 {
		// No 'T' found, this is date-only duration
		dateDuration, err := parseDatePart(duration)
		if err != nil {
			return 0, err
		}
		return dateDuration, nil
	}

	// Parse time part
	timeDuration, err := parseTimePart(duration)
	if err != nil {
		return 0, err
	}

	return totalDuration + timeDuration, nil
}

// parseDatePart parses the date portion of ISO 8601 duration (years, months, days)
func parseDatePart(datePart string) (time.Duration, error) {
	var duration time.Duration
	var numStr string

	for _, char := range datePart {
		if char >= '0' && char <= '9' {
			numStr += string(char)
		} else {
			if numStr == "" {
				continue
			}

			num := 0
			for _, digit := range numStr {
				num = num*10 + int(digit-'0')
			}

			switch char {
			case 'Y':
				duration += time.Duration(num) * 365 * 24 * time.Hour // Approximate
			case 'M':
				duration += time.Duration(num) * 30 * 24 * time.Hour // Approximate
			case 'D':
				duration += time.Duration(num) * 24 * time.Hour
			default:
				return 0, fmt.Errorf("unknown date unit: %c", char)
			}
			numStr = ""
		}
	}

	return duration, nil
}

// parseTimePart parses the time portion of ISO 8601 duration (hours, minutes, seconds)
func parseTimePart(timePart string) (time.Duration, error) {
	var duration time.Duration
	var numStr string

	for _, char := range timePart {
		if char >= '0' && char <= '9' || char == '.' {
			numStr += string(char)
		} else {
			if numStr == "" {
				continue
			}

			// Parse number (could be float for seconds)
			var num float64
			var err error

			// Simple float parsing
			if char == 'S' {
				// For seconds, we might have decimal places
				num, err = parseFloat(numStr)
			} else {
				// For hours and minutes, always integer
				intNum := 0
				for _, digit := range numStr {
					if digit >= '0' && digit <= '9' {
						intNum = intNum*10 + int(digit-'0')
					}
				}
				num = float64(intNum)
			}

			if err != nil {
				return 0, err
			}

			switch char {
			case 'H':
				duration += time.Duration(num) * time.Hour
			case 'M':
				duration += time.Duration(num) * time.Minute
			case 'S':
				duration += time.Duration(num * float64(time.Second))
			default:
				return 0, fmt.Errorf("unknown time unit: %c", char)
			}
			numStr = ""
		}
	}

	return duration, nil
}

// parseFloat parses a simple float string
func parseFloat(s string) (float64, error) {
	var result float64
	var decimal float64
	var divisor float64 = 1
	afterDecimal := false
	dotCount := 0

	for _, char := range s {
		if char == '.' {
			dotCount++
			if dotCount > 1 {
				return 0, fmt.Errorf("multiple dots in float: %s", s)
			}
			afterDecimal = true
			continue
		}
		if char >= '0' && char <= '9' {
			digit := float64(char - '0')
			if afterDecimal {
				divisor *= 10
				decimal = decimal*10 + digit
			} else {
				result = result*10 + digit
			}
		} else {
			return 0, fmt.Errorf("invalid character in float: %c", char)
		}
	}

	return result + decimal/divisor, nil
}

// Point represents a price point with position and amount
type Point struct {
	Position    int     `xml:"position"`
	PriceAmount float64 `xml:"price.amount"`
}

// ParseDateTime parses the XML datetime format to Go time.Time
func ParseDateTime(dateStr string) (time.Time, error) {
	return parseTimeString(dateStr)
}

// LookupPriceByTime searches all TimeSeries in the market data for a price at the given time.
// Returns the first matching price found and true, or 0 and false if no price is found.
// The time lookup checks if the given time falls within any interval in any TimeSeries.
func (pmd *PublicationMarketData) LookupPriceByTime(t time.Time) (float64, bool) {
	for _, timeSeries := range pmd.TimeSeries {
		if price, found := timeSeries.Period.GetPriceByTime(t); found {
			return price, true
		}
	}
	return 0, false
}

// GetPriceByTime returns the price for a specific time.
// The price corresponds to the interval that contains the given time.
// For example, if the period starts at 22:00 with hourly resolution:
// - 22:00-22:59 maps to position 1
// - 23:00-23:59 maps to position 2
// Returns (price, true) if found, (0, false) if the time is outside the period.
func (p *Period) GetPriceByTime(t time.Time) (float64, bool) {
	position := p.calculatePosition(t)
	if position <= 0 {
		return 0, false
	}

	var checked *Point

	for _, point := range p.Points {
		if point.Position == position {
			return point.PriceAmount, true
		}
		if point.Position > position && checked != nil {
			return checked.PriceAmount, true
		}
		checked = &point
	}
	return 0, false
}

// calculatePosition calculates the 1-based position for a given time.
// Position 1 corresponds to the first interval [start, start+resolution).
// Returns 0 if the time is outside the valid period range.
func (p *Period) calculatePosition(t time.Time) int {
	// Calculate the time difference from period start
	timeDiff := t.Sub(p.TimeInterval.Start)

	// Handle negative time differences (time before period start)
	if timeDiff < 0 {
		return 0
	}

	// Handle time after period end
	if t.After(p.TimeInterval.End) || t.Equal(p.TimeInterval.End) {
		return 0
	}

	// Calculate position (1-based) based on resolution
	// Position 1 covers [start, start+resolution)
	// Position 2 covers [start+resolution, start+2*resolution)
	// etc.
	positionZeroBased := int(timeDiff.Nanoseconds() / p.Resolution.Nanoseconds())
	return positionZeroBased + 1
}

// GetTimeRangeForPosition returns the start and end time for a given position.
// Position must be >= 1. Returns the time interval [start, end) that corresponds
// to the position within this period. The 'valid' return value indicates whether
// the position exists within the period boundaries.
func (p *Period) GetTimeRangeForPosition(position int) (start, end time.Time, valid bool) {
	if position < 1 {
		return time.Time{}, time.Time{}, false
	}

	start = p.TimeInterval.Start.Add(time.Duration(position-1) * p.Resolution)
	end = start.Add(p.Resolution)

	// Check if the position is within the period
	if start.After(p.TimeInterval.End) || start.Equal(p.TimeInterval.End) {
		return time.Time{}, time.Time{}, false
	}

	// Adjust end time if it goes beyond period end
	if end.After(p.TimeInterval.End) {
		end = p.TimeInterval.End
	}

	return start, end, true
}

// DecodeEnergyPricesXML decodes the XML file and returns the parsed data.
//
// The ENTSO-E Transparency Platform may answer a Publication_MarketDocument request
// with an Acknowledgement_MarketDocument (even with HTTP 200), for example when the
// day-ahead prices for the requested interval have not been published yet. In that
// case an *AcknowledgementError is returned, which unwraps to ErrNoMatchingData for
// reason code 999.
func DecodeEnergyPricesXML(file io.Reader) (*PublicationMarketData, error) {
	decoder := xml.NewDecoder(file)

	// Find the root element so we can dispatch on the document type.
	var root *xml.StartElement
	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				return nil, fmt.Errorf("error parsing XML: empty document, no root element found")
			}
			return nil, fmt.Errorf("error parsing XML: %v", err)
		}
		if start, ok := token.(xml.StartElement); ok {
			root = &start
			break
		}
	}

	switch root.Name.Local {
	case "Publication_MarketDocument":
		var doc PublicationMarketData
		if err := decoder.DecodeElement(&doc, root); err != nil {
			return nil, fmt.Errorf("error parsing XML: %v", err)
		}
		return &doc, nil

	case "Acknowledgement_MarketDocument":
		var ack AcknowledgementMarketDocument
		if err := decoder.DecodeElement(&ack, root); err != nil {
			return nil, fmt.Errorf("error parsing Acknowledgement_MarketDocument: %v", err)
		}
		return nil, &AcknowledgementError{Document: &ack}

	default:
		return nil, fmt.Errorf("error parsing XML: expected element type <Publication_MarketDocument> but have <%s>", root.Name.Local)
	}
}
