package server

import "testing"

// Side-question HTTP and exchange-status messages must be English consistently with the sidequestion package; preserve underlying data and technical identifiers.
func TestSideQuestionAPIErrorsLocalized(t *testing.T) {
	for _, c := range []struct{ label, msg string }{
		{"ctx_not_saved", sideErrCtxNotSaved},
		{"model_config_changed", sideErrModelConfigChanged},
		{"service_unavailable", sideErrServiceUnavailable},
		{"task_archived", sideErrTaskArchived},
		{"worker_deleted", sideErrWorkerDeleted},
		{"bad_question", sideErrBadQuestion},
		{"task_archiving", sideErrTaskArchiving},
		{"request_id_reused", sideErrRequestIDReused},
		{"no_snapshot", sideErrNoSnapshot},
		{"concurrency_limit", sideErrConcurrencyLimit},
		{"answer_stopped", sideErrAnswerStopped},
		{"answer_timeout", sideErrAnswerTimeout},
	} {
		assertEnglishError(t, c.label, c.msg)
	}
}
