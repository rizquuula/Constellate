package audit

import (
	"context"
	"time"

	"github.com/rizquuula/Constellate/internal/hub/domain/audit"
)

// AuditStore is the persistence port for audit records.
type AuditStore interface {
	Append(ctx context.Context, e audit.Event) error
}

// Clock returns the current unix-second timestamp.
type Clock interface {
	Now() int64
}

// SystemClock implements Clock using the real wall clock.
type SystemClock struct{}

func (SystemClock) Now() int64 { return time.Now().Unix() }
