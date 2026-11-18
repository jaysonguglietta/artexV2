package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/notify"
)

// Global settings keys stored in the existing settings table.
const (
	// settingNotifyEnabled is an emergency global off switch, on by default. Configuring a channel is the actual feature-enablement step.
	settingNotifyEnabled = "notify_enabled"
	// settingNotifyPublicBaseURL is the external finding-detail link origin, such as https://artex.example.com. Empty disables link buttons; no existing global external-URL setting was reusable.
	settingNotifyPublicBaseURL = "notify_public_base_url"
	// settingNotifyDigestMinutes controls the digest interval in minutes.
	settingNotifyDigestMinutes = "notify_digest_interval_min"
)

const (
	// notifyTick sets the polling interval and the main event-to-message latency; three seconds bounds this engine's responsiveness.
	notifyTick = 3 * time.Second
	// notifyBackgroundDisabledEnv disables the background delivery loop only for deterministic integration fixtures that drive stepRealtime/stepDigest directly. Without it, normal startup behavior is unchanged.
	notifyBackgroundDisabledEnv = "ARTEX_NOTIFY_BACKGROUND_DISABLED"
	// notifyLease must substantially exceed a delivery's worst-case duration (the channel HTTP client timeout is 15 seconds), or multiple dispatchers can send the same row concurrently.
	notifyLease = 3 * time.Minute
	// notifyFanOutPerTick bounds fanout work so newly enabled channels do not expand the entire historical backlog in one tick.
	notifyFanOutPerTick = 200
	// notifyDefaultDigestMinutes is the default digest interval.
	notifyDefaultDigestMinutes = 30
	// notifyUnlimitedBurstPerTick bounds work even for unlimited channels, preventing a large scan from blocking an entire polling iteration.
	notifyUnlimitedBurstPerTick = 50
	// notifyMaxSendsPerChannelPerTick is bounded by the claim lease. With multiple processes sharing a database, a lease expiring before later serial sends finish causes duplicate claims, sends and attempt accounting.
// A three-minute lease and 30-second send deadline permit at most five sends with 30 seconds of margin; six would leave none. TestNotifyTickBudgetFitsWithinLease pins this relationship.
	notifyMaxSendsPerChannelPerTick = 5
	// notifySendTimeout bounds one delivery; its product with the per-tick send count must remain below notifyLease.
	notifySendTimeout = 30 * time.Second
)

// notifyBackoff indexes delays by attempt count. Its three attempts including the first must stay aligned with db.MaxNotifyAttempts.
var notifyBackoff = []time.Duration{
	time.Second,
	5 * time.Second,
	30 * time.Second,
}

// Delivery failure/defer reasons are stored in notification_deliveries.last_error and displayed directly in the delivery-history UI; keep this narrative English.
const (
	errDeliveryChannelKindUnregistered = "Unregistered channel type: %q"
	errDeliverySnapshotUnrenderable    = "Could not parse the event snapshot, so this vulnerability could not be turned into a message"
	errDeliveryChannelLengthCapped     = "This message reached the channel length limit, so only the first %d were sent and the rest are deferred to the next batch"
	errDeliveryNoDeliveredCount        = "The channel did not report the number of messages sent (delivered=%d)"
	errDeliveryRetryExhausted          = "Still failed after %d retries: %s"
	errDeliverySnapshotEmpty           = "The event snapshot for delivery item #%d is empty"
	errDeliverySnapshotParse           = "Failed to parse the event snapshot of delivery item #%d: %w"
	errDeliveryBatchAllUnparseable     = "Failed to parse all %d deliveries in the summary batch"
)

// Notifier runs independently of Scheduler. Its three-second delivery cadence and failures must not interfere with agent-trigger scheduling.
type Notifier struct {
	s  *Server
	pg *db.DB

	// mu protects the small channel token-bucket map; low contention does not justify a more complex locking structure.
	mu      sync.Mutex
	buckets map[int64]*notifyBucket
}

// notifyBucket is a per-channel token bucket. Constant refill avoids fixed-window boundary bursts such as 20 sends just before and 20 just after a minute boundary.
type notifyBucket struct {
	tokens   float64
	lastFill time.Time
}

func newNotifier(s *Server) *Notifier {
	return &Notifier{s: s, pg: s.m.pg, buckets: map[int64]*notifyBucket{}}
}

// Run loops until cancellation and is started once by server.New.
func (n *Notifier) Run(ctx context.Context) {
	if n.pg == nil {
		return
	}
	t := time.NewTicker(notifyTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n.step(ctx)
		}
	}
}

// step fans out new events and delivers due work. Errors are logged per tick and retried naturally; a notification failure must not terminate the process.
func (n *Notifier) step(ctx context.Context) {
	if !n.enabled() {
		return
	}
	if _, _, err := n.pg.FanOutPendingEvents(ctx, notifyFanOutPerTick); err != nil {
		log.Printf("[notify] failed to dispatch events: %v", err)
		return
	}
	channels, err := n.pg.ListNotificationChannels(ctx)
	if err != nil {
		log.Printf("[notify] failed to read channels: %v", err)
		return
	}
	baseURL := n.publicBaseURL()
	for _, ch := range channels {
		if !ch.IsEnabled() {
			continue
		}
		// Tokens count messages/HTTP requests, not findings. A digest consumes one token for the whole batch. Ask the bucket before claiming rows so rate limiting does not waste retry attempts.
		now := time.Now()
		if ch.Mode == db.NotifyModeDigest {
			tokens, claimLimit := digestTickPlan()
			if n.takeTokens(ch.ID, ch.RatePerMin, tokens, now) <= 0 {
				continue
			}
			n.stepDigest(ctx, ch, claimLimit, baseURL)
			continue
		}
		allow := n.takeTokens(ch.ID, ch.RatePerMin, notifyMaxSendsPerChannelPerTick, now)
		if allow <= 0 {
			continue
		}
		n.stepRealtime(ctx, ch, allow, baseURL)
	}
}

// digestTickPlan separates two units: tokens counts outgoing messages (always one per digest), while claimLimit bounds the number of findings in a batch independently of the send budget.
// Using the request budget as batch size previously reduced a 20/minute digest channel to one finding per digest, bypassing the intended MaxDigestBatchSize. TestDigestTickPlanDecouplesBatchSizeFromSendBudget pins the distinction because direct stepDigest fixtures previously skipped this planning path.
func digestTickPlan() (tokens, claimLimit int) {
	return 1, db.MaxDigestBatchSize
}

// stepRealtime claims and sends individual finding deliveries for one channel.
func (n *Notifier) stepRealtime(ctx context.Context, ch *db.NotificationChannel, allow int, baseURL string) {
	deliveries, err := n.pg.ClaimRealtimeDeliveries(ctx, ch.ID, allow, notifyLease)
	if err != nil {
		log.Printf("[notify] failed to claim realtime deliveries channel=%d: %v", ch.ID, err)
		return
	}
	if len(deliveries) == 0 {
		return
	}
	channel, cfg, ok := n.adapt(ch)
	if !ok {
		_ = n.pg.FailDeliveries(ctx, deliveryIDs(deliveries), fmt.Sprintf(trMsg(errDeliveryChannelKindUnregistered), ch.Kind))
		return
	}
	for _, dl := range deliveries {
		msg, err := n.renderSingle(ctx, dl, baseURL)
		if err != nil {
			// Rendering failure is a local data problem; retrying will not repair it.
			_ = n.pg.FailDeliveries(ctx, []int64{dl.ID}, err.Error())
			continue
		}
		n.send(ctx, channel, cfg, msg, []*db.NotificationDelivery{dl})
	}
}

// stepDigest aggregates a due channel batch into a single outgoing message.
func (n *Notifier) stepDigest(ctx context.Context, ch *db.NotificationChannel, allow int, baseURL string) {
	window := n.digestInterval()
	due, err := n.pg.DigestBatchDue(ctx, ch.ID, window)
	if err != nil {
		log.Printf("[notify] failed to evaluate digest batch channel=%d: %v", ch.ID, err)
		return
	}
	if !due {
		return
	}
	deliveries, err := n.pg.ClaimDigestBatch(ctx, ch.ID, allow, notifyLease)
	if err != nil {
		log.Printf("[notify] failed to claim digest batch channel=%d: %v", ch.ID, err)
		return
	}
	if len(deliveries) == 0 {
		return
	}
	channel, cfg, ok := n.adapt(ch)
	if !ok {
		_ = n.pg.FailDeliveries(ctx, deliveryIDs(deliveries), fmt.Sprintf(trMsg(errDeliveryChannelKindUnregistered), ch.Kind))
		return
	}
	msg, included, err := n.renderBatch(ctx, deliveries, baseURL, int(window.Minutes()))
	if err != nil {
		_ = n.pg.FailDeliveries(ctx, deliveryIDs(deliveries), err.Error())
		return
	}
	// Explicitly fail malformed snapshots excluded from the message; otherwise they remain sending until lease expiry and are repeatedly reclaimed.
	if skipped := excludeDeliveries(deliveries, included); len(skipped) > 0 {
		reason := errDeliverySnapshotUnrenderable
		if fErr := n.pg.FailDeliveries(ctx, deliveryIDs(skipped), reason); fErr != nil {
			log.Printf("[notify] failed to mark deliveries with bad snapshots channel=%s ids=%v: %v", ch.Kind, deliveryIDs(skipped), fErr)
		}
		log.Printf("[notify] skipped %d deliveries with unparsable snapshots channel=%d", len(skipped), ch.ID)
	}
	// Only pass included deliveries to send. included[i] must correspond exactly to msg.Items[i] so a channel's delivered-prefix count maps to the correct rows.
	n.send(ctx, channel, cfg, msg, included)
}

// send applies one delivery result to a shared batch: success or retry as a unit. Partial channel-length packing is the exception: when only the first K items fit, later items must remain queued rather than being silently marked delivered.
func (n *Notifier) send(ctx context.Context, channel notify.Channel, cfg map[string]any, msg notify.Message, deliveries []*db.NotificationDelivery) {
	// Bound one delivery so a stalled channel cannot block the rest of the tick indefinitely.
	sendCtx, cancel := context.WithTimeout(ctx, notifySendTimeout)
	defer cancel()
	delivered, err := channel.Send(sendCtx, cfg, msg)
	if err == nil && delivered > 0 {
		if delivered > len(deliveries) {
			// A channel reporting more delivered items than supplied indicates a renderer bug. Clamp to all delivered and log the inconsistency rather than corrupting row accounting.
			log.Printf("[notify] channel reported %d delivered, more than the %d sent channel=%s; treating all as delivered",
				delivered, len(deliveries), channel.Kind())
			delivered = len(deliveries)
		}
		sent, rest := deliveries[:delivered], deliveries[delivered:]
		if err := n.pg.MarkDeliveriesSent(ctx, deliveryIDs(sent)); err != nil {
			log.Printf("[notify] failed to mark as delivered channel=%s ids=%v: %v", channel.Kind(), deliveryIDs(sent), err)
		}
		if len(rest) > 0 {
			// Requeue items beyond the channel size limit immediately with DeferDeliveries, which refunds the optimistic claim attempt. They were not delivery failures.
			if err := n.pg.DeferDeliveries(ctx, deliveryIDs(rest),
				fmt.Sprintf(trMsg(errDeliveryChannelLengthCapped), delivered)); err != nil {
				log.Printf("[notify] failed to queue remaining segments channel=%s ids=%v: %v", channel.Kind(), deliveryIDs(rest), err)
			}
		}
		return
	}
	if err == nil {
		// A channel reporting neither an error nor any delivered count is a failure with backoff, not a permanently unacknowledged claim.
		err = fmt.Errorf(trMsg(errDeliveryNoDeliveredCount), delivered)
	}

	// Retry exhaustion is decided per row, not from the maximum attempt count in a batch. An old exhausted row must not permanently fail fresh findings that have never been retried.
	permanent := notify.IsPermanent(err)
	var failIDs, exhaustedIDs []int64
	byDelay := map[time.Duration][]int64{}
	for _, dl := range deliveries {
		switch {
		case permanent:
			failIDs = append(failIDs, dl.ID)
		case dl.Attempts >= db.MaxNotifyAttempts:
			exhaustedIDs = append(exhaustedIDs, dl.ID)
		default:
			delay := notifyBackoff[min(dl.Attempts, len(notifyBackoff)-1)]
			byDelay[delay] = append(byDelay[delay], dl.ID)
		}
	}

	if len(failIDs) > 0 {
		if fErr := n.pg.FailDeliveries(ctx, failIDs, err.Error()); fErr != nil {
			log.Printf("[notify] failed to mark failure state channel=%s ids=%v: %v", channel.Kind(), failIDs, fErr)
		}
	}
	if len(exhaustedIDs) > 0 {
		reason := fmt.Sprintf(trMsg(errDeliveryRetryExhausted), db.MaxNotifyAttempts, err)
		if fErr := n.pg.FailDeliveries(ctx, exhaustedIDs, reason); fErr != nil {
			log.Printf("[notify] failed to mark failure state channel=%s ids=%v: %v", channel.Kind(), exhaustedIDs, fErr)
		}
	}
	// Group rescheduling by the three backoff delays instead of issuing hundreds of individual database updates.
	for delay, group := range byDelay {
		if rErr := n.pg.RescheduleDeliveries(ctx, group, delay, err.Error()); rErr != nil {
			log.Printf("[notify] failed to requeue deliveries channel=%s ids=%v: %v", channel.Kind(), group, rErr)
		}
	}
	if len(failIDs)+len(exhaustedIDs) > 0 {
		log.Printf("[notify] delivery failed channel=%d kind=%s permanent=%d retries_exhausted=%d pending_retry=%d: %s",
			deliveries[0].ChannelID, channel.Kind(), len(failIDs), len(exhaustedIDs), len(byDelay), err)
	}
}

// excludeDeliveries returns pointers in all absent from keep. Excluded message entries require an explicit failure/defer outcome.
func excludeDeliveries(all, keep []*db.NotificationDelivery) []*db.NotificationDelivery {
	inKeep := make(map[*db.NotificationDelivery]bool, len(keep))
	for _, dl := range keep {
		inKeep[dl] = true
	}
	var out []*db.NotificationDelivery
	for _, dl := range all {
		if !inKeep[dl] {
			out = append(out, dl)
		}
	}
	return out
}

// adapt resolves the registered channel type and parses config. Unknown types fail permanently rather than retrying forever.
func (n *Notifier) adapt(ch *db.NotificationChannel) (notify.Channel, map[string]any, bool) {
	channel, ok := notify.Get(ch.Kind)
	if !ok {
		return nil, nil, false
	}
	var cfg map[string]any
	if len(ch.Config) > 0 {
		// Use an empty map on malformed config so channel Validate can identify missing fields more helpfully than a generic JSON error.
		_ = json.Unmarshal(ch.Config, &cfg)
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	return channel, cfg, true
}

// renderSingle renders one finding notification.
func (n *Notifier) renderSingle(ctx context.Context, dl *db.NotificationDelivery, baseURL string) (notify.Message, error) {
	snap, err := parseSnapshot(dl)
	if err != nil {
		return notify.Message{}, err
	}
	item, err := n.itemFor(ctx, snap, baseURL)
	if err != nil {
		return notify.Message{}, err
	}
	return notify.Message{Items: []notify.Item{item}, HomeURL: baseURL}, nil
}

// renderBatch decodes snapshots independently and skips only malformed entries. included must remain in exact index correspondence with msg.Items so the delivered-prefix count marks the correct records. The caller explicitly fails skipped snapshots.
func (n *Notifier) renderBatch(ctx context.Context, deliveries []*db.NotificationDelivery, baseURL string, windowMinutes int) (notify.Message, []*db.NotificationDelivery, error) {
	items := make([]notify.Item, 0, len(deliveries))
	included := make([]*db.NotificationDelivery, 0, len(deliveries))
	for _, dl := range deliveries {
		snap, err := parseSnapshot(dl)
		if err != nil {
			// Exclude a malformed snapshot from both the rendered items and included; the caller must mark it failed rather than delivered.
			log.Printf("[notify] skipped unparsable snapshot in digest batch delivery=%d: %v", dl.ID, err)
			continue
		}
		item, err := n.itemFor(ctx, snap, baseURL)
		if err != nil {
			return notify.Message{}, nil, err
		}
		items = append(items, item)
		included = append(included, dl)
	}
	if len(items) == 0 {
		return notify.Message{}, nil, fmt.Errorf(trMsg(errDeliveryBatchAllUnparseable), len(deliveries))
	}
	return notify.Message{
		Items:         items,
		Batch:         true,
		WindowMinutes: windowMinutes,
		HomeURL:       baseURL,
	}, included, nil
}

// itemFor renders an event snapshot and resolves asset labels and finding-detail links.
func (n *Notifier) itemFor(ctx context.Context, snap notify.Snapshot, baseURL string) (notify.Item, error) {
	assets, err := n.pg.NotificationAssetNames(ctx, snap.AssetIDs)
	if err != nil {
		// An asset-name lookup failure must not suppress the entire notification; omit that optional line instead.
		log.Printf("[notify] failed to resolve asset name finding=%d: %v", snap.FindingID, err)
	}
	item := notify.Item{
		FindingID:  snap.FindingID,
		Name:       snap.Name,
		VulnClass:  snap.VulnClass,
		Severity:   snap.Severity,
		Summary:    snap.Summary,
		Assets:     assets,
		FromStatus: snap.FromStatus,
		ToStatus:   snap.ToStatus,
	}
	if baseURL != "" {
		// The finding-detail route reads the finding ID from its id query parameter.
		item.DetailURL = fmt.Sprintf("%s/function/findings/detail?id=%d", baseURL, snap.FindingID)
	}
	return item, nil
}

// takeTokens consumes at most want message tokens and returns the actual number. A realtime message consumes one; a digest batch also consumes one.
// Capacity equals the channel's per-minute limit and refills continuously. A nonpositive rate means unlimited, with a finite per-tick ceiling. The want bound prevents unused token withdrawals from silently destroying accumulated burst capacity.
func (n *Notifier) takeTokens(channelID int64, ratePerMin, want int, now time.Time) int {
	if want <= 0 {
		return 0
	}
	if ratePerMin <= 0 {
		return min(want, notifyUnlimitedBurstPerTick)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	b := n.buckets[channelID]
	if b == nil {
		b = &notifyBucket{tokens: float64(ratePerMin), lastFill: now}
		n.buckets[channelID] = b
	}
	// Refill from actual elapsed time at ratePerMin/60 tokens per second.
	if elapsed := now.Sub(b.lastFill).Seconds(); elapsed > 0 {
		b.tokens = minF(float64(ratePerMin), b.tokens+elapsed*float64(ratePerMin)/60)
		b.lastFill = now
	}
	// Add a tiny epsilon before integer conversion to avoid rounding 0.5+0.5 to 0.9999999999 and losing an available token. 1e-9 cannot meaningfully admit an underfunded request.
	take := min(int(b.tokens+1e-9), want)
	if take <= 0 {
		return 0
	}
	b.tokens -= float64(take)
	return take
}

// enabled reads the global emergency switch.
func (n *Notifier) enabled() bool {
	return n.pg.GetBool(settingNotifyEnabled, true)
}

// publicBaseURL returns the external backlink base without a trailing slash.
func (n *Notifier) publicBaseURL() string {
	v, ok, err := n.pg.GetSetting(settingNotifyPublicBaseURL)
	if err != nil || !ok {
		return ""
	}
	return trimTrailingSlash(v)
}

// digestInterval validates the configured interval and falls back to its default.
func (n *Notifier) digestInterval() time.Duration {
	v, ok, err := n.pg.GetSetting(settingNotifyDigestMinutes)
	if err != nil || !ok {
		return time.Duration(notifyDefaultDigestMinutes) * time.Minute
	}
	m := 0
	if _, err := fmt.Sscanf(v, "%d", &m); err != nil || m <= 0 {
		return time.Duration(notifyDefaultDigestMinutes) * time.Minute
	}
	return time.Duration(m) * time.Minute
}

// parseSnapshot decodes the delivery event snapshot.
func parseSnapshot(dl *db.NotificationDelivery) (notify.Snapshot, error) {
	var snap notify.Snapshot
	if len(dl.Snapshot) == 0 {
		return snap, fmt.Errorf(trMsg(errDeliverySnapshotEmpty), dl.ID)
	}
	if err := json.Unmarshal(dl.Snapshot, &snap); err != nil {
		return snap, fmt.Errorf(trMsg(errDeliverySnapshotParse), dl.ID, err)
	}
	if snap.Kind == "" {
		// The event row owns the kind; the snapshot copy may have been written by an older version.
		snap.Kind = dl.EventKind
	}
	return snap, nil
}

func deliveryIDs(deliveries []*db.NotificationDelivery) []int64 {
	out := make([]int64, 0, len(deliveries))
	for _, dl := range deliveries {
		out = append(out, dl.ID)
	}
	return out
}

func trimTrailingSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
