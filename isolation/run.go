package isolation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"regexp"
	"sync"
	"time"
)

var runPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

type runKey struct{}
type runIdentity struct {
	id          string
	mu          sync.Mutex
	scopes      map[string]bool
	closed      bool
	once        sync.Once
	stop        func() bool
	renewCancel context.CancelFunc
	err         error
}

// NewRunID is a trusted orchestration identity, never derived from model input.
func NewRunID() string {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		panic("secure worker run identity unavailable")
	}
	return hex.EncodeToString(entropy[:])
}
func identityFrom(ctx context.Context) *runIdentity {
	identity, _ := ctx.Value(runKey{}).(*runIdentity)
	return identity
}
func WithRunID(ctx context.Context, id string) context.Context {
	if prior := identityFrom(ctx); prior != nil && prior.id == id {
		return ctx
	}
	identity := &runIdentity{id: id, scopes: map[string]bool{}}
	ctx = context.WithValue(ctx, runKey{}, identity)
	identity.stop = context.AfterFunc(ctx, func() {
		if err := identity.close(); err != nil {
			log.Printf("isolated cancelled-run cleanup failed: %v", err)
		}
	})
	return ctx
}

// WithRunContext preserves cancellation/deadlines of ctx while sharing the trusted
// execution identity and scope registry already assigned by orchestration.
func WithRunContext(ctx, source context.Context) context.Context {
	if identity := identityFrom(source); identity != nil {
		return context.WithValue(ctx, runKey{}, identity)
	}
	return ctx
}
func RunIDFrom(ctx context.Context) string {
	if identity := identityFrom(ctx); identity != nil {
		return identity.id
	}
	return ""
}
func validRunID(id string) bool { return runPattern.MatchString(id) }
func RegisterRunScope(ctx context.Context, session string) error {
	identity := identityFrom(ctx)
	if identity == nil || !validRunID(identity.id) || !sessionPattern.MatchString(session) {
		return errors.New("invalid trusted worker run scope")
	}
	identity.mu.Lock()
	defer identity.mu.Unlock()
	if identity.closed {
		return errors.New("worker run is closed")
	}
	if len(identity.scopes) == 0 && Configured() {
		renewCtx, cancel := context.WithCancel(context.Background())
		identity.renewCancel = cancel
		go identity.renew(renewCtx)
	}
	identity.scopes[session] = true
	return nil
}

// CloseRunContext stops every workspace/discovery container belonging to a single
// run. Callers with a borrowed MCP identity must leave this to session lifecycle.
func CloseRunContext(ctx context.Context) error {
	identity := identityFrom(ctx)
	if identity == nil {
		return nil
	}
	if identity.stop != nil {
		identity.stop()
	}
	return identity.close()
}
func (identity *runIdentity) close() error {
	identity.once.Do(func() {
		identity.mu.Lock()
		identity.closed = true
		if identity.renewCancel != nil {
			identity.renewCancel()
		}
		scopes := make([]string, 0, len(identity.scopes))
		for scope := range identity.scopes {
			scopes = append(scopes, scope)
		}
		identity.mu.Unlock()
		var wg sync.WaitGroup
		var mu sync.Mutex
		var errs []error
		for _, scope := range scopes {
			wg.Add(1)
			go func(scope string) {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				err := CloseRun(ctx, scope, identity.id)
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}(scope)
		}
		wg.Wait()
		identity.err = errors.Join(errs...)
	})
	return identity.err
}

func (identity *runIdentity) renew(ctx context.Context) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			identity.mu.Lock()
			scopes := make([]string, 0, len(identity.scopes))
			for scope := range identity.scopes {
				scopes = append(scopes, scope)
			}
			identity.mu.Unlock()
			for _, scope := range scopes {
				if err := LeaseRun(ctx, scope, identity.id); err != nil && ctx.Err() == nil {
					log.Printf("isolated worker lease renewal failed: %v", err)
				}
			}
		}
	}
}
