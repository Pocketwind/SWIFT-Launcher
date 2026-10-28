package xmlutil

import "github.com/antchfx/xmlquery"

// OutputStandaloneXML serializes a subtree with its in-scope namespace
// declarations, including prefixes used only in attribute values. The original
// tree is unchanged; declarations on the nearest ancestor take precedence.
func OutputStandaloneXML(node *xmlquery.Node) string {
	if node == nil {
		return ""
	}
	standalone := *node
	standalone.Attr = append([]xmlquery.Attr(nil), node.Attr...)
	seen := make(map[string]bool)
	for current := node; current != nil; current = current.Parent {
		for _, attr := range current.Attr {
			prefix, declaration := namespaceDeclaration(attr)
			if !declaration || seen[prefix] {
				continue
			}
			seen[prefix] = true
			if current != node {
				standalone.Attr = append(standalone.Attr, attr)
			}
		}
	}
	return standalone.OutputXMLWithOptions(xmlquery.WithOutputSelf(), xmlquery.WithPreserveSpace())
}

func namespaceDeclaration(attr xmlquery.Attr) (string, bool) {
	if attr.Name.Space == "xmlns" {
		return attr.Name.Local, true
	}
	if attr.Name.Space == "" && attr.Name.Local == "xmlns" {
		return "", true
	}
	return "", false
}
