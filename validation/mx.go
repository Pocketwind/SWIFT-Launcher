package validation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/knroy/go-xml/xdm"
	"github.com/knroy/go-xml/xsd"
)

const iso20022Namespace = "urn:iso:std:iso:20022:tech:xsd:"

// ValidateMX validates the original input tree, preserving namespaces inherited
// from the SAA envelope when AppHdr and Document are assessed separately.
func (v *Validator) ValidateMX(payload string) error {
	tree, err := xdm.ParseString(payload, xdm.ParseOptions{TrackPositions: true})
	if err != nil {
		return fmt.Errorf("parse MX XML: %w", err)
	}
	var headers, documents []*xdm.Node
	walkMX(tree.Root, func(node *xdm.Node) {
		switch node.Name.Local {
		case "AppHdr":
			headers = append(headers, node)
		case "Document":
			documents = append(documents, node)
		}
	})
	if len(headers) != 1 || len(documents) != 1 {
		return fmt.Errorf("MX requires exactly one AppHdr and Document (found %d and %d)", len(headers), len(documents))
	}
	header, document := headers[0], documents[0]
	var failures []error
	headerType, headerErr := mxSchemaType(header)
	bodyType, bodyErr := mxSchemaType(document)
	failures = append(failures, headerErr, bodyErr)
	var services []*xdm.Node
	for _, child := range header.ChildElements() {
		if child.Name.Local == "BizSvc" && child.Name.URI == header.Name.URI {
			services = append(services, child)
		}
	}
	service := ""
	if len(services) > 1 {
		failures = append(failures, fmt.Errorf("AppHdr contains multiple BizSvc elements"))
	} else if len(services) == 1 {
		service = strings.TrimSpace(services[0].StringValue())
		if service != "" && !safeSchemaComponent(service) {
			failures = append(failures, fmt.Errorf("invalid AppHdr/BizSvc %q", service))
			bodyErr = fmt.Errorf("cannot select schema for invalid BizSvc")
		}
	}
	if bodyErr == nil {
		if service != "" {
			bodyType += "-" + service
		}
		failures = append(failures, v.validateMXElement(document, bodyType))
	}
	if headerErr == nil {
		// MyStandards ships service-specific business application headers next
		// to their body schema. Prefer those restrictions when supplied.
		if bodyErr == nil && service != "" {
			bah := bodyType + "_BAH"
			if info, err := os.Stat(filepath.Join(v.schemaDir, bah+".xsd")); err == nil {
				if !info.Mode().IsRegular() {
					failures = append(failures, fmt.Errorf("AppHdr schema %s.xsd is not a regular file", bah))
					headerErr = fmt.Errorf("unavailable AppHdr schema")
				} else {
					headerType = bah
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				failures = append(failures, fmt.Errorf("inspect AppHdr schema %s.xsd: %w", bah, err))
				headerErr = err
			}
		}
		if headerErr == nil {
			failures = append(failures, v.validateMXElement(header, headerType))
		}
	}
	for _, root := range []*xdm.Node{header, document} {
		walkMX(root, func(node *xdm.Node) {
			if !strings.HasPrefix(node.Name.URI, iso20022Namespace) {
				return
			}
			location := mxLocation(node)
			for _, attr := range node.Attrs {
				if attr.Name.URI == "" && attr.Name.Local == "Ccy" {
					failures = append(failures, v.checkCurrency(strings.TrimSpace(attr.Value), location+"/@Ccy"))
				}
			}
			// Successful XSD assessment identifies even generically named code
			// elements (for example Id typed as AnyBICIdentifier). The name lists
			// also report reference errors when a separate XSD error is present.
			_, typeName := xdm.SplitAnnotationName(node.TypeAnnotation)
			fallback := mxReferenceNameFallback(node)
			value := strings.TrimSpace(node.StringValue())
			switch {
			case fallback && mxBICName(node.Name.Local) || strings.HasPrefix(typeName, "BICFI") || strings.HasPrefix(typeName, "AnyBIC"):
				failures = append(failures, v.checkBIC(value, location))
			case fallback && mxCurrencyName(node.Name.Local) || strings.HasPrefix(typeName, "ActiveCurrencyCode") || strings.HasPrefix(typeName, "ActiveOrHistoricCurrencyCode"):
				failures = append(failures, v.checkCurrency(value, location))
			case fallback && mxCountryName(node.Name.Local) || strings.HasPrefix(typeName, "CountryCode"):
				failures = append(failures, v.checkCountry(value, location))
			}
		})
	}
	return errors.Join(failures...)
}

func mxReferenceNameFallback(node *xdm.Node) bool {
	if len(node.ChildElements()) != 0 {
		return false
	}
	uri, name := xdm.SplitAnnotationName(node.TypeAnnotation)
	if uri != "" && uri != xdm.NSXS {
		return false
	}
	// An unassessed node or a generic string can use the conventional field
	// name. A schema-assigned application type takes precedence: for example,
	// colr.020's Ccy can be a ProprietaryReason4 container, even when empty.
	switch name {
	case "", "untyped", "untypedAtomic", "string", "normalizedString", "token", "anySimpleType", "anyAtomicType":
		return true
	}
	return false
}

func mxSchemaType(node *xdm.Node) (string, error) {
	if !strings.HasPrefix(node.Name.URI, iso20022Namespace) {
		return "", fmt.Errorf("unsupported %s namespace %q", node.Name.Local, node.Name.URI)
	}
	name := strings.TrimPrefix(node.Name.URI, iso20022Namespace)
	if !safeSchemaComponent(name) {
		return "", fmt.Errorf("invalid %s schema type %q", node.Name.Local, name)
	}
	return name, nil
}

// Components originate in message XML, so accept only filename characters that
// cannot become a path, drive, URI or parent-directory reference.
func safeSchemaComponent(value string) bool {
	if value == "" || strings.Contains(value, "..") {
		return false
	}
	for i, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || i > 0 && (r == '.' || r == '-' || r == '_') {
			continue
		}
		return false
	}
	return true
}

func (v *Validator) validateMXElement(node *xdm.Node, schemaName string) error {
	schema, err := v.mxSchema(schemaName)
	if err != nil {
		return fmt.Errorf("%s XSD %s.xsd: %w", node.Name.Local, schemaName, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := schema.ValidateContext(ctx, node, xsd.ValidateOptions{Annotate: true}); err != nil {
		return fmt.Errorf("%s XSD %s.xsd: %w", node.Name.Local, schemaName, err)
	}
	return nil
}

func (v *Validator) mxSchema(name string) (*xsd.Schema, error) {
	v.schemaMu.Lock()
	defer v.schemaMu.Unlock()
	if schema := v.schemas[name]; schema != nil {
		return schema, nil
	}
	schema, err := xsd.LoadFile(filepath.Join(v.schemaDir, name+".xsd"), xsd.Options{
		Resolver: &xsd.FileResolver{Root: v.schemaDir},
	})
	if err != nil {
		return nil, err
	}
	if v.schemas == nil {
		v.schemas = make(map[string]*xsd.Schema)
	}
	v.schemas[name] = schema
	return schema, nil
}

func walkMX(node *xdm.Node, visit func(*xdm.Node)) {
	if node.Kind == xdm.KindElement {
		visit(node)
	}
	for _, child := range node.Children {
		walkMX(child, visit)
	}
}

func mxLocation(node *xdm.Node) string {
	var parts []string
	for current := node; current != nil; current = current.Parent {
		if current.Kind == xdm.KindElement {
			parts = append(parts, current.Name.Local)
		}
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	path := "/" + strings.Join(parts, "/")
	if line, col, ok := node.Position(); ok {
		path += fmt.Sprintf(" (line %d, column %d)", line, col)
	}
	return path
}

func mxBICName(name string) bool {
	switch name {
	case "BICFI", "AnyBIC", "BIC", "BICOrBEI", "TechRcvr", "TechSndr", "VoteDrctlyToIssr":
		return true
	}
	return false
}

func mxCurrencyName(name string) bool {
	switch name {
	case "Ccy", "BalCcy", "BaseCcy", "CcyOptn", "CcyToBuy", "CcyToSell", "DnmtnCcy", "QtdCcy", "RptCcy", "SrcCcy", "SysCcy", "TrgtCcy", "UnitCcy", "ValtnCcy", "CcyOfTrf", "CollstnCcy", "CshCollCcy", "ReqdNAVCcy", "RptgCcy":
		return true
	}
	return false
}

func mxCountryName(name string) bool {
	switch name {
	case "Ctry", "CtryOfBirth", "CtryOfDmcl", "CtryOfIncmSrc", "CtryOfIncorprtn", "CtryOfRes", "DmclCtry", "DmstIdSrc", "IdAsCtry", "IssrCtry", "NonDmclCtry", "Ntlty", "PlcOfJursdctn", "RegdDstrbtnCtry", "VldDmclCtry":
		return true
	}
	return false
}
