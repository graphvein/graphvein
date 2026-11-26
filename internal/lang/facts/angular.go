package facts

// AngularInjection retains DI syntax, leaving token proof to the shared resolver.
type AngularInjection struct {
	Token, Factory RouteExpr
	Scope          int
}

// AngularClass retains decorators and methods needed for bounded DI wrappers.
type AngularClass struct {
	Name       string
	Decorators []RouteExpr
	Methods    map[string]string
}

// AngularInterceptor retains request forwarding syntax without evaluating code.
type AngularInterceptor struct {
	Name, Request, Next string
	Body                []URLStatement
	Expressions         []RouteExpr
	Scope, URLScope     int
}

// AngularRegistration retains one injector's provider expressions.
type AngularRegistration struct {
	Expression  RouteExpr
	Scope       int
	Conditional bool
}

// AngularFacts is cached source syntax, not deployment or token resolution.
type AngularFacts struct {
	Classes       []AngularClass
	Interceptors  []AngularInterceptor
	Registrations []AngularRegistration
}
