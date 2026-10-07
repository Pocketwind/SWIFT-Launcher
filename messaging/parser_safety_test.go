package messaging

import (
	"strings"
	"testing"
)

func TestMXDNBIC8(t *testing.T) {
	for _, dn := range []string{
		"cn=xxx,o=testkrse,o=swift",
		"cn=xxx,ou=payments,o=testkrse,o=swift",
		" CN = xxx , O = testkrse , O = swift ",
	} {
		bic, err := mxDNBIC8(dn)
		if err != nil || bic != "TESTKRSE" {
			t.Errorf("DN %q: BIC=%q err=%v", dn, bic, err)
		}
	}
	for _, dn := range []string{"", "cn=xxx", "cn=xxx,o=,o=swift", "cn=xxx,o=short,o=swift"} {
		if _, err := mxDNBIC8(dn); err == nil {
			t.Errorf("invalid BIC accepted: %q", dn)
		}
	}
}

func TestParsersRejectMalformedPayloadWithoutPanic(t *testing.T) {
	for _, payload := range []string{"", "unstructured text", ":missing-colon"} {
		if _, err := MTParser(payload); err == nil {
			t.Errorf("invalid MT payload accepted: %q", payload)
		}
	}
	for _, payload := range []string{"<Envelope/>", "<Envelope><AppHdr/></Envelope>", "<Envelope><Document/></Envelope>"} {
		if _, err := MXParser(payload); err == nil {
			t.Errorf("invalid MX payload accepted: %q", payload)
		}
	}
	mt, err := MTParser(":20:REFERENCE\n:79:First line\nContinuation")
	if err != nil || len(mt.Line) != 2 || mt.Line[1].Data != "First line\nContinuation" {
		t.Fatalf("valid multiline payload changed: mt=%+v err=%v", mt, err)
	}
}

func TestMessageMakersRejectMissingRequiredStructure(t *testing.T) {
	for _, messageType := range []string{"", "103", "fin.", "fin.abc", "fin.103.extra"} {
		if _, err := FINMessageMaker(MTDownload{Message: MTMessage{MessageType: messageType}}); err == nil {
			t.Errorf("invalid FIN type accepted: %q", messageType)
		}
	}
	for _, dn := range []string{"", "cn=xxx", "cn=xxx,o=", "broken,also-broken"} {
		if _, err := MXMessageMaker(MXDownload{Message: MXMessage{Requestor: dn}}); err == nil {
			t.Errorf("invalid MX DN accepted: %q", dn)
		}
		if _, err := MXReportMaker(MXReport{TransmissionReport: MXTransmissionReport{Message: MXMessage{Requestor: dn}}}); err == nil {
			t.Errorf("invalid MX report DN accepted: %q", dn)
		}
	}
	message := MXMessage{Requestor: "cn=xxx,o=testkrse,o=swift", Responder: "cn=xxx,o=otherbic,o=swift", Payload: "<Document/>"}
	if _, err := MXMessageMaker(MXDownload{Message: message}); err == nil {
		t.Fatal("MX message without Envelope accepted")
	}
	message.Payload = "<Envelope><AppHdr/><Document/></Envelope>"
	message.MessageType, message.Format = "pacs.008.001.08", "MX"
	result, err := MXMessageMaker(MXDownload{Message: message})
	if err != nil || !strings.Contains(result, "<RequestType>pacs.008.001.08</RequestType>") {
		t.Fatalf("MX request type lost: result=%q err=%v", result, err)
	}
}
