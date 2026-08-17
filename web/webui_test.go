package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesIndexAndSPAPaths(t *testing.T) {
	handler := Handler()
	for _, target := range []string{"/", "/work-items/example"} {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", target, response.Code)
		}
		if !strings.Contains(response.Body.String(), "Kairos Operations Console") {
			t.Fatalf("GET %s did not serve the console index", target)
		}
	}
}
