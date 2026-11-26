package routes

import "github.com/graphvein/graphvein/internal/testscope"

func selectedRules(rules []testscope.Rules) testscope.Rules {
	if len(rules) > 0 {
		return rules[0]
	}
	return testscope.Rules{}
}

func chainScope(a, b testscope.Scope) testscope.Scope {
	if a == testscope.Test || b == testscope.Test {
		return testscope.Test
	}
	return ""
}

func mergedScope(a, b testscope.Scope) testscope.Scope {
	if a == testscope.Test && b == testscope.Test {
		return testscope.Test
	}
	return ""
}
