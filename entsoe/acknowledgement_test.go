package entsoe

import (
	"errors"
	"strings"
	"testing"
)

const acknowledgementNoDataXML = `<?xml version="1.0" encoding="UTF-8"?>
<Acknowledgement_MarketDocument xmlns="urn:iec62325.351:tc57wg16:451-1:acknowledgementdocument:7:0">
	<mRID>920295cf-fbfb-4</mRID>
	<createdDateTime>2026-09-18T11:58:51Z</createdDateTime>
	<sender_MarketParticipant.mRID codingScheme="A01">10X1001A1001A450</sender_MarketParticipant.mRID>
	<sender_MarketParticipant.marketRole.type>A32</sender_MarketParticipant.marketRole.type>
	<receiver_MarketParticipant.mRID codingScheme="A01">10X1001A1001A450</receiver_MarketParticipant.mRID>
	<receiver_MarketParticipant.marketRole.type>A39</receiver_MarketParticipant.marketRole.type>
	<received_MarketDocument.createdDateTime>2026-09-18T11:58:51Z</received_MarketDocument.createdDateTime>
	<Reason>
		<code>999</code>
		<text>No matching data found for Data item ENERGY_PRICES [12.1.D] (10YLV-1001A00074, 10YLV-1001A00074) and interval 2026-09-19T00:00:00Z/2026-09-20T00:00:00Z.</text>
	</Reason>
</Acknowledgement_MarketDocument>`

func TestDecodeEnergyPricesXML_AcknowledgementNoMatchingData(t *testing.T) {
	doc, err := DecodeEnergyPricesXML(strings.NewReader(acknowledgementNoDataXML))
	if doc != nil {
		t.Fatalf("expected nil document, got %+v", doc)
	}
	if err == nil {
		t.Fatal("expected an error for an Acknowledgement_MarketDocument")
	}
	if !IsNoMatchingData(err) {
		t.Fatalf("expected ErrNoMatchingData, got %v", err)
	}

	var ackErr *AcknowledgementError
	if !errors.As(err, &ackErr) {
		t.Fatalf("expected *AcknowledgementError, got %T", err)
	}
	if ackErr.Document.MRID != "920295cf-fbfb-4" {
		t.Errorf("unexpected mRID: %q", ackErr.Document.MRID)
	}
	if ackErr.Document.SenderMarketParticipantMRID.Value != "10X1001A1001A450" {
		t.Errorf("unexpected sender mRID: %q", ackErr.Document.SenderMarketParticipantMRID.Value)
	}
	if len(ackErr.Document.Reason) != 1 || ackErr.Document.Reason[0].Code != "999" {
		t.Fatalf("unexpected reasons: %+v", ackErr.Document.Reason)
	}
	if !strings.Contains(err.Error(), "No matching data found") {
		t.Errorf("error message should carry the reason text, got %q", err.Error())
	}
}

func TestDecodeEnergyPricesXML_AcknowledgementOtherReason(t *testing.T) {
	xmlData := strings.Replace(acknowledgementNoDataXML, "<code>999</code>", "<code>A01</code>", 1)

	_, err := DecodeEnergyPricesXML(strings.NewReader(xmlData))
	if err == nil {
		t.Fatal("expected an error")
	}
	if IsNoMatchingData(err) {
		t.Fatal("reason code A01 must not be reported as ErrNoMatchingData")
	}
}

func TestDecodeEnergyPricesXML_UnknownRootElement(t *testing.T) {
	_, err := DecodeEnergyPricesXML(strings.NewReader(`<?xml version="1.0"?><Something_Else/>`))
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "<Something_Else>") {
		t.Errorf("error should name the unexpected root element, got %q", err.Error())
	}
}

func TestDecodeEnergyPricesXML_EmptyDocument(t *testing.T) {
	if _, err := DecodeEnergyPricesXML(strings.NewReader("")); err == nil {
		t.Fatal("expected an error for an empty document")
	}
}
