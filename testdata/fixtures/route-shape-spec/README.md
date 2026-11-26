# route-shape-spec fixture

FastAPI routes documented by three committed specs, for the mapped route
shape items of [route-shape-spec](../../../docs/contract/claims/route-shape-spec.md)
(rows SH-O1..SH-O19). `TestSpecShapeGolden` (internal/index) indexes it,
checks `route-shape-spec.golden.json` and that every form of the claim
table is reached; `TestFixtureDocuments` (internal/openapi) checks the
generated document `route-shape-spec.openapi.golden.json`, where the
mapped items stay outside the standard fields.

- `openapi30.yaml` (OpenAPI 3.0.3): parameters the code contradicts
  (type, presence, names), a path-item parameter overridden by the
  operation, an ignored `Accept` header, cookie and `content` parameters,
  referenced parameters and responses, a request body the code proves
  absent, a 201 the spec does not document, `security: []` against a
  required bearer token, a scheme whose name the code also uses, and the
  `Item` schema with every 3.0 schema form (nullable, enums, readOnly
  required, cycles, external and missing references, compositions).
- `openapi31.json` (OpenAPI 3.1.0): type lists, `anyOf` with `null`,
  `const`, boolean schemas, `$ref` beside keywords, `prefixItems`,
  conditionals, a schema nested deeper than 32 levels; HTTP, OpenID
  Connect, OAuth2, mutual TLS and undefined schemes on an operation whose
  server names an origin (link confidence 0.7).
- `swagger.yaml` (Swagger 2.0): body and formData parameters with
  consumes, `type: file`, produces, root and operation security with
  basic, API key and OAuth2 password schemes.
- `refs/fragment.yaml`: the target of an external reference, which is not
  resolved.
- Further forms (all three specs): `Account` (3.0) with readOnly and
  writeOnly fields behind `$ref` and a single `allOf`, required or not,
  and an enum number float64 cannot hold; `POST /v30/accounts` answers a
  pure `$ref` cycle (`Loop`); `POST /v30/charset` and `POST /v30/wild`
  document the JSON body the code reads as `application/json;
  charset=utf-8` and `*/*` (no "spec differs" note); `Thing` (3.1) gains
  a required readOnly field behind a `oneOf` with `null` and a `oneOf`
  whose other member accepts null; the Swagger photo upload documents a
  `2XX` key.
- `GET /dup` is documented by `openapi30.yaml` and `swagger.yaml`: it gets
  no mapped items.

No ignore files or ignored directory names are used.
