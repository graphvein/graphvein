package typescript

import (
	"path"
	"regexp"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// nextConfigFile matches the configuration file names Next.js loads
// (rows NX-P2, NX-P3 of docs/extract/routes.md).
var nextConfigFile = regexp.MustCompile(`^next\.config\.(js|mjs|cjs|ts|mts)$`)

// extractNext records the syntax the Next.js route linker needs and the
// shared export facts do not keep: destructured export names, and the
// literal keys of a next.config file. It never claims anything itself.
func extractNext(file string, root *parse.Node, out *Facts) {
	var next facts.NextFacts
	for _, statement := range root.Children {
		if defaultExport(statement) && next.DefaultExport == nil {
			location := span(statement)
			next.DefaultExport = &location
		}
		if statement.Kind != "export_statement" {
			continue
		}
		declaration := statement.Fields["declaration"]
		if declaration == nil || declaration.Kind != "lexical_declaration" && declaration.Kind != "variable_declaration" {
			continue
		}
		for _, declarator := range declaration.Children {
			if declarator.Kind != "variable_declarator" {
				continue
			}
			name := declarator.Fields["name"]
			if name == nil || name.Kind != "object_pattern" && name.Kind != "array_pattern" {
				continue
			}
			for _, bound := range destructuredNames(name) {
				next.DestructuredExports = append(next.DestructuredExports, facts.NamedSpan{Name: bound, Span: span(statement)})
			}
		}
	}
	if nextConfigFile.MatchString(path.Base(file)) {
		next.Config = readNextConfig(root)
	}
	if len(next.DestructuredExports) > 0 || next.Config != nil || next.DefaultExport != nil {
		out.Next = &next
	}
}

// defaultExport reports a statement that sets the default export.
func defaultExport(statement *parse.Node) bool {
	switch statement.Kind {
	case "export_statement":
		for _, t := range statement.Tokens {
			if t == "default" {
				return true
			}
		}
	case "expression_statement":
		for _, c := range statement.Children {
			if left := c.Fields["left"]; c.Kind == "assignment_expression" && left != nil && compact(left.Text) == "module.exports" {
				return true
			}
		}
	}
	return false
}

// destructuredNames lists the identifiers a destructuring pattern binds.
// (patternNames, from T022 B6c, adds to a set instead.)
func destructuredNames(n *parse.Node) []string {
	var names []string
	var walk func(*parse.Node)
	walk = func(n *parse.Node) {
		switch n.Kind {
		case "identifier", "shorthand_property_identifier_pattern":
			names = append(names, n.Text)
		case "pair_pattern":
			if v := n.Fields["value"]; v != nil {
				walk(v)
			}
		case "object_assignment_pattern", "assignment_pattern":
			if v := n.Fields["left"]; v != nil {
				walk(v)
			}
		case "object_pattern", "array_pattern", "rest_pattern":
			for _, c := range n.Children {
				walk(c)
			}
		}
	}
	walk(n)
	return names
}

// readNextConfig reads the default export of a next.config file: an object
// literal, a top-level const bound to one, or one plugin call wrapping
// either (assumption NX-V2). Anything else leaves every key unread.
func readNextConfig(root *parse.Node) *facts.NextConfig {
	unread := &facts.NextConfig{Shape: "unread", BasePathState: "unread", TrailingSlashState: "unread", PageExtensionsState: "unread", RewritesState: "unread"}
	consts := map[string]*parse.Node{}
	var exported []*parse.Node
	for _, statement := range root.Children {
		switch statement.Kind {
		case "lexical_declaration":
			for _, d := range statement.Children {
				if d.Kind == "variable_declarator" && d.Fields["name"] != nil && d.Fields["name"].Kind == "identifier" && d.Fields["value"] != nil && constDeclaration(statement) {
					consts[d.Fields["name"].Text] = d.Fields["value"]
				}
			}
		case "export_statement":
			if v := statement.Fields["value"]; v != nil {
				exported = append(exported, v)
			}
		case "expression_statement":
			for _, c := range statement.Children {
				if c.Kind != "assignment_expression" {
					continue
				}
				left := c.Fields["left"]
				if left != nil && left.Kind == "member_expression" && compact(left.Text) == "module.exports" && c.Fields["right"] != nil {
					exported = append(exported, c.Fields["right"])
				}
			}
		}
	}
	if len(exported) != 1 {
		return unread
	}
	value := peelExpression(exported[0])
	// Plugin calls, nested or not, each wrapping the one config argument
	// (assumption NX-V2: a plugin keeps the keys Graphvein reads).
	for depth := 0; value.Kind == "call_expression"; depth++ {
		args := value.Fields["arguments"]
		if args == nil || len(args.Children) != 1 || depth == 8 {
			return unread
		}
		value = peelExpression(args.Children[0])
	}
	if value.Kind == "identifier" {
		bound, ok := consts[value.Text]
		if !ok || mutated(root, value.Text) {
			return unread
		}
		value = peelExpression(bound)
	}
	if value.Kind != "object" {
		return unread
	}
	config := &facts.NextConfig{Shape: "object", BasePathState: "absent", TrailingSlashState: "absent", PageExtensionsState: "absent", RewritesState: "absent"}
	spread := false
	spreadKeys := map[string]bool{}
	rules := &configRules{reader: ruleReader{root: root, consts: consts}, seen: map[string]bool{}, unread: map[string]bool{}, rules: map[string][]facts.NextConfigRule{}}
	for _, member := range value.Children {
		key, v := memberKey(member)
		switch member.Kind {
		case "spread_element":
			// A spread of object literals (or a conditional choosing
			// between them) can only set the keys those literals name;
			// any other spread may set every key.
			if len(member.Children) != 1 || !literalKeys(member.Children[0], spreadKeys) {
				spread = true
			}
			continue
		case "method_definition":
			if key == "rewrites" {
				config.Rewrites, config.RewritesState = readRewrites(member.Fields["body"])
			}
			if ruleKeys[key] {
				rules.add(key, member, nil)
			}
			continue
		}
		switch key {
		case "basePath":
			if s, ok := stringLiteral(v); ok {
				config.BasePath, config.BasePathState = s, "literal"
			} else {
				config.BasePath, config.BasePathState = "", "unread"
			}
		case "trailingSlash":
			if v != nil && (v.Kind == "true" || v.Kind == "false") {
				config.TrailingSlash, config.TrailingSlashState = v.Kind == "true", "literal"
			} else {
				config.TrailingSlash, config.TrailingSlashState = false, "unread"
			}
		case "pageExtensions":
			config.PageExtensions, config.PageExtensionsState = nil, "unread"
			if v != nil && v.Kind == "array" {
				var list []string
				ok := true
				for _, item := range v.Children {
					s, literal := stringLiteral(item)
					ok = ok && literal
					list = append(list, s)
				}
				if ok && len(list) > 0 {
					config.PageExtensions, config.PageExtensionsState = list, "literal"
				}
			}
		case "redirects", "headers":
			rules.add(key, nil, v)
		case "rewrites":
			rules.add(key, nil, v)
			if v == nil {
				config.Rewrites, config.RewritesState = nil, "unread"
				break
			}
			switch v.Kind {
			case "arrow_function":
				body := v.Fields["body"]
				if body != nil && body.Kind == "statement_block" {
					config.Rewrites, config.RewritesState = readRewrites(body)
				} else if body != nil {
					config.Rewrites, config.RewritesState = rewriteValue(body)
				} else {
					config.Rewrites, config.RewritesState = nil, "unread"
				}
			case "function_expression", "function":
				config.Rewrites, config.RewritesState = readRewrites(v.Fields["body"])
			default:
				config.Rewrites, config.RewritesState = nil, "unread"
			}
		}
	}
	// A key a spread may set is unread, whatever the literal says.
	for key, state := range map[string]*string{"basePath": &config.BasePathState, "trailingSlash": &config.TrailingSlashState, "pageExtensions": &config.PageExtensionsState, "rewrites": &config.RewritesState} {
		if spreadKeys[key] && *state != "unread" {
			*state = "unread"
			switch key {
			case "basePath":
				config.BasePath = ""
			case "trailingSlash":
				config.TrailingSlash = false
			case "pageExtensions":
				config.PageExtensions = nil
			case "rewrites":
				config.Rewrites = nil
			}
		}
	}
	// A spread of anything else may supply any key the literal does not name.
	if spread {
		for _, state := range []*string{&config.BasePathState, &config.TrailingSlashState, &config.PageExtensionsState, &config.RewritesState} {
			if *state == "absent" {
				*state = "unread"
			}
		}
	}
	// Rules a spread may set are not read.
	for _, key := range []string{"redirects", "rewrites", "headers"} {
		if (spread && !rules.seen[key]) || spreadKeys[key] {
			rules.unread[key] = true
		}
	}
	for _, key := range []string{"redirects", "rewrites", "headers"} {
		if rules.unread[key] {
			config.RulesUnread = append(config.RulesUnread, key)
			continue
		}
		config.Rules = append(config.Rules, rules.rules[key]...)
	}
	return config
}

// ruleKeys are the next.config keys whose rules the shape rows read.
var ruleKeys = map[string]bool{"redirects": true, "rewrites": true, "headers": true}

// configRules collects the rules of one config object by key.
type configRules struct {
	reader ruleReader
	seen   map[string]bool
	unread map[string]bool
	rules  map[string][]facts.NextConfigRule
}

func (c *configRules) add(key string, member, value *parse.Node) {
	c.seen[key] = true
	rules, ok := c.reader.keyRules(key, member, value)
	if !ok {
		c.unread[key] = true
		return
	}
	c.rules[key] = rules
}

// literalKeys collects the keys an object literal, or a conditional
// between object literals, may set; false when the value is any other form
// or has a computed key or a nested spread.
func literalKeys(n *parse.Node, keys map[string]bool) bool {
	n = peelExpression(n)
	if n == nil {
		return false
	}
	switch n.Kind {
	case "ternary_expression":
		consequence, alternative := n.Fields["consequence"], n.Fields["alternative"]
		return consequence != nil && alternative != nil && literalKeys(consequence, keys) && literalKeys(alternative, keys)
	case "object":
		for _, member := range n.Children {
			if member.Kind == "comment" {
				continue
			}
			key, _ := memberKey(member)
			if key == "" {
				return false
			}
			keys[key] = true
		}
		return true
	}
	return false
}

// constDeclaration reports whether a lexical declaration is `const`.
func constDeclaration(n *parse.Node) bool {
	for _, t := range n.Tokens {
		if t == "const" {
			return true
		}
	}
	return len(n.Text) >= 6 && n.Text[:6] == "const "
}

// mutated reports whether any assignment writes a member of name, which
// would change the config object after its literal.
func mutated(n *parse.Node, name string) bool {
	if n.Kind == "assignment_expression" || n.Kind == "augmented_assignment_expression" {
		left := n.Fields["left"]
		for left != nil && left.Kind == "member_expression" || left != nil && left.Kind == "subscript_expression" {
			left = left.Fields["object"]
		}
		if left != nil && left.Kind == "identifier" && left.Text == name {
			return true
		}
	}
	if n.Kind == "call_expression" {
		// Object.assign(config, ...) and the like.
		if args := n.Fields["arguments"]; args != nil && len(args.Children) > 0 && args.Children[0].Kind == "identifier" && args.Children[0].Text == name {
			if f := n.Fields["function"]; f != nil && compact(f.Text) == "Object.assign" {
				return true
			}
		}
	}
	for _, c := range n.Children {
		if mutated(c, name) {
			return true
		}
	}
	return false
}

// peelExpression removes parentheses and type-only wrappers.
func peelExpression(n *parse.Node) *parse.Node {
	for n != nil {
		switch n.Kind {
		case "parenthesized_expression", "satisfies_expression", "as_expression", "non_null_expression":
			if len(n.Children) == 0 {
				return n
			}
			n = n.Children[0]
		default:
			return n
		}
	}
	return n
}

// memberKey returns an object member's static key and value.
func memberKey(member *parse.Node) (string, *parse.Node) {
	switch member.Kind {
	case "pair":
		key := member.Fields["key"]
		if key == nil {
			return "", nil
		}
		value := peelExpression(member.Fields["value"])
		switch key.Kind {
		case "property_identifier":
			return key.Text, value
		case "string":
			if s, ok := stringLiteral(key); ok {
				return s, value
			}
		}
		return "", value
	case "method_definition":
		if key := member.Fields["name"]; key != nil && key.Kind == "property_identifier" {
			return key.Text, nil
		}
	case "shorthand_property_identifier":
		// basePath (a variable): never a literal.
		return member.Text, member
	}
	return "", nil
}

// stringLiteral reads a string or substitution-free template literal
// without escape sequences.
func stringLiteral(n *parse.Node) (string, bool) {
	n = peelExpression(n)
	if n == nil || n.Kind != "string" && n.Kind != "template_string" {
		return "", false
	}
	value := ""
	for _, c := range n.Children {
		if c.Kind != "string_fragment" {
			return "", false
		}
		value += c.Text
	}
	return value, true
}

// readRewrites reads a function body that is a single return statement.
func readRewrites(body *parse.Node) ([]facts.NextRewrite, string) {
	if body == nil || body.Kind != "statement_block" {
		return nil, "unread"
	}
	var statements []*parse.Node
	for _, c := range body.Children {
		if c.Kind != "comment" {
			statements = append(statements, c)
		}
	}
	if len(statements) != 1 || statements[0].Kind != "return_statement" || len(statements[0].Children) != 1 {
		return nil, "unread"
	}
	return rewriteValue(statements[0].Children[0])
}

// rewriteValue reads the value rewrites() returns: an array literal of
// rules (phase afterFiles) or an object of the three phase arrays.
func rewriteValue(v *parse.Node) ([]facts.NextRewrite, string) {
	v = peelExpression(v)
	if v == nil {
		return nil, "unread"
	}
	if v.Kind == "array" {
		return rewriteRules("afterFiles", v)
	}
	if v.Kind != "object" {
		return nil, "unread"
	}
	var rules []facts.NextRewrite
	for _, member := range v.Children {
		key, value := memberKey(member)
		if member.Kind != "pair" || key != "beforeFiles" && key != "afterFiles" && key != "fallback" || value == nil || value.Kind != "array" {
			return nil, "unread"
		}
		phase, state := rewriteRules(key, value)
		if state != "literal" {
			return nil, "unread"
		}
		rules = append(rules, phase...)
	}
	return rules, "literal"
}

// rewritePath is a source of literal segments and :name parameters with
// an optional *, + or ? modifier; regex groups and other syntax are unread.
var rewritePath = regexp.MustCompile(`^(/([A-Za-z0-9._~-]+|:[A-Za-z_][A-Za-z0-9_]*[*+?]?))+/?$|^/$`)

func rewriteRules(phase string, array *parse.Node) ([]facts.NextRewrite, string) {
	var rules []facts.NextRewrite
	for _, item := range array.Children {
		item = peelExpression(item)
		if item.Kind == "comment" {
			continue
		}
		if item.Kind != "object" {
			return nil, "unread"
		}
		rule := facts.NextRewrite{Phase: phase}
		seen := map[string]bool{}
		for _, member := range item.Children {
			key, value := memberKey(member)
			s, ok := stringLiteral(value)
			if member.Kind != "pair" || !ok || seen[key] {
				return nil, "unread"
			}
			seen[key] = true
			switch key {
			case "source":
				rule.Source = s
			case "destination":
				rule.Destination = s
			default:
				return nil, "unread"
			}
		}
		if !seen["source"] || !seen["destination"] || !rewritePath.MatchString(rule.Source) {
			return nil, "unread"
		}
		rules = append(rules, rule)
	}
	return rules, "literal"
}
