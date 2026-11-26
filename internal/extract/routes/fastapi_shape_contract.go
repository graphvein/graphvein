package routes

// ShapeRow is one row of the FastAPI route shape claim contract
// (docs/contract/claims/route-shape-fastapi.md, rows SH-F). Forms are the
// code forms the row reads; every modelled form is listed by exactly one
// row, and every form no row lists goes to D (TestFastAPIShapeContract
// keeps the table and the document 1:1 and checks that the fixture
// reaches every row).
type ShapeRow struct {
	ID    string
	Title string
	Forms []string
}

// FastAPIShapeContract lists the rows in document order.
var FastAPIShapeContract = []ShapeRow{
	{"SH-F1", "path parameter declared", []string{"path name annotation", "Path()"}},
	{"SH-F2", "path parameter from the template only", []string{"{name}", "{name:convertor}"}},
	{"SH-F3", "path section state", []string{"path complete", "path provably_none"}},
	{"SH-F4", "query parameter from a scalar annotation", []string{"scalar annotation"}},
	{"SH-F5", "Query, Header and Cookie markers", []string{"Query()", "Header()", "Cookie()", "alias=", "convert_underscores="}},
	{"SH-F6", "parameter without annotation", []string{"no annotation"}},
	{"SH-F7", "Annotated aliases", []string{"X = Annotated[...]"}},
	{"SH-F8", "enum and Literal types", []string{"Enum subclass", "Literal[...]"}},
	{"SH-F9", "dependency function parameters", []string{"Depends(function)", "Security(function)"}},
	{"SH-F10", "dependency class with __init__", []string{"Depends(Class)"}},
	{"SH-F11", "OAuth2 password request form", []string{"OAuth2PasswordRequestForm", "OAuth2PasswordRequestFormStrict"}},
	{"SH-F12", "dependencies= lists", []string{"route dependencies=", "router dependencies=", "include_router dependencies=", "app dependencies="}},
	{"SH-F13", "security scheme dependency", []string{"Depends(scheme instance)", "Depends(subclass instance)"}},
	{"SH-F14", "framework values", []string{"Request", "WebSocket", "HTTPConnection", "Response", "BackgroundTasks", "SecurityScopes"}},
	{"SH-F15", "names read on the request", []string{"request.headers", "request.cookies", "request.query_params", "request.path_params"}},
	{"SH-F16", "raw body reads", []string{"request.json()", "request.body()", "request.form()", "request.stream()"}},
	{"SH-F17", "request object passed on", []string{"f(request)", "f(request.headers)"}},
	{"SH-F18", "single body parameter", []string{"model annotation", "Body()"}},
	{"SH-F19", "embedded body", []string{"several body parameters", "Body(embed=True)"}},
	{"SH-F20", "form and file bodies", []string{"Form()", "File()", "UploadFile", "list[UploadFile]", "Annotated[Model, Form()]"}},
	{"SH-F21", "sequence or mapping without a marker", []string{"list[T]", "set[T]", "dict[K, V]"}},
	{"SH-F22", "repository models", []string{"BaseModel subclass", "SQLModel subclass", "Field()", "model_config extra", "pydantic version"}},
	{"SH-F23", "parameter and body section state", []string{"complete", "provably_none"}},
	{"SH-F24", "success status and content type", []string{"status_code=", "default 200", "response_class=", "default_response_class=", "**options", "x.default_response_class = C", "no-body status", "returned Response", "Response parameter"}},
	{"SH-F25", "response_model", []string{"response_model=T"}},
	{"SH-F26", "return annotation", []string{"-> T"}},
	{"SH-F27", "no response model", []string{"response_model=None", "-> None", "-> Response", "no return annotation"}},
	{"SH-F28", "responses= dictionary", []string{"responses={code: {...}}"}},
	{"SH-F29", "raised HTTPException", []string{"raise HTTPException(status_code=n)"}},
	{"SH-F30", "validation error response", []string{"422"}},
	{"SH-F31", "returned values followed", []string{"return f(...)", "x = f(...); return x", "return Model(...)"}},
	{"SH-F32", "security schemes", []string{"OAuth2PasswordBearer", "APIKeyHeader", "APIKeyQuery", "APIKeyCookie", "HTTPBearer", "HTTPBasic", "HTTPDigest", "OpenIdConnect"}},
	{"SH-F33", "security requirement", []string{"security_dependencies"}},
	{"SH-F34", "hidden routes", []string{"include_in_schema=False"}},
	{"SH-F35", "call following", []string{"proven call", "stop"}},
	{"SH-F36", "response model options", []string{"response_model_include=", "response_model_exclude=", "response_model_exclude_none=", "response_model_by_alias=", "response_model_exclude_unset=", "response_model_exclude_defaults="}},
	{"SH-F37", "project exception handlers", []string{"exception_handler(K)", "add_exception_handler(K, h)", "exception_handlers=", "FastAPI(**options)"}},
	{"SH-F38", "custom route classes", []string{"route_class=", "APIRouter(**options)", "x.route_class = C"}},
	{"SH-F39", "streamed handlers", []string{"yield", "-> Iterable[T]"}},
	{"D", "every other form", nil},
}

// shapeTag is a test-only hook: every claim branch reports its row
// ("<row>" or "<row>/<form>").
var shapeTag func(row string)

func tagShape(row string) {
	if shapeTag != nil {
		shapeTag(row)
	}
}
