package routeshape

// ModelTypeRows is the claim contract of route model types and of change
// impact through them (docs/contract/claims/route-model-types.md, rows
// MT-1..MT-19 and D), in table order. Forms are written "group:value"
// like Rows'; TestModelTypeContract keeps the table and the document 1:1
// and checks that every modelled value is the form of exactly one row.
// Rows MT-13..MT-19 are applied by internal/changes (relation I16).
var ModelTypeRows = []Row{
	{"MT-1", "edge uses_model", []string{"edge:" + UsesModelEdge}},
	{"MT-2", "attribute model_uses", []string{"attribute:" + ModelUsesKey}},
	{"MT-3", "proven declarations only", []string{"declaration:resolved", "declaration:emitted"}},
	{"MT-4", "FastAPI models", []string{"rule:SH-F22", "python:base"}},
	{"MT-5", "TypeScript types and validators", []string{"rule:SH-E22", "ts:validator_const"}},
	{"MT-6", "Go structs", []string{"go:named", "go:embedded", "go:form"}},
	{"MT-7", "Rust serde types", []string{"rust:serde_type"}},
	{"MT-8", "nested models", []string{"nested:field", "nested:items", "nested:ref"}},
	{"MT-9", "kind label", []string{"kind:" + string(Fact), "kind:" + string(Intended), "kind:" + string(Returned)}},
	{"MT-10", "mapped uses", []string{"kind:" + string(Mapped)}},
	{"MT-11", "edge provenance", []string{"provenance:fact", "provenance:inferred"}},
	{"MT-12", "no type, no use", []string{"kind:" + string(ReadInHandler), "schema:not_read"}},
	{"MT-13", "changes: edge on its route", []string{"label:models_changed", "field:model_edges"}},
	{"MT-14", "impact relation I16", []string{"relation:I16"}},
	{"MT-15", "wording", []string{"wording:through_model"}},
	{"MT-16", "mapped never a fact", []string{"claim:mapped_inferred"}},
	{"MT-17", "caps", []string{"cap:depth", "cap:fan_out", "cap:reached"}},
	{"MT-18", "a changed field inside a model", []string{"seed:declaration_body"}},
	{"MT-19", "UI", []string{"ui:impact_step"}},
	{"D", "anything no row covers", nil},
}
