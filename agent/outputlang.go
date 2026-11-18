package agent

// OutputLanguage remains an integration hook for existing callers. This edition
// supports English only; stale saved locale preferences cannot change it.
var OutputLanguage func() string

const defaultOutputLang = "en"

var SupportedOutputLangs = []string{"en"}

func normalizeLang(string) string { return "en" }
func resolveOutputLang() string   { return "en" }
