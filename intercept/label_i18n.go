package intercept

// Approval-action enums remain unchanged; display labels are English only.
var judgeActionLabels = map[string]map[string]string{"en": {"allow": "allow", "deny": "deny", "ask": "ask"}}

func judgeActionLang() string { return "en" }
