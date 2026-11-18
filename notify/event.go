package notify

// Snapshot is the notification_events.snapshot JSONB contract between finding transactions and delivery/filtering. It belongs to the notification domain; db only serializes it. Store event-time fields rather than reloading mutable findings so later severity/status edits cannot misrepresent the original event, and fanout/rendering need no findings/tasks/assets joins.
type Snapshot struct {
	// Event type: finding_created or finding_status_changed.
	Kind      string  `json:"kind"`
	FindingID int64   `json:"finding_id"`
	TaskID    int64   `json:"task_id"`
	VulnClass string  `json:"vulnclass"`
	Name      string  `json:"name"`
	Severity  string  `json:"severity"`
	Summary   string  `json:"summary"`
	AssetIDs  []int64 `json:"asset_ids"`
	// Populated only for finding_status_changed.
	FromStatus string `json:"from_status,omitempty"`
	ToStatus   string `json:"to_status,omitempty"`
}

// Item is a finding prepared for channel rendering.
type Item struct {
	FindingID int64
	Name      string
	VulnClass string
	Severity  string
	Summary   string
	// Assets contains resolved display names, such as domains or IPs. The server fills it because this package has no database dependency.
	Assets []string
	// DetailURL is the finding backlink; empty means no public_base_url is configured.
	DetailURL string
	// Status-change fields render the transition when both are populated.
	FromStatus string
	ToStatus   string
}

// IsStatusChange identifies a disposition-change event.
func (i Item) IsStatusChange() bool { return i.FromStatus != "" || i.ToStatus != "" }

// Title prefers the assigned name, then vulnclass, then a placeholder; it never returns an empty display title.
func (i Item) Title() string {
	if i.Name != "" {
		return i.Name
	}
	if i.VulnClass != "" {
		return i.VulnClass
	}
	return "(unnamed finding)"
}

// Message contains a complete channel send.
type Message struct {
	// A single message has one item; a digest contains a batch. Callers must provide at least one item.
	Items []Item
	// Batch selects digest formatting with a count and time window.
	Batch bool
	// WindowMinutes is the configured digest interval used in its introduction. Passing it explicitly keeps rendering deterministic rather than deriving it from wall time.
	WindowMinutes int
	// HomeURL links to the dashboard; empty omits that entry point.
	HomeURL string
}
