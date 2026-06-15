package xmlutil

import (
	"fmt"

	"github.com/antchfx/xmlquery"
	"github.com/beevik/etree"
)

func SafeInnerText(node *xmlquery.Node) string {
	if node == nil {
		return ""
	}
	return node.InnerText()
}

func FormatXMLString(raw string) (string, error) {
	doc := etree.NewDocument()
	if err := doc.ReadFromString(raw); err != nil {
		return "", fmt.Errorf("error parsing XML for formatting: %w", err)
	}

	doc.IndentTabs()

	formatted, err := doc.WriteToString()
	if err != nil {
		return "", fmt.Errorf("error writing formatted XML: %w", err)
	}

	return formatted, nil
}
