package entsoe

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
)

// ReasonCodeNoMatchingData is the ENTSO-E reason code returned when no data is
// available for the requested data item and interval (e.g. day-ahead prices for
// tomorrow have not been published yet).
const ReasonCodeNoMatchingData = "999"

// ErrNoMatchingData is returned when ENTSO-E replies with an
// Acknowledgement_MarketDocument carrying reason code 999 ("No matching data found").
// This is an expected, non-fatal condition for not-yet-published periods.
var ErrNoMatchingData = errors.New("entsoe: no matching data found")

// Reason represents a <Reason> element of an Acknowledgement_MarketDocument.
type Reason struct {
	Code string `xml:"code"`
	Text string `xml:"text"`
}

// AcknowledgementMarketDocument represents the Acknowledgement_MarketDocument that
// the ENTSO-E Transparency Platform returns instead of a Publication_MarketDocument
// when a request cannot be served (no data, invalid parameters, quota exceeded, ...).
// Note: it is returned with HTTP 200 in several cases, so the status code alone is
// not sufficient to detect it.
type AcknowledgementMarketDocument struct {
	XMLName                           xml.Name              `xml:"Acknowledgement_MarketDocument"`
	Xmlns                             string                `xml:"xmlns,attr"`
	MRID                              string                `xml:"mRID"`
	CreatedDateTime                   string                `xml:"createdDateTime"`
	SenderMarketParticipantMRID       MarketParticipantMRID `xml:"sender_MarketParticipant.mRID"`
	SenderMarketParticipantRoleType   string                `xml:"sender_MarketParticipant.marketRole.type"`
	ReceiverMarketParticipantMRID     MarketParticipantMRID `xml:"receiver_MarketParticipant.mRID"`
	ReceiverMarketParticipantRoleType string                `xml:"receiver_MarketParticipant.marketRole.type"`
	ReceivedDocumentCreatedDateTime   string                `xml:"received_MarketDocument.createdDateTime"`
	Reason                            []Reason              `xml:"Reason"`
}

// AcknowledgementError wraps an AcknowledgementMarketDocument as an error so that
// callers can inspect the reason codes returned by ENTSO-E.
type AcknowledgementError struct {
	Document *AcknowledgementMarketDocument
}

// Error implements the error interface.
func (e *AcknowledgementError) Error() string {
	if e == nil || e.Document == nil {
		return "entsoe: acknowledgement received"
	}

	parts := make([]string, 0, len(e.Document.Reason))
	for _, r := range e.Document.Reason {
		parts = append(parts, fmt.Sprintf("%s: %s", r.Code, strings.TrimSpace(r.Text)))
	}
	if len(parts) == 0 {
		return "entsoe: acknowledgement received without reason"
	}

	return "entsoe: acknowledgement received (" + strings.Join(parts, "; ") + ")"
}

// Unwrap allows errors.Is(err, ErrNoMatchingData) to succeed for reason code 999.
func (e *AcknowledgementError) Unwrap() error {
	if e != nil && e.Document != nil && e.Document.HasReasonCode(ReasonCodeNoMatchingData) {
		return ErrNoMatchingData
	}
	return nil
}

// HasReasonCode reports whether the acknowledgement contains the given reason code.
func (d *AcknowledgementMarketDocument) HasReasonCode(code string) bool {
	if d == nil {
		return false
	}
	for _, r := range d.Reason {
		if r.Code == code {
			return true
		}
	}
	return false
}

// IsNoMatchingData reports whether err represents an ENTSO-E "no matching data found"
// acknowledgement.
func IsNoMatchingData(err error) bool {
	return errors.Is(err, ErrNoMatchingData)
}
