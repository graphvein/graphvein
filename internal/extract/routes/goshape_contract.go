package routes

// goShapeRow is one row of the Go route shape claim contract
// (docs/contract/claims/route-shape-go.md). Forms are the code forms the
// row reads, written "group:form"; TestGoShapeContractTable keeps the
// table and the document 1:1.
type goShapeRow struct {
	ID    string
	Title string
	Forms []string
}

// goShapeRows is the claim contract in table order. A form no row lists
// goes to D: it claims nothing.
var goShapeRows = []goShapeRow{
	{"SH-G1", "gin path parameter read", []string{"gin:Param"}},
	{"SH-G2", "chi path parameter read", []string{"chi:URLParam"}},
	{"SH-G3", "net/http path parameter read", []string{"http:PathValue"}},
	{"SH-G4", "conversion of a read value", []string{"strconv:Atoi", "strconv:ParseInt", "strconv:ParseUint", "strconv:ParseFloat", "strconv:ParseBool"}},
	{"SH-G5", "gin query read", []string{"gin:Query", "gin:DefaultQuery", "gin:GetQuery", "gin:QueryArray", "gin:GetQueryArray"}},
	{"SH-G6", "net/http query read", []string{"http:URL.Query().Get", "http:URL.Query().Has"}},
	{"SH-G7", "FormValue", []string{"http:FormValue"}},
	{"SH-G8", "form body field read", []string{"gin:PostForm", "gin:DefaultPostForm", "gin:GetPostForm", "gin:PostFormArray", "gin:GetPostFormArray", "gin:FormFile", "http:PostFormValue", "http:FormFile"}},
	{"SH-G9", "header read", []string{"gin:GetHeader", "http:Header.Get", "http:Header.Values"}},
	{"SH-G10", "cookie read", []string{"gin:Cookie", "http:Cookie"}},
	{"SH-G11", "gin JSON bind", []string{"gin:ShouldBindJSON", "gin:BindJSON"}},
	{"SH-G12", "gin Bind by Content-Type", []string{"gin:ShouldBind", "gin:Bind"}},
	{"SH-G13", "gin query bind", []string{"gin:ShouldBindQuery", "gin:BindQuery"}},
	{"SH-G14", "gin uri bind", []string{"gin:ShouldBindUri", "gin:BindUri"}},
	{"SH-G15", "unchecked bind or decode", []string{"check:unchecked"}},
	{"SH-G16", "encoding/json decode", []string{"json:Decoder.Decode"}},
	{"SH-G17", "gin automatic 400", []string{"gin:bind-400"}},
	{"SH-G18", "gin JSON response", []string{"gin:JSON", "gin:IndentedJSON", "gin:PureJSON", "gin:AbortWithStatusJSON"}},
	{"SH-G19", "gin string response", []string{"gin:String"}},
	{"SH-G20", "gin status only", []string{"gin:Status", "gin:AbortWithStatus", "gin:AbortWithError", "gin:Redirect"}},
	{"SH-G21", "net/http WriteHeader", []string{"http:WriteHeader"}},
	{"SH-G22", "net/http Error and NotFound", []string{"http:Error", "http:NotFound"}},
	{"SH-G23", "net/http Redirect", []string{"http:Redirect"}},
	{"SH-G24", "encoding/json encode", []string{"json:Encoder.Encode", "http:Header().Set"}},
	{"SH-G25", "status values", []string{"status:literal", "status:constant", "status:argument"}},
	{"SH-G26", "struct schema", []string{"json:struct"}},
	{"SH-G27", "gin validation presence", []string{"tag:binding-required", "tag:form-default"}},
	{"SH-G28", "gin form mapping", []string{"tag:form", "tag:uri"}},
	{"SH-G29", "inline map response", []string{"gin:H", "literal:map"}},
	{"SH-G30", "value types", []string{"type:composite", "type:var", "type:new", "type:parameter", "type:argument"}},
	{"SH-G31", "refused types", []string{"unknown:marshaler", "unknown:external", "unknown:generic", "unknown:interface", "unknown:recursive", "unknown:embedded", "unknown:defined"}},
	{"SH-G32", "proven call targets", []string{"call:function", "call:package-function", "call:receiver-method", "call:typed-method", "call:result-method"}},
	{"SH-G33", "calls not followed", []string{"stop:unresolved", "stop:ambiguous", "stop:external", "stop:opaque_type", "stop:transformed"}},
	{"SH-G34", "names passed in", []string{"name:literal", "name:constant", "name:argument"}},
	{"SH-G35", "gin's request and writer", []string{"gin:Request", "gin:Writer"}},
	{"SH-G36", "generated servers", []string{"oapi:generated"}},
	{"SH-G37", "section states", []string{"state:partial", "security:not_read"}},
	{"SH-G38", "chain records", []string{"follow:one-per-route"}},
	{"D", "anything no row covers", nil},
}
