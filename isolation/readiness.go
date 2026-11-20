package isolation

import (
	"context"
	"encoding/json"
	"net/http"
)

// CheckPolicy is read-only: no container, target connection, lease or ledger entry
// is created. The trusted operator's policy remains outside the API's authority.
func CheckPolicy(ctx context.Context, session string) (bool, error) {
	out, err := brokerCall(ctx, Request{Session: session}, "/v1/readiness", brokerLeaseClient)
	if err != nil {
		return false, err
	}
	return out.PolicyApproved, nil
}

func (b *Broker) serveReadiness(w http.ResponseWriter, r *http.Request) {
	// This control request cannot carry tool arguments or execution state.
	var envelope struct {
		Session string `json:"session"`
	}
	if decodeRequest(w, r, 4096, &envelope) != nil {
		http.Error(w, "invalid readiness request", 400)
		return
	}
	if envelope.Session != "" && !sessionPattern.MatchString(envelope.Session) {
		http.Error(w, "invalid session", 400)
		return
	}
	_ = json.NewEncoder(w).Encode(Response{PolicyApproved: len(b.Policy.Sessions[envelope.Session]) > 0})
}
