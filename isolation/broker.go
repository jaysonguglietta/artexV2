package isolation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Policy is loaded only from an operator-owned file mounted into the broker.
// Neither the API nor a model can expand it through the execution protocol.
type Policy struct {
	Sessions   map[string][]string `json:"sessions"`
	DNS        []string            `json:"dns_servers"`
	Management []string            `json:"management_cidrs"`
}
type brokerRun struct {
	session, id, name string
	ctx               context.Context
	cancel            context.CancelFunc
	ready             chan struct{}
}
type Broker struct {
	gate                  chan struct{}
	Policy                Policy
	Image, Network, Token string
	mu                    sync.Mutex // only short state/ledger operations; never Docker calls
	runs                  map[string]*brokerRun
	closed                map[string]bool
	known                 map[string]bool
	frozen                map[string]bool
	slots, closeSlots     chan struct{}
	leaseSlots            chan struct{}
	stopping              bool
	ledger                *os.File
	ledgerErr             error
	// Private dependency injection lets unit tests exercise lifecycle races without Docker.
	dockerFn func(context.Context, []byte, ...string) ([]byte, error)
}

const maxRunLedger = 100000

var sessionPattern = regexp.MustCompile(`^(task-[1-9][0-9]{0,18}|session-[a-f0-9]{32}|discovery)$`)

func ValidatePolicy(p Policy) error {
	if len(p.DNS) == 0 || len(p.Management) == 0 {
		return errors.New("explicit DNS and management deny ranges are required")
	}
	for _, ip := range p.DNS {
		addr, err := netip.ParseAddr(ip)
		if err != nil || !addr.Is4() || addr.IsLoopback() || addr.IsUnspecified() {
			return errors.New("DNS servers must be explicit non-loopback IPv4 addresses")
		}
	}
	for _, cidr := range p.Management {
		if _, err := netip.ParsePrefix(cidr); err != nil {
			return err
		}
	}
	for session, ranges := range p.Sessions {
		if !sessionPattern.MatchString(session) || len(ranges) == 0 || len(ranges) > 64 {
			return errors.New("invalid worker session policy")
		}
		for _, cidr := range ranges {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || !prefix.Addr().Is4() || prefix.Bits() == 0 {
				return errors.New("worker targets must be explicit IPv4 CIDRs, not a default route")
			}
		}
	}
	return nil
}
func NewBroker(policy Policy, image, network, token string) (*Broker, error) {
	if err := ValidatePolicy(policy); err != nil {
		return nil, err
	}
	if !regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(image) {
		return nil, errors.New("worker image must be a reviewed immutable local image ID")
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`).MatchString(network) || network == "host" || network == "none" || network == "bridge" {
		return nil, errors.New("worker network must be a dedicated named bridge, never host/container/default modes")
	}
	if len(token) < 32 {
		return nil, errors.New("worker network and a 32-character broker token are required")
	}
	return &Broker{Policy: policy, Image: image, Network: network, Token: token, runs: map[string]*brokerRun{}, closed: map[string]bool{}, known: map[string]bool{}, frozen: map[string]bool{}, slots: make(chan struct{}, 2), closeSlots: make(chan struct{}, 8), leaseSlots: make(chan struct{}, 2), gate: make(chan struct{}, 1), dockerFn: docker}, nil
}
func docker(ctx context.Context, input []byte, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "docker", args...)
	command.Stdin = bytes.NewReader(input)
	var out cappedBuffer
	command.Stdout = &out
	command.Stderr = &out
	command.WaitDelay = 2 * time.Second
	err := command.Run()
	if err != nil {
		return nil, fmt.Errorf("isolated runtime operation failed: %w: %s", err, strings.TrimSpace(out.String()))
	}
	if out.exceeded {
		return nil, errors.New("isolated runtime output exceeded its limit")
	}
	return out.Bytes(), nil
}

type cappedBuffer struct {
	bytes.Buffer
	exceeded bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > MaxMessage {
		b.exceeded = true
		return 0, errors.New("output limit")
	}
	return b.Buffer.Write(p)
}

func decodeRequest(w http.ResponseWriter, r *http.Request, limit int64, req any) error {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(req); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errors.New("trailing request data")
	}
	return nil
}
func (b *Broker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+b.Token)) != 1 {
		http.Error(w, "unauthorized", 401)
		return
	}
	if r.Method != "POST" {
		http.Error(w, "not found", 404)
		return
	}
	if r.URL.Path == "/v1/lease" {
		b.serveLease(w, r)
		return
	}
	if r.URL.Path == "/v1/close" {
		b.serveClose(w, r)
		return
	}
	if r.URL.Path != "/v1/call" {
		http.Error(w, "not found", 404)
		return
	}
	select {
	case b.slots <- struct{}{}:
		defer func() { <-b.slots }()
	default:
		http.Error(w, "worker admission limit", 429)
		return
	}
	var req Request
	if decodeRequest(w, r, MaxMessage, &req) != nil {
		http.Error(w, "invalid worker request", 400)
		return
	}
	lifecycle := req.Tool == "__snapshot" || req.Tool == "__retire" || req.Tool == "__resume"
	if !sessionPattern.MatchString(req.Session) || (!lifecycle && len(b.Policy.Sessions[req.Session]) == 0) {
		http.Error(w, "no operator-approved target policy for this session", 403)
		return
	}
	if (lifecycle && req.RunID != "") || (!lifecycle && !validRunID(req.RunID)) {
		http.Error(w, "invalid worker run identity", 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	select {
	case b.gate <- struct{}{}:
		defer func() { <-b.gate }()
	case <-ctx.Done():
		http.Error(w, "worker admission deadline", 504)
		return
	}
	if lifecycle {
		result, err := b.lifecycle(ctx, req)
		if err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		if err = json.NewEncoder(w).Encode(result); (err != nil || ctx.Err() != nil) && req.Tool == "__snapshot" {
			b.resumeSession(req.Session)
		}
		return
	}
	run, fresh, err := b.admit(req.Session, req.RunID)
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	callCtx, callCancel := context.WithCancel(ctx)
	stop := context.AfterFunc(run.ctx, callCancel)
	defer stop()
	defer callCancel()
	if fresh {
		_, err = b.ensure(callCtx, run)
		close(run.ready)
		if err != nil {
			b.removeRun(run)
			b.closeRun(run.session, run.id)
			http.Error(w, err.Error(), 503)
			return
		}
	}
	b.mu.Lock()
	closed := b.closed[runKeyName(run.session, run.id)]
	b.mu.Unlock()
	if closed || callCtx.Err() != nil {
		b.removeRun(run)
		http.Error(w, "worker run is closed", 409)
		return
	}
	if err = b.renewRun(callCtx, run); err != nil {
		b.closeRun(run.session, run.id)
		http.Error(w, "worker lease renewal failed", 502)
		return
	}
	result, err := b.exec(callCtx, run, req)
	if err != nil {
		b.closeRun(run.session, run.id)
		http.Error(w, err.Error(), 502)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}
func runKeyName(session, id string) string { return session + "/" + id }
func (b *Broker) runName(session, id string) string {
	digest := sha256.Sum256([]byte(session + "/" + id + "/" + b.Token))
	return "artex-worker-" + hex.EncodeToString(digest[:10])
}
func (b *Broker) admit(session, id string) (*brokerRun, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := runKeyName(session, id)
	if b.stopping || b.closed[key] || b.frozen[session] {
		return nil, false, errors.New("worker run is closed or task is frozen")
	}
	if r := b.runs[key]; r != nil {
		return r, false, nil
	}
	if b.ledgerErr != nil || len(b.known) >= maxRunLedger {
		return nil, false, errors.New("worker run ledger unavailable or exhausted; operator maintenance required")
	}
	if len(b.runs) >= 8 {
		return nil, false, errors.New("worker run limit reached")
	}
	if err := b.reserveLocked(key); err != nil {
		return nil, false, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &brokerRun{session: session, id: id, name: b.runName(session, id), ctx: ctx, cancel: cancel, ready: make(chan struct{})}
	b.runs[key] = r
	return r, true, nil
}
func (b *Broker) reserveLocked(key string) error {
	if b.known[key] {
		return nil
	}
	if len(b.known) >= maxRunLedger {
		b.ledgerErr = errors.New("worker run ledger exhausted")
		return b.ledgerErr
	}
	b.known[key] = true
	if b.ledger != nil {
		if _, err := b.ledger.WriteString(key + "\n"); err != nil {
			b.ledgerErr = err
			return err
		}
		if err := b.ledger.Sync(); err != nil {
			b.ledgerErr = err
			return err
		}
	}
	return nil
}
func (b *Broker) serveClose(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	stopping := b.stopping
	b.mu.Unlock()
	if stopping {
		http.Error(w, "worker broker is stopping", 503)
		return
	}
	select {
	case b.closeSlots <- struct{}{}:
		defer func() { <-b.closeSlots }()
	default:
		http.Error(w, "worker control admission limit", 429)
		return
	}
	var req struct {
		Session string `json:"session"`
		RunID   string `json:"run_id"`
	}
	if decodeRequest(w, r, 8192, &req) != nil || !sessionPattern.MatchString(req.Session) || !validRunID(req.RunID) {
		http.Error(w, "invalid close identity", 400)
		return
	}
	if err := b.closeRun(req.Session, req.RunID); err != nil {
		http.Error(w, err.Error(), 503)
		return
	}
	_ = json.NewEncoder(w).Encode(Response{})
}
func (b *Broker) closeRun(session, id string) error {
	b.mu.Lock()
	key := runKeyName(session, id)
	run := b.runs[key]
	if !b.known[key] && len(b.known) >= maxRunLedger {
		b.ledgerErr = errors.New("worker run ledger exhausted")
		err := b.ledgerErr
		b.mu.Unlock()
		return err
	}
	b.closed[key] = true
	if run != nil {
		run.cancel()
	}
	ledgerErr := b.reserveLocked(key)
	b.mu.Unlock()
	if run == nil {
		return ledgerErr
	}
	// Cancel before any Docker operation. The dedicated endpoint never waits for
	// the global scan gate, so foreground and detached processes are both stopped.
	removeErr := b.removeRun(run)
	select {
	case <-run.ready:
	case <-time.After(4 * time.Second):
	}
	finalErr := b.removeRun(run) // catch a Docker create acknowledged after cancellation
	b.mu.Lock()
	delete(b.runs, key)
	b.mu.Unlock()
	return errors.Join(ledgerErr, removeErr, finalErr)
}
func (b *Broker) removeRun(run *brokerRun) error {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	_, err := b.dockerFn(ctx, nil, "rm", "-f", run.name)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "no such container") {
		return nil
	}
	return err
}
func (b *Broker) exec(ctx context.Context, run *brokerRun, req Request) (Response, error) {
	var result Response
	payload, err := json.Marshal(req)
	if err != nil {
		return result, err
	}
	output, err := b.dockerFn(ctx, payload, "exec", "-i", "--user", "0:0", "--workdir", "/", run.name, "/usr/bin/setpriv", "--reuid=10001", "--regid=10001", "--clear-groups", "--bounding-set=-all", "--inh-caps=-all", "--ambient-caps=-all", "--no-new-privs", "/usr/local/bin/tool-worker", "call")
	if err != nil {
		return result, err
	}
	if json.Unmarshal(output, &result) != nil {
		return result, errors.New("invalid isolated runtime response")
	}
	return result, nil
}
func (b *Broker) sessionRuns(session string) []*brokerRun {
	b.mu.Lock()
	defer b.mu.Unlock()
	runs := []*brokerRun{}
	for _, run := range b.runs {
		if run.session == session {
			runs = append(runs, run)
		}
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].id < runs[j].id })
	return runs
}
func (b *Broker) lifecycle(ctx context.Context, req Request) (Response, error) {
	result := Response{}
	b.mu.Lock()
	if req.Tool != "__resume" {
		b.frozen[req.Session] = true
	}
	b.mu.Unlock()
	runs := b.sessionRuns(req.Session)
	switch req.Tool {
	case "__retire":
		var errs []error
		for _, run := range runs {
			errs = append(errs, b.closeRun(run.session, run.id))
		}
		b.mu.Lock()
		delete(b.frozen, req.Session)
		b.mu.Unlock()
		return result, errors.Join(errs...)
	case "__resume":
		b.resumeSession(req.Session)
		return result, nil
	case "__snapshot":
		if len(runs) == 0 {
			b.mu.Lock()
			delete(b.frozen, req.Session)
			b.mu.Unlock()
			return result, nil
		}
		result.Files = map[string][]byte{}
		var used int
		for _, run := range runs {
			snapshot, err := b.exec(ctx, run, Request{Session: run.session, RunID: run.id, Tool: "__snapshot"})
			if err == nil && snapshot.Error != "" {
				err = errors.New(snapshot.Error)
			}
			if err == nil && !snapshot.SnapshotComplete {
				err = errors.New("worker snapshot was incomplete")
			}
			if err == nil {
				_, err = b.dockerFn(ctx, nil, "pause", run.name)
			}
			if err != nil {
				b.resumeSession(req.Session)
				return Response{}, err
			}
			for name, data := range snapshot.Files {
				if prior, exists := result.Files[name]; exists {
					if !bytes.Equal(prior, data) {
						b.resumeSession(req.Session)
						return Response{}, errors.New("sibling worker artifact conflict; preserve each run separately before archive")
					}
					continue
				}
				used += len(data)
				if used > 4<<20 || len(result.Files) >= 1000 {
					b.resumeSession(req.Session)
					return Response{}, errors.New("aggregate worker snapshot exceeds limit")
				}
				result.Files[name] = data
			}
		}
		result.SnapshotComplete = true
		return result, nil
	}
	return result, errors.New("unknown lifecycle request")
}
func (b *Broker) resumeSession(session string) {
	for _, run := range b.sessionRuns(session) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, _ = b.dockerFn(ctx, nil, "unpause", run.name)
		_, _ = b.exec(ctx, run, Request{Session: run.session, RunID: run.id, Tool: "__resume"})
		cancel()
	}
	b.mu.Lock()
	delete(b.frozen, session)
	b.mu.Unlock()
}
func (b *Broker) ensure(ctx context.Context, run *brokerRun) (string, error) {
	// Deny every Docker subnet, including the broker, API, database and host
	// gateway. A broad target allowlist cannot override these earlier deny rules.
	ids, err := b.dockerFn(ctx, nil, "network", "ls", "-q")
	if err != nil {
		return "", err
	}
	args := append([]string{"network", "inspect"}, strings.Fields(string(ids))...)
	networks, err := b.dockerFn(ctx, nil, args...)
	if err != nil {
		return "", err
	}
	var inspected []struct {
		Name   string
		Driver string
		IPAM   struct{ Config []struct{ Subnet string } }
	}
	if err = json.Unmarshal(networks, &inspected); err != nil {
		return "", err
	}
	foundBridge := false
	for _, candidate := range inspected {
		if candidate.Name == b.Network && candidate.Driver == "bridge" {
			foundBridge = true
		}
	}
	if !foundBridge {
		return "", errors.New("worker network is missing or is not a bridge driver")
	}
	deny := append([]string{"0.0.0.0/8", "169.254.0.0/16", "224.0.0.0/4", "240.0.0.0/4"}, b.Policy.Management...)
	for _, network := range inspected {
		for _, entry := range network.IPAM.Config {
			if prefix, e := netip.ParsePrefix(entry.Subnet); e == nil && prefix.Addr().Is4() {
				deny = append(deny, prefix.String())
			}
		}
	}
	name := run.name
	_, _ = b.dockerFn(ctx, nil, "rm", "-f", name)
	args = []string{"create", "--name", name, "--label", "artex.purpose=isolated-worker", "--label", "artex.worker.owner=" + b.owner(), "--network", b.Network, "--no-healthcheck", "--entrypoint", "/usr/local/bin/worker-entrypoint", "--read-only", "--cap-drop", "ALL", "--cap-add", "NET_ADMIN", "--cap-add", "SETUID", "--cap-add", "SETGID", "--cap-add", "SETPCAP", "--security-opt", "no-new-privileges:true", "--pids-limit", "128", "--memory", "1g", "--memory-swap", "1g", "--cpus", "1", "--user", "0:0", "--tmpfs", "/workspace:rw,nosuid,nodev,size=512m,uid=10001,gid=10001", "--tmpfs", "/tmp:rw,nosuid,nodev,size=128m,uid=10001,gid=10001", "-e", "HOME=/workspace", "-e", "WORKER_RUN_ID=" + run.id, "-e", "WORKER_SESSION=" + run.session, "-e", "WORKER_ALLOW=" + strings.Join(b.Policy.Sessions[run.session], ","), "-e", "WORKER_DENY=" + strings.Join(deny, ","), "-e", "WORKER_DNS=" + strings.Join(b.Policy.DNS, ","), b.Image}
	args = args[:len(args)-1]
	for _, resolver := range b.Policy.DNS {
		args = append(args, "--dns", resolver)
	}
	args = append(args, b.Image)
	if _, err = b.dockerFn(ctx, nil, args...); err != nil {
		return "", err
	}
	// Separate creation from start: cancellation cannot leave an asynchronously
	// acknowledged Docker run executing after the close channel removed its name.
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if _, err = b.dockerFn(ctx, nil, "start", name); err != nil {
		return "", err
	}
	return name, nil
}
func (b *Broker) Close() {
	b.mu.Lock()
	b.stopping = true
	runs := []*brokerRun{}
	for _, run := range b.runs {
		runs = append(runs, run)
	}
	b.mu.Unlock()
	for _, run := range runs {
		_ = b.closeRun(run.session, run.id)
	}
	b.mu.Lock()
	if b.ledger != nil {
		_ = b.ledger.Close()
		b.ledger = nil
	}
	b.mu.Unlock()
}

func LoadBroker() (*Broker, error) {
	raw, err := os.ReadFile(os.Getenv("ARTEX_WORKER_POLICY_FILE"))
	if err != nil {
		return nil, err
	}
	var policy Policy
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&policy); err != nil {
		return nil, err
	}
	token, err := os.ReadFile(os.Getenv("ARTEX_WORKER_TOKEN_FILE"))
	if err != nil {
		return nil, err
	}
	broker, err := NewBroker(policy, os.Getenv("ARTEX_WORKER_IMAGE"), os.Getenv("ARTEX_WORKER_NETWORK"), strings.TrimSpace(string(token)))
	if err != nil {
		return nil, err
	}
	if err = broker.openLedger(os.Getenv("ARTEX_WORKER_STATE_DIR")); err != nil {
		return nil, err
	}
	// Remove only this deployment's orphan workers after an ungraceful restart.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	success := false
	defer func() {
		if !success {
			broker.Close()
		}
	}()
	ids, err := broker.dockerFn(ctx, nil, "ps", "-aq", "--filter", "label=artex.worker.owner="+broker.owner())
	if err != nil {
		return nil, err
	}
	if names := strings.Fields(string(ids)); len(names) > 0 {
		if _, err = broker.dockerFn(ctx, nil, append([]string{"rm", "-f"}, names...)...); err != nil {
			return nil, err
		}
	}
	success = true
	return broker, nil
}

func (b *Broker) owner() string {
	digest := sha256.Sum256([]byte(b.Token))
	return hex.EncodeToString(digest[:8])
}

func (b *Broker) renewRun(ctx context.Context, run *brokerRun) error {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	_, err := b.dockerFn(ctx, nil, "exec", "--user", "0:0", "--workdir", "/", run.name, "/usr/bin/setpriv", "--bounding-set=-all", "--inh-caps=-all", "--ambient-caps=-all", "--no-new-privs", "/usr/local/bin/tool-worker", "renew")
	return err
}
func (b *Broker) serveLease(w http.ResponseWriter, r *http.Request) {
	select {
	case b.leaseSlots <- struct{}{}:
		defer func() { <-b.leaseSlots }()
	default:
		http.Error(w, "worker lease admission limit", 429)
		return
	}
	var req struct {
		Session string `json:"session"`
		RunID   string `json:"run_id"`
	}
	if decodeRequest(w, r, 8192, &req) != nil || !sessionPattern.MatchString(req.Session) || !validRunID(req.RunID) {
		http.Error(w, "invalid lease identity", 400)
		return
	}
	b.mu.Lock()
	run := b.runs[runKeyName(req.Session, req.RunID)]
	skip := b.closed[runKeyName(req.Session, req.RunID)] || b.frozen[req.Session] || b.stopping
	b.mu.Unlock()
	if run == nil || skip {
		_ = json.NewEncoder(w).Encode(Response{})
		return
	}
	select {
	case <-run.ready:
	default:
		_ = json.NewEncoder(w).Encode(Response{})
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stop := context.AfterFunc(run.ctx, cancel)
	defer stop()
	if err := b.renewRun(ctx, run); err != nil {
		http.Error(w, "worker lease renewal failed", 503)
		return
	}
	_ = json.NewEncoder(w).Encode(Response{})
}
