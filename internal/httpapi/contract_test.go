package httpapi

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	generated "github.com/simpul/hr-backend/internal/api/generated"
	"github.com/simpul/hr-backend/internal/config"
)

func TestRegisteredRoutesMatchOpenAPI(t *testing.T) {
	// A nil database and Redis are deliberate: this test asserts route/spec parity, which needs
	// the router to be constructed, not the dependencies to work. Behaviour is verified
	// separately against a running server.
	server, err := NewServer(config.Config{
		Environment: "test", AccessTokenSecret: strings.Repeat("x", 32),
		AccessTokenTTL: 1, EncryptionKey: []byte("01234567890123456789012345678901"),
	}, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	registered := map[string]bool{}
	for _, route := range server.Router().Routes() {
		if !strings.HasPrefix(route.Path, "/v1/") || route.Path == "/v1/ws" {
			continue
		}
		path := strings.TrimPrefix(route.Path, "/v1")
		for _, name := range []string{"id", "provider", "key"} {
			path = strings.ReplaceAll(path, ":"+name, "{"+name+"}")
		}
		registered[route.Method+" "+path] = true
	}
	document, err := generated.GetSwagger()
	if err != nil {
		t.Fatal(err)
	}
	for path, item := range document.Paths.Map() {
		for method := range item.Operations() {
			key := strings.ToUpper(method) + " " + path
			if !registered[key] {
				t.Errorf("OpenAPI operation is not registered: %s", key)
			}
			delete(registered, key)
		}
	}
	for extra := range registered {
		t.Errorf("registered API operation is missing from OpenAPI: %s", extra)
	}
}
