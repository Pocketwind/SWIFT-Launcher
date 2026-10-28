package xmlutil

import (
	"strings"
	"testing"

	"github.com/antchfx/xmlquery"
)

func TestOutputStandaloneXMLPreservesInheritedNamespaces(t *testing.T) {
	const source = `<Root xmlns="urn:root" xmlns:p="urn:outer" xmlns:q="urn:qualifier" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"><Body xmlns="urn:body" xmlns:p="urn:inner"><p:Document q:kind="q:Type" xsi:type="q:DocumentType"><Value>  keep spaces  </Value><Child xmlns=""><p:More/></Child></p:Document></Body></Root>`
	doc, err := xmlquery.Parse(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	node := xmlquery.FindOne(doc, "//*[local-name()='Document']")
	before := doc.OutputXML(true)
	attrCount := len(node.Attr)
	output := OutputStandaloneXML(node)
	standalone, err := xmlquery.Parse(strings.NewReader(output))
	if err != nil {
		t.Fatalf("standalone XML cannot be parsed: %v; %s", err, output)
	}
	root := xmlquery.FindOne(standalone, "/*")
	if root.NamespaceURI != "urn:inner" || root.SelectAttr("q:kind") != "q:Type" || root.SelectAttr("xsi:type") != "q:DocumentType" {
		t.Fatalf("namespace or QName attribute lost: %s", output)
	}
	value := xmlquery.FindOne(standalone, "//*[local-name()='Value']")
	if value.NamespaceURI != "urn:body" || value.InnerText() != "  keep spaces  " {
		t.Fatalf("default namespace or text changed: %s", output)
	}
	child := xmlquery.FindOne(standalone, "//*[local-name()='Child']")
	more := xmlquery.FindOne(standalone, "//*[local-name()='More']")
	if child.NamespaceURI != "" || more.NamespaceURI != "urn:inner" {
		t.Fatalf("descendant namespace reset changed: %s", output)
	}
	if !strings.Contains(output, `xmlns:q="urn:qualifier"`) || !strings.Contains(output, `xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"`) {
		t.Fatalf("attribute namespace declaration lost: %s", output)
	}
	if len(node.Attr) != attrCount || doc.OutputXML(true) != before {
		t.Fatal("serializing a subtree mutated the original tree")
	}
}

func TestOutputStandaloneXMLKeepsLocalDeclarations(t *testing.T) {
	for _, source := range []string{
		`<Root xmlns="urn:outer" xmlns:p="urn:outer"><Body xmlns="urn:body" xmlns:p="urn:body"><Document xmlns="urn:local" xmlns:p="urn:local"><p:Child/></Document></Body></Root>`,
		`<Root xmlns="urn:outer" xmlns:p="urn:outer"><Body xmlns="" xmlns:p="urn:local"><Document><p:Child/></Document></Body></Root>`,
	} {
		doc, err := xmlquery.Parse(strings.NewReader(source))
		if err != nil {
			t.Fatal(err)
		}
		node := xmlquery.FindOne(doc, "//*[local-name()='Document']")
		output := OutputStandaloneXML(node)
		standalone, err := xmlquery.Parse(strings.NewReader(output))
		if err != nil {
			t.Fatal(err)
		}
		root := xmlquery.FindOne(standalone, "/*")
		child := xmlquery.FindOne(standalone, "//*[local-name()='Child']")
		if root.NamespaceURI != node.NamespaceURI || child.NamespaceURI != "urn:local" {
			t.Fatalf("nearer namespace declaration was overridden: %s", output)
		}
		if strings.Count(output, " xmlns:p=") != 1 || strings.Count(output, " xmlns=") != 1 {
			t.Fatalf("namespace declarations duplicated: %s", output)
		}
	}
}

func TestOutputStandaloneXMLNil(t *testing.T) {
	if got := OutputStandaloneXML(nil); got != "" {
		t.Fatalf("nil node output = %q", got)
	}
}
