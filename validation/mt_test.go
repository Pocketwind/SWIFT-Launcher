package validation

import (
	"strings"
	"testing"
)

func mtTestValidator() *Validator {
	return &Validator{
		bic8:       map[string]struct{}{"BANKKRSE": {}, "CHASUS33": {}},
		bic11:      map[string]struct{}{"BANKKRSEXXX": {}, "CHASUS33XXX": {}},
		currencies: map[string]struct{}{"KRW": {}, "USD": {}, "EUR": {}, "NAD": {}},
		countries:  map[string]struct{}{"KR": {}, "US": {}, "GB": {}, "DE": {}},
	}
}

func mtTestMessage(body string) string {
	return "{1:F01BANKKRSEAXXX0000000000}{2:I103CHASUS33XXXXN}{4:\n" + body + "\n-}"
}

func TestMTValidatesReferenceCodesInTheirFields(t *testing.T) {
	const body = `:20:PAYMENT
:32A:261027USD100,
:33B:KRW100000,
:50F:ARNU/KR/12345
1/CUSTOMER
3/KR/SEOUL
3/CONTINUED CITY INFORMATION
5/US/NEW YORK
6/GB/ISSUER/12345
:52A:/ACCOUNT
BANKKRSE
:57A:CHASUS33XXX
:59F:/12345
1/BENEFICIARY
3/US/NEW YORK
:71F:USD1,
:77B:/ORDERRES/KR
/BENEFRES/US//ADDITIONAL INFO
:70:USD ZZZ ZZ UNKNOWNBIC
:52D:UNKNOWN BANK
SOME ADDRESS ZZ`
	if err := mtTestValidator().ValidateMT(mtTestMessage(body)); err != nil {
		t.Fatal(err)
	}
}

func TestMTReportsAllInvalidReferenceCodesWithLocations(t *testing.T) {
	message := mtTestMessage(":20:PAYMENT\n:32A:261027ZZZ100,\n:57A:UNKNOWN1\n:59F:/123\n1/NAME\n3/ZZ/CITY\n:77B:/ORDERRES/YY")
	err := mtTestValidator().ValidateMT(message)
	if err == nil {
		t.Fatal("invalid codes accepted")
	}
	for _, want := range []string{"32A", "ZZZ", "57A", "UNKNOWN1", "59F", "ZZ", "77B", "YY"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestMTHeaderUsesExactBranchAndConvertsLT(t *testing.T) {
	v := mtTestValidator()
	if err := v.ValidateMT(mtTestMessage(":20:REF")); err != nil {
		t.Fatal(err)
	}
	message := strings.Replace(mtTestMessage(":20:REF"), "BANKKRSEAXXX", "BANKKRSEA123", 1)
	err := v.ValidateMT(message)
	if err == nil || !strings.Contains(err.Error(), "BANKKRSE123") || !strings.Contains(err.Error(), "Sender") {
		t.Fatalf("unknown LT branch accepted: %v", err)
	}
	message = strings.Replace(mtTestMessage(":20:REF"), "CHASUS33XXXX", "CHASUS33X123", 1)
	if err := v.ValidateMT(message); err == nil || !strings.Contains(err.Error(), "Receiver") {
		t.Fatalf("unknown receiver branch accepted: %v", err)
	}
}

func TestMTBICEmbeddedCountryIsChecked(t *testing.T) {
	v := mtTestValidator()
	v.bic8["BANKZZSE"] = struct{}{}
	err := v.ValidateMT(mtTestMessage(":20:REF\n:57A:BANKZZSE"))
	if err == nil || !strings.Contains(err.Error(), "ZZ") {
		t.Fatalf("BIC with unlisted country accepted: %v", err)
	}
}

func TestMTQualifiedSecuritiesReferenceFields(t *testing.T) {
	const body = `:20C::SEME//REF
:95P::PSET//BANKKRSE
:11A::DENO//USD
:19A::SETT//NUSD10,
:19A::DEAL//NAD20,
:19A::DEAL//NNAD20,
:19B::ENTL//EUR11,
:90B::DEAL//ACTU/USD10,
:90F::DEAL//ACTU/USD10,/UNIT/1,
:90E::OFFR//UKWN
:92B::EXCH//USD/EUR/1,12
:92F::INTR//USD1,
:94C::COUN//KR
:94F::SAFE//NCSD/CHASUS33`
	if err := mtTestValidator().ValidateMT(mtTestMessage(body)); err != nil {
		t.Fatal(err)
	}
	for _, replacement := range []struct{ old, bad string }{
		{"DENO//USD", "DENO//ZZZ"},
		{"SETT//NUSD", "SETT//NZZZ"},
		{"ACTU/USD10,", "ACTU/ZZZ10,"},
		{"USD/EUR/", "USD/ZZZ/"},
		{"COUN//KR", "COUN//ZZ"},
		{"NCSD/CHASUS33", "NCSD/BADCODE1"},
	} {
		t.Run(replacement.bad, func(t *testing.T) {
			message := mtTestMessage(strings.Replace(body, replacement.old, replacement.bad, 1))
			if err := mtTestValidator().ValidateMT(message); err == nil {
				t.Fatal("invalid code accepted")
			}
		})
	}
}

func TestMTStatementBalancesUseCurrencyAfterDate(t *testing.T) {
	const body = `:20:STATEMENT
:60F:C261026USD100,
:62F:C261027EUR90,
:64:C261027KRW100000,
:65:D261028USD10,
:34F:USDD1,`
	if err := mtTestValidator().ValidateMT(mtTestMessage(body)); err != nil {
		t.Fatal(err)
	}
	if err := mtTestValidator().ValidateMT(mtTestMessage(strings.Replace(body, "261028USD", "261028ZZZ", 1))); err == nil {
		t.Fatal("invalid statement currency accepted")
	}
}

func TestMTOutputHeaderFindsSenderInMIR(t *testing.T) {
	message := "{1:F01CHASUS33AXXX0000000000}{2:O1031200261027BANKKRSEAXXX00000000002610271200N}{4:\n:20:REF\n-}"
	if err := mtTestValidator().ValidateMT(message); err != nil {
		t.Fatal(err)
	}
	message = strings.Replace(message, "BANKKRSEAXXX", "BANKKRSEA123", 1)
	if err := mtTestValidator().ValidateMT(message); err == nil || !strings.Contains(err.Error(), "Sender") {
		t.Fatalf("unknown output sender accepted: %v", err)
	}
}

func TestMTRejectsMissingAndTruncatedCodedFields(t *testing.T) {
	for _, message := range []string{
		"", "{1:F01}{2:I103}", mtTestMessage(":20:REF\n:32A:USD"),
		mtTestMessage(":20:REF\n:57A:BAD"), mtTestMessage(":20:REF\n:95P::PSET/BANKKRSE"),
		mtTestMessage(":20:REF\n:59F:/123\n3//CITY"),
		mtTestMessage(":20:REF\n:92B::EXCH//USD"),
		strings.Replace(mtTestMessage(":20:REF"), "-}", "", 1),
	} {
		if err := mtTestValidator().ValidateMT(message); err == nil {
			t.Errorf("invalid message accepted: %q", message)
		}
	}
}
