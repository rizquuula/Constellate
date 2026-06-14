package id

import (
	crand "crypto/rand"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

var (
	mu      sync.Mutex
	entropy = ulid.Monotonic(crand.Reader, 0)
)

// New returns a new unique ULID string. Safe for concurrent use.
func New() string {
	mu.Lock()
	defer mu.Unlock()
	return ulid.MustNew(ulid.Timestamp(time.Now()), entropy).String()
}
