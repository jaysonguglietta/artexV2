package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUnsignedUpdateEndpointsCannotBeEnabled(t *testing.T) {
	t.Setenv("ARTEX_ENABLE_SELF_UPDATE", "1")
	s := &Server{}
	for _, endpoint := range []struct {
		name    string
		handler http.HandlerFunc
	}{{"apply", s.updateApply}, {"rollback", s.updateRollback}} {
		t.Run(endpoint.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			endpoint.handler(w, httptest.NewRequest(http.MethodPost, "/api/update/"+endpoint.name, nil))
			if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "signed") {
				t.Fatalf("unsigned update reachable: %d %s", w.Code, w.Body.String())
			}
		})
	}
}
