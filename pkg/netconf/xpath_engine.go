package netconf

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/antchfx/xmlquery"
	"github.com/antchfx/xpath"
)

const experimentalXPathDataPrefix = "arca_nc_data"

func usesExperimentalXPathEngine(filter *Filter) bool {
	if filter == nil || normalizedFilterType(filter) != "xpath" {
		return false
	}
	_, err := parseFilterXPathWithNamespaces(filter)
	return err != nil
}

func validateExperimentalXPathFilter(rpcName string, filter *Filter) *RPCError {
	if err := validateExperimentalXPathSelect(rpcName, filter); err != nil {
		return err
	}
	expr, err := compileExperimentalXPathExpression(filter)
	if err != nil {
		return ErrInvalidFilter(rpcName, fmt.Sprintf("invalid xpath filter: %v", err))
	}
	doc, rpcErr := parseExperimentalXPathDocument(rpcName, nil)
	if rpcErr != nil {
		return rpcErr
	}
	if !experimentalXPathResultIsNodeSet(expr, doc) {
		return ErrInvalidFilter(rpcName, "xpath filter must evaluate to a node-set")
	}
	return nil
}

func applyExperimentalXPathFilter(rpcName string, xmlData []byte, filter *Filter) ([]byte, error) {
	if !usesExperimentalXPathEngine(filter) {
		return append([]byte(nil), xmlData...), nil
	}
	if err := validateExperimentalXPathSelect(rpcName, filter); err != nil {
		return nil, err
	}
	expr, err := compileExperimentalXPathExpression(filter)
	if err != nil {
		return nil, ErrInvalidFilter(rpcName, fmt.Sprintf("invalid xpath filter: %v", err))
	}
	doc, rpcErr := parseExperimentalXPathDocument(rpcName, xmlData)
	if rpcErr != nil {
		return nil, rpcErr
	}
	if !experimentalXPathResultIsNodeSet(expr, doc) {
		return nil, ErrInvalidFilter(rpcName, "xpath filter must evaluate to a node-set")
	}

	nodes := xmlquery.QuerySelectorAll(doc, expr)
	if len(nodes) > MaxXMLElements {
		return nil, NewRPCError(ErrorTypeProtocol, ErrorTagInvalidValue,
			fmt.Sprintf("xpath filter result exceeds maximum element limit (%d)", MaxXMLElements)).
			WithPath(fmt.Sprintf("/rpc/%s/filter", rpcName)).
			WithAppTag("size-limit")
	}
	if len(nodes) == 0 {
		return []byte{}, nil
	}

	dataRoot := experimentalXPathDataRoot(doc)
	if dataRoot == nil {
		return nil, ErrOperationFailed("xpath filter failed to locate NETCONF data root")
	}
	for _, node := range nodes {
		if node.Type == xmlquery.DocumentNode || node == dataRoot {
			return append([]byte(nil), xmlData...), nil
		}
	}

	filtered, err := renderExperimentalXPathNodes(dataRoot, nodes)
	if err != nil {
		return nil, ErrInvalidFilter(rpcName, err.Error())
	}
	if len(filtered) > MaxXMLSize {
		return nil, NewRPCError(ErrorTypeProtocol, ErrorTagInvalidValue,
			fmt.Sprintf("xpath filter result exceeds maximum size limit (%d bytes)", MaxXMLSize)).
			WithPath(fmt.Sprintf("/rpc/%s/filter", rpcName)).
			WithAppTag("size-limit")
	}
	return filtered, nil
}

func validateExperimentalXPathSelect(rpcName string, filter *Filter) *RPCError {
	if filter == nil {
		return nil
	}
	selectExpr := strings.TrimSpace(filter.Select)
	if selectExpr == "" {
		return ErrInvalidFilter(rpcName, "xpath filter requires select attribute")
	}
	if !strings.HasPrefix(selectExpr, "/") {
		return ErrInvalidFilter(rpcName, fmt.Sprintf("invalid xpath filter: XPath must start with /: %s", selectExpr))
	}
	if experimentalXPathSelectsAttribute(selectExpr) {
		return ErrInvalidFilter(rpcName, "xpath filter attribute selection is not supported by experimental response shaping")
	}
	namespaceCtx := experimentalXPathNamespaceContext(filter)
	if rpcErr := validateExperimentalXPathRootStep(rpcName, selectExpr, namespaceCtx); rpcErr != nil {
		return rpcErr
	}
	return nil
}

func compileExperimentalXPathExpression(filter *Filter) (*xpath.Expr, error) {
	selectExpr := strings.TrimSpace(filter.Select)
	namespaceCtx := experimentalXPathNamespaceContext(filter)
	if needsExperimentalXPathDataRoot(selectExpr, namespaceCtx) {
		dataPrefix := experimentalXPathDataNamespacePrefix(namespaceCtx)
		namespaceCtx[dataPrefix] = NetconfBaseNS
		selectExpr = "/" + dataPrefix + ":data" + selectExpr
	}
	return xpath.CompileWithNS(selectExpr, namespaceCtx)
}

func experimentalXPathNamespaceContext(filter *Filter) map[string]string {
	namespaceCtx := make(map[string]string)
	if filter == nil {
		return namespaceCtx
	}
	for _, attr := range collectNamespaceAttrs(filter.InheritedAttrs, filter.Attrs) {
		if attr.Name.Space != "xmlns" || attr.Name.Local == "" {
			continue
		}
		namespaceCtx[attr.Name.Local] = attr.Value
	}
	return namespaceCtx
}

func experimentalXPathSelectsAttribute(selectExpr string) bool {
	quote := byte(0)
	for i := 0; i < len(selectExpr); i++ {
		ch := selectExpr[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		if ch == '@' || strings.HasPrefix(selectExpr[i:], "attribute::") {
			return true
		}
	}
	return false
}

func validateExperimentalXPathRootStep(rpcName, selectExpr string, namespaceCtx map[string]string) *RPCError {
	prefix, local, ok := experimentalXPathFirstStepQName(selectExpr)
	if !ok {
		return nil
	}
	if prefix == "" {
		return ErrInvalidFilter(rpcName, fmt.Sprintf("experimental xpath filter requires namespace-prefixed root element: %s", local))
	}
	namespace := namespaceCtx[prefix]
	if namespace == "" {
		return nil
	}
	if local == "data" {
		if namespace != NetconfBaseNS {
			return ErrInvalidFilter(rpcName, fmt.Sprintf("invalid xpath filter namespace: /data uses namespace %q, want %q", namespace, NetconfBaseNS))
		}
		return nil
	}
	if implementedYANGPathSchema.children[local] == nil {
		return ErrInvalidFilter(rpcName, fmt.Sprintf("unsupported xpath filter path: unsupported root element /%s", local))
	}
	if expected := expectedXPathNamespace([]string{local}); namespace != expected {
		return ErrInvalidFilter(rpcName, fmt.Sprintf("invalid xpath filter namespace: /%s uses namespace %q, want %q", local, namespace, expected))
	}
	return nil
}

func experimentalXPathFirstStepQName(selectExpr string) (string, string, bool) {
	if !strings.HasPrefix(selectExpr, "/") || strings.HasPrefix(selectExpr, "//") {
		return "", "", false
	}
	step := strings.TrimPrefix(selectExpr, "/")
	end := len(step)
	for idx, ch := range step {
		if strings.ContainsRune("/[|+-=<>! \t\r\n)", ch) {
			end = idx
			break
		}
	}
	step = step[:end]
	if step == "" || step == "." || step == ".." || step == "*" || strings.HasPrefix(step, "@") || strings.Contains(step, "::") {
		return "", "", false
	}
	if prefix, local, ok := strings.Cut(step, ":"); ok {
		if prefix == "" || local == "" || strings.Contains(local, ":") {
			return "", "", false
		}
		return prefix, local, true
	}
	return "", step, true
}

func experimentalXPathDataNamespacePrefix(namespaceCtx map[string]string) string {
	for prefix, namespace := range namespaceCtx {
		if namespace == NetconfBaseNS {
			return prefix
		}
	}
	prefix := experimentalXPathDataPrefix
	for i := 0; ; i++ {
		if _, exists := namespaceCtx[prefix]; !exists {
			return prefix
		}
		prefix = fmt.Sprintf("%s%d", experimentalXPathDataPrefix, i)
	}
}

func needsExperimentalXPathDataRoot(selectExpr string, namespaceCtx map[string]string) bool {
	if strings.HasPrefix(selectExpr, "//") {
		return false
	}
	if !strings.HasPrefix(selectExpr, "/") {
		return false
	}
	firstSegment := strings.TrimPrefix(selectExpr, "/")
	for idx, r := range firstSegment {
		if r == '/' || r == '[' || r == '(' || r == ')' {
			firstSegment = firstSegment[:idx]
			break
		}
	}
	if firstSegment == "" {
		return false
	}
	prefix, local, ok := strings.Cut(firstSegment, ":")
	if !ok {
		return true
	}
	return local != "data" || namespaceCtx[prefix] != NetconfBaseNS
}

func parseExperimentalXPathDocument(rpcName string, xmlData []byte) (*xmlquery.Node, *RPCError) {
	var buf bytes.Buffer
	buf.WriteString(`<data xmlns="`)
	buf.WriteString(NetconfBaseNS)
	buf.WriteString(`">`)
	buf.Write(xmlData)
	buf.WriteString(`</data>`)
	if buf.Len() > MaxXMLSize {
		return nil, NewRPCError(ErrorTypeProtocol, ErrorTagInvalidValue,
			fmt.Sprintf("xpath evaluation input exceeds maximum size limit (%d bytes)", MaxXMLSize)).
			WithPath(fmt.Sprintf("/rpc/%s/filter", rpcName)).
			WithAppTag("size-limit")
	}
	doc, err := xmlquery.Parse(bytes.NewReader(buf.Bytes()))
	if err != nil {
		return nil, ErrInvalidFilter(rpcName, fmt.Sprintf("xpath evaluation input is not valid XML: %v", err))
	}
	return doc, nil
}

func experimentalXPathResultIsNodeSet(expr *xpath.Expr, doc *xmlquery.Node) bool {
	_, ok := expr.Evaluate(xmlquery.CreateXPathNavigator(doc)).(*xpath.NodeIterator)
	return ok
}

func experimentalXPathDataRoot(doc *xmlquery.Node) *xmlquery.Node {
	if doc == nil {
		return nil
	}
	for node := doc.FirstChild; node != nil; node = node.NextSibling {
		if node.Type == xmlquery.ElementNode && node.Data == "data" && node.NamespaceURI == NetconfBaseNS {
			return node
		}
	}
	return nil
}

func renderExperimentalXPathNodes(dataRoot *xmlquery.Node, nodes []*xmlquery.Node) ([]byte, error) {
	include := map[*xmlquery.Node]struct{}{dataRoot: {}}
	fullSubtree := map[*xmlquery.Node]struct{}{}
	for _, node := range nodes {
		if node == nil {
			continue
		}
		switch node.Type {
		case xmlquery.AttributeNode:
			return nil, fmt.Errorf("xpath filter selected attributes, which are not supported by experimental response shaping")
		case xmlquery.ElementNode:
			if !markExperimentalXPathAncestors(dataRoot, node, include) {
				return nil, fmt.Errorf("xpath filter selected a node outside NETCONF data")
			}
			fullSubtree[node] = struct{}{}
		case xmlquery.TextNode, xmlquery.CharDataNode, xmlquery.CommentNode:
			if !markExperimentalXPathAncestors(dataRoot, node, include) {
				return nil, fmt.Errorf("xpath filter selected a node outside NETCONF data")
			}
		default:
			return nil, fmt.Errorf("xpath filter selected unsupported node type")
		}
	}

	dataClone := cloneExperimentalXPathNode(dataRoot, include, fullSubtree, false)
	if dataClone == nil {
		return []byte{}, nil
	}
	var buf bytes.Buffer
	for child := dataClone.FirstChild; child != nil; child = child.NextSibling {
		if err := child.WriteWithOptions(&buf, xmlquery.WithOutputSelf(), xmlquery.WithoutComments()); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func markExperimentalXPathAncestors(dataRoot, node *xmlquery.Node, include map[*xmlquery.Node]struct{}) bool {
	for current := node; current != nil; current = current.Parent {
		include[current] = struct{}{}
		if current == dataRoot {
			return true
		}
	}
	return false
}

func cloneExperimentalXPathNode(node *xmlquery.Node, include, fullSubtree map[*xmlquery.Node]struct{}, force bool) *xmlquery.Node {
	if node == nil {
		return nil
	}
	if !force {
		if _, ok := include[node]; !ok {
			return nil
		}
	}
	clone := &xmlquery.Node{
		Type:         node.Type,
		Data:         node.Data,
		Prefix:       node.Prefix,
		NamespaceURI: node.NamespaceURI,
		LineNumber:   node.LineNumber,
	}
	if len(node.Attr) > 0 {
		clone.Attr = append([]xmlquery.Attr(nil), node.Attr...)
	}
	if node.ProcInst != nil {
		procInst := *node.ProcInst
		clone.ProcInst = &procInst
	}

	forceChildren := force
	if _, ok := fullSubtree[node]; ok {
		forceChildren = true
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		childClone := cloneExperimentalXPathNode(child, include, fullSubtree, forceChildren)
		if childClone != nil {
			xmlquery.AddChild(clone, childClone)
		}
	}
	return clone
}
