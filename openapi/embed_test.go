package openapi

import (
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	generated "github.com/simpul/hr-backend/internal/api/generated"
)

func TestEmbeddedDocumentIsPresent(t *testing.T) {
	if len(Document) == 0 {
		t.Fatal("embedded OpenAPI document is empty — the go:embed directive is not matching openapi.yaml")
	}
	if !strings.HasPrefix(strings.TrimSpace(string(Document)), "openapi:") {
		t.Fatalf("embedded document does not begin with an openapi: version key, so it is not the specification")
	}
}

// TestEmbeddedDocumentMatchesGeneratedSpec is a drift guard.
//
// The document served at /openapi.yaml and the ServerInterface this router is compiled
// against are both derived from openapi.yaml, but only the generated copy is refreshed by
// `go generate`. Editing the YAML without regenerating would publish a contract advertising
// endpoints the binary does not implement — precisely the failure a contract-first design
// exists to prevent — and nothing else in the suite would notice, because the parity test in
// internal/httpapi compares the *generated* spec against the routes, not the served file.
func TestEmbeddedDocumentMatchesGeneratedSpec(t *testing.T) {
	served, err := openapi3.NewLoader().LoadFromData(Document)
	if err != nil {
		t.Fatalf("embedded document is not valid OpenAPI: %v", err)
	}
	compiled, err := generated.GetSwagger()
	if err != nil {
		t.Fatalf("load generated spec: %v", err)
	}

	servedOps, compiledOps := operations(served), operations(compiled)
	if diff := onlyIn(servedOps, compiledOps); len(diff) > 0 {
		t.Errorf("published at /openapi.yaml but not implemented: %v\nrun `go generate ./internal/api/...`", diff)
	}
	if diff := onlyIn(compiledOps, servedOps); len(diff) > 0 {
		t.Errorf("implemented but absent from the published contract: %v", diff)
	}
}

// operations lists every method+path pair in a document, sorted so failures are stable.
func operations(document *openapi3.T) []string {
	var out []string
	for path, item := range document.Paths.Map() {
		for method := range item.Operations() {
			out = append(out, strings.ToUpper(method)+" "+path)
		}
	}
	sort.Strings(out)
	return out
}

// onlyIn returns the entries of a that are absent from b.
func onlyIn(a, b []string) []string {
	present := make(map[string]bool, len(b))
	for _, item := range b {
		present[item] = true
	}
	var out []string
	for _, item := range a {
		if !present[item] {
			out = append(out, item)
		}
	}
	return out
}
