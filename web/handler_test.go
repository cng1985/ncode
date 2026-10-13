package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesEmbeddedAppAndClientRoutes(t *testing.T) {
	handler := Handler()
	for _, path := range []string{"/", "/projects/ncode"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("GET %s returned status %d, content type %q, location %q", path, recorder.Code, recorder.Header().Get("Content-Type"), recorder.Header().Get("Location"))
		}
		body, err := io.ReadAll(recorder.Result().Body)
		if err != nil || !strings.Contains(string(body), "NCode") {
			t.Fatalf("GET %s did not return the embedded app index", path)
		}
	}
}

func TestHandlerDoesNotFallbackForMissingAssetsOrAPIs(t *testing.T) {
	handler := Handler()
	for _, path := range []string{"/assets/missing.js", "/api/v1/missing", "/healthz"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("GET %s returned status %d, want 404", path, recorder.Code)
		}
	}
	if recorder := httptest.NewRecorder(); func() bool {
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/", nil))
		return recorder.Code == http.StatusMethodNotAllowed
	}() == false {
		t.Fatalf("POST / returned status %d, want 405", recorder.Code)
	}
}
