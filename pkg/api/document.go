// Package api defines public, serialized Aginctus result projections.
//
// API documents are distinct from internal domain objects, Incus SDK types,
// accepted-state revisions, and execution telemetry. This package must not
// import internal implementations or command-line machinery.
package api

// APIVersionV1Alpha1 identifies the initial experimental result schema.
// This version is independent of the Aginctus binary and accepted-state revision.
const APIVersionV1Alpha1 = "aginctus.dev/v1alpha1"

// TypeMeta identifies the schema of a top-level machine-readable document.
// Embed it in a named public response type; collection elements should not
// redundantly contain a TypeMeta.
type TypeMeta struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
}

// NewTypeMeta constructs the type header for a v1alpha1 response.
func NewTypeMeta(kind string) TypeMeta {
	return TypeMeta{APIVersion: APIVersionV1Alpha1, Kind: kind}
}

// TypeMetadata allows the shared machine serializer to accept only
// explicitly typed response documents.
func (m TypeMeta) TypeMetadata() TypeMeta { return m }

// Document is a public top-level structured response. Named response
// structs can satisfy it by embedding TypeMeta.
type Document interface {
	TypeMetadata() TypeMeta
}
