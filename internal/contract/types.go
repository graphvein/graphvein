// Package contract defines the only fact boundary consumed by the application.
package contract

// Version protects downstream consumers from incompatible fact semantics.
const Version = 1

// Indexer identifies the producer so runs can be reproduced.
type Indexer struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Header scopes a complete snapshot to a stable repository identifier.
type Header struct {
	Type            string  `json:"type"`
	ContractVersion int     `json:"contract_version"`
	Indexer         Indexer `json:"indexer"`
	Repository      string  `json:"repository"`
	GeneratedAt     string  `json:"generated_at"`
	Commit          string  `json:"commit,omitempty"`
}

// File carries freshness independently of git availability.
type File struct {
	Type        string `json:"type"`
	Path        string `json:"path"`
	Language    string `json:"language"`
	Precision   string `json:"precision"`
	Size        int64  `json:"size"`
	ContentHash string `json:"content_hash"`
	GitBlobOID  string `json:"git_blob_oid,omitempty"`
}

// Span uses half-open byte columns so a consumer can slice UTF-8 source exactly.
type Span struct {
	StartLine   int `json:"start_line"`
	StartColumn int `json:"start_column"`
	EndLine     int `json:"end_line"`
	EndColumn   int `json:"end_column"`
}

// Node separates stable identity from the source revision described by its hash.
type Node struct {
	Type          string         `json:"type"`
	Identity      string         `json:"identity"`
	Kind          string         `json:"kind"`
	Level         int            `json:"level"`
	Name          string         `json:"name"`
	QualifiedName string         `json:"qualified_name"`
	File          string         `json:"file"`
	Span          *Span          `json:"span,omitempty"`
	ContentHash   string         `json:"content_hash"`
	Attributes    map[string]any `json:"attributes"`
}

// Evidence gives an edge a verifiable location rather than an unsupported claim.
type Evidence struct {
	File string `json:"file"`
	Span Span   `json:"span"`
}

// Edge carries its precision explicitly for projections of the graph.
type Edge struct {
	Type       string     `json:"type"`
	Kind       string     `json:"kind"`
	Source     string     `json:"source"`
	Target     string     `json:"target"`
	Provenance string     `json:"provenance"`
	Confidence float64    `json:"confidence"`
	Evidence   []Evidence `json:"evidence"`
}

// Diagnostic preserves failures without preventing other facts from being ingested.
type Diagnostic struct {
	Type     string `json:"type"`
	File     string `json:"file"`
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// Counts counts wire records, including invalid body records and excluding framing.
type Counts struct {
	Records    int `json:"records"`
	File       int `json:"file"`
	Node       int `json:"node"`
	Edge       int `json:"edge"`
	Diagnostic int `json:"diagnostic"`
}

// Footer makes silent truncation detectable before a snapshot is published.
type Footer struct {
	Type   string `json:"type"`
	Counts Counts `json:"counts"`
}
