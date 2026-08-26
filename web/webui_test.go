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
		if got := response.Header().Get("Content-Security-Policy"); !strings.Contains(got, "frame-ancestors 'none'") {
			t.Fatalf("GET %s CSP = %q, want frame-ancestors restriction", target, got)
		}
		if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("GET %s X-Content-Type-Options = %q, want nosniff", target, got)
		}
		if got := response.Header().Get("X-Frame-Options"); got != "DENY" {
			t.Fatalf("GET %s X-Frame-Options = %q, want DENY", target, got)
		}
		if got := response.Header().Get("Referrer-Policy"); got != "no-referrer" {
			t.Fatalf("GET %s Referrer-Policy = %q, want no-referrer", target, got)
		}
		if !strings.Contains(response.Body.String(), "Kairos Operations Console") {
			t.Fatalf("GET %s did not serve the console index", target)
		}
	}
}
