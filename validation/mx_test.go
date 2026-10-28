package validation

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const mxTestHeaderType = "head.001.001.02"
const mxTestBodyType = "pacs.008.001.08"

func mxTestValidator(t *testing.T) *Validator {
	t.Helper()
	v := &Validator{
		bic8: map[string]struct{}{"TESTKRSE": {}}, bic11: map[string]struct{}{"TESTKRSEXXX": {}},
		currencies: map[string]struct{}{"USD": {}, "KRW": {}}, countries: map[string]struct{}{"KR": {}, "US": {}},
		schemaDir: t.TempDir(),
	}
	mxWriteSchema(t, v, mxTestHeaderType, mxHeaderSchema(""))
	mxWriteSchema(t, v, mxTestBodyType, mxBodySchema())
	return v
}

func mxWriteSchema(t *testing.T, v *Validator, name, schema string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(v.schemaDir, name+".xsd"), []byte(schema), 0600); err != nil {
		t.Fatal(err)
	}
}

func mxHeaderSchema(extra string) string {
	return `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="` + iso20022Namespace + mxTestHeaderType + `" elementFormDefault="qualified">
	<xs:element name="AppHdr"><xs:complexType><xs:sequence>
	<xs:element name="BizSvc" type="xs:string" minOccurs="0"/>
	<xs:element name="BICFI" type="xs:string" minOccurs="0"/>` + extra + `
	</xs:sequence></xs:complexType></xs:element></xs:schema>`
}

func mxBodySchema() string {
	return `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:p="` + iso20022Namespace + mxTestBodyType + `" targetNamespace="` + iso20022Namespace + mxTestBodyType + `" elementFormDefault="qualified">
	<xs:simpleType name="AnyBICDec2014Identifier"><xs:restriction base="xs:string"/></xs:simpleType>
	<xs:simpleType name="CountryCode"><xs:restriction base="xs:string"/></xs:simpleType>
	<xs:simpleType name="ActiveCurrencyCode"><xs:restriction base="xs:string"/></xs:simpleType>
	<xs:element name="Document"><xs:complexType><xs:sequence>
	<xs:element name="Payment"><xs:complexType><xs:sequence>
	<xs:element name="BICFI" type="xs:string"/>
	<xs:element name="AnyBIC" type="xs:string"/>
	<xs:element name="Ctry" type="xs:string"/>
	<xs:element name="Ccy" type="xs:string"/>
	<xs:element name="Amt"><xs:complexType><xs:simpleContent><xs:extension base="xs:decimal"><xs:attribute name="Ccy" type="xs:string" use="required"/></xs:extension></xs:simpleContent></xs:complexType></xs:element>
	<xs:element name="Id" type="p:AnyBICDec2014Identifier"/>
	<xs:element name="Nationality" type="p:CountryCode"/>
	<xs:element name="SettlementCurrency" type="p:ActiveCurrencyCode"/>
	</xs:sequence></xs:complexType></xs:element>
	</xs:sequence></xs:complexType></xs:element></xs:schema>`
}

// Prefix declarations deliberately belong to an ancestor of the validated roots.
func mxTestPayload(service string) string {
	serviceElement := ""
	if service != "" {
		serviceElement = "<h:BizSvc>" + service + "</h:BizSvc>"
	}
	return `<DataPDU xmlns:h="` + iso20022Namespace + mxTestHeaderType + `" xmlns:p="` + iso20022Namespace + mxTestBodyType + `">
<Body><h:AppHdr>` + serviceElement + `<h:BICFI>TESTKRSE</h:BICFI></h:AppHdr>
<p:Document><p:Payment>
<p:BICFI>TESTKRSEXXX</p:BICFI><p:AnyBIC>TESTKRSE</p:AnyBIC>
<p:Ctry>KR</p:Ctry><p:Ccy>USD</p:Ccy><p:Amt Ccy="KRW">123.45</p:Amt>
<p:Id>TESTKRSE</p:Id><p:Nationality>US</p:Nationality><p:SettlementCurrency>USD</p:SettlementCurrency>
</p:Payment></p:Document></Body></DataPDU>`
}

func TestValidateMXPreservesInheritedNamespacesAndChecksBothRoots(t *testing.T) {
	v := mxTestValidator(t)
	if err := v.ValidateMX(mxTestPayload("")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ from, to, want string }{
		{"<p:Amt Ccy=\"KRW\">123.45", "<p:Amt Ccy=\"KRW\">not-a-number", "Document XSD"},
		{"<h:BICFI>TESTKRSE</h:BICFI>", "<h:Unexpected/>", "AppHdr XSD"},
	} {
		err := v.ValidateMX(strings.Replace(mxTestPayload(""), tc.from, tc.to, 1))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("want %s failure, got %v", tc.want, err)
		}
	}
}

func TestValidateMXReportsEveryReferenceWithLocation(t *testing.T) {
	v := mxTestValidator(t)
	message := strings.NewReplacer(
		"<h:BICFI>TESTKRSE</h:BICFI>", "<h:BICFI>SHORT</h:BICFI>",
		"<p:AnyBIC>TESTKRSE</p:AnyBIC>", "<p:AnyBIC>NONEKRSE</p:AnyBIC>",
		"<p:Ctry>KR</p:Ctry>", "<p:Ctry>ZZ</p:Ctry>",
		"<p:Ccy>USD</p:Ccy>", "<p:Ccy>ZZZ</p:Ccy>",
		`Ccy="KRW"`, `Ccy="BAD"`,
		"<p:Id>TESTKRSE</p:Id>", "<p:Id>OTHERKRS</p:Id>",
		"<p:Nationality>US</p:Nationality>", "<p:Nationality>XX</p:Nationality>",
		"<p:SettlementCurrency>USD</p:SettlementCurrency>", "<p:SettlementCurrency>XXX</p:SettlementCurrency>",
	).Replace(mxTestPayload(""))
	err := v.ValidateMX(message)
	if err == nil {
		t.Fatal("invalid references accepted")
	}
	for _, want := range []string{"SHORT", "NONEKRSE", "ZZ", "ZZZ", "BAD", "OTHERKRS", "XX", "XXX", "/@Ccy", "line", "/Id", "/Nationality", "/SettlementCurrency"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q from %v", want, err)
		}
	}
}

func TestValidateMXDoesNotTreatComplexCcyAsCurrency(t *testing.T) {
	v := mxTestValidator(t)
	schema := strings.Replace(mxBodySchema(), `<xs:element name="Ccy" type="xs:string"/>`, `<xs:element name="Ccy" type="p:ProprietaryReason4"/>`, 1)
	schema = strings.Replace(schema, `</xs:schema>`, `
	<xs:complexType name="ProprietaryReason4"><xs:sequence>
	<xs:element name="Rsn" type="xs:string" minOccurs="0"/>
	<xs:element name="AddtlRsnInf" type="xs:string" minOccurs="0"/>
	</xs:sequence></xs:complexType></xs:schema>`, 1)
	mxWriteSchema(t, v, mxTestBodyType, schema)
	for _, ccy := range []string{
		`<p:Ccy/>`,
		`<p:Ccy><p:Rsn>free text reason</p:Rsn><p:AddtlRsnInf>more reason text</p:AddtlRsnInf></p:Ccy>`,
	} {
		message := strings.Replace(mxTestPayload(""), `<p:Ccy>USD</p:Ccy>`, ccy, 1)
		if err := v.ValidateMX(message); err != nil {
			t.Errorf("XSD-valid reason container %s rejected: %v", ccy, err)
		}
	}
}

func TestValidateMXDoesNotOverrideNamedNonCodeTypes(t *testing.T) {
	v := mxTestValidator(t)
	schema := strings.Replace(mxBodySchema(), `<xs:element name="Ccy" type="xs:string"/>`, `<xs:element name="Ccy" type="p:Max35Text"/>`, 1)
	schema = strings.Replace(schema, `</xs:schema>`, `<xs:simpleType name="Max35Text"><xs:restriction base="xs:string"/></xs:simpleType></xs:schema>`, 1)
	mxWriteSchema(t, v, mxTestBodyType, schema)
	message := strings.Replace(mxTestPayload(""), `<p:Ccy>USD</p:Ccy>`, `<p:Ccy>free text</p:Ccy>`, 1)
	if err := v.ValidateMX(message); err != nil {
		t.Fatalf("named non-code type treated as currency: %v", err)
	}
}

func TestValidateMXSelectsBizSvcAndBAHSchemas(t *testing.T) {
	const service = "swift.cbprplus.03"
	v := mxTestValidator(t)
	message := mxTestPayload(service)
	if err := v.ValidateMX(message); err == nil || !strings.Contains(err.Error(), mxTestBodyType+"-"+service+".xsd") {
		t.Fatalf("missing service schema must not fall back to base: %v", err)
	}
	mxWriteSchema(t, v, mxTestBodyType+"-"+service, mxBodySchema())
	if err := v.ValidateMX(message); err != nil {
		t.Fatalf("generic header fallback without BAH: %v", err)
	}
	mxWriteSchema(t, v, mxTestBodyType+"-"+service+"_BAH", mxHeaderSchema(`<xs:element name="RequiredServiceHeader" type="xs:string"/>`))
	if err := v.ValidateMX(message); err == nil || !strings.Contains(err.Error(), "_BAH.xsd") {
		t.Fatalf("service BAH restrictions were ignored: %v", err)
	}
}

func TestValidateMXRejectsAmbiguousRootsAndUnsafeSchemaNames(t *testing.T) {
	v := mxTestValidator(t)
	for _, message := range []string{
		`<Envelope/>`,
		strings.Replace(mxTestPayload(""), "</Body>", "<h:AppHdr/></Body>", 1),
		strings.Replace(mxTestPayload(""), "</Body>", "<p:Document/></Body>", 1),
		strings.ReplaceAll(mxTestPayload(""), iso20022Namespace+mxTestBodyType, iso20022Namespace+"../../outside"),
		mxTestPayload("../../outside"),
		mxTestPayload(`C:\outside`),
	} {
		if err := v.ValidateMX(message); err == nil {
			t.Errorf("invalid message accepted: %s", message)
		}
	}
}

func TestValidateMXSharesCachedSchemasAcrossCollectors(t *testing.T) {
	v := mxTestValidator(t)
	var wg sync.WaitGroup
	errors := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := v.ValidateMX(mxTestPayload("")); err != nil {
				errors <- fmt.Errorf("concurrent validation: %w", err)
			}
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	if len(v.schemas) != 2 {
		t.Fatalf("schema cache size = %d, want 2", len(v.schemas))
	}
}
