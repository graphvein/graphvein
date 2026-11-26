function provideRouter(routes: unknown) { return routes; }
provideRouter([]);
const x = { forRoot() {}, forChild() {} };
x.forRoot();
x.forChild();
