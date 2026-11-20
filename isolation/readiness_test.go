package isolation

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadinessCannotExecuteOrAuthorize(t *testing.T) {
	b := &Broker{Token: strings.Repeat("x", 32), Policy: Policy{Sessions: map[string][]string{"task-1": {"198.18.0.10/32"}}}, slots: make(chan struct{}, 2)}
	for _, tc := range []struct {
		session  string
		auth     bool
		status   int
		approved bool
	}{
		{"task-1", false, 401, false}, {"task-1", true, 200, true}, {"task-2", true, 200, false}, {"../task-1", true, 400, false},
	} {
		r := httptest.NewRequest(http.MethodPost, "/v1/readiness", strings.NewReader(`{"session":"`+tc.session+`"}`))
		if tc.auth {
			r.Header.Set("Authorization", "Bearer "+b.Token)
		}
		w := httptest.NewRecorder()
		b.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s: got %d", tc.session, w.Code)
		}
		if tc.status == 200 && strings.Contains(w.Body.String(), `"policy_approved":true`) != tc.approved {
			t.Fatal(w.Body.String())
		}
	}
	r := httptest.NewRequest(http.MethodPost, "/v1/readiness", strings.NewReader(`{"session":"task-1","tool":"Bash"}`))
	r.Header.Set("Authorization", "Bearer "+b.Token)
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("readiness accepted execution fields")
	}
	if len(b.Policy.Sessions) != 1 || len(b.runs) != 0 || len(b.known) != 0 {
		t.Fatal("Readiness mutated execution/policy state")
	}
}
