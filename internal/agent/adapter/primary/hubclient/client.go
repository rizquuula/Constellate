package hubclient

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"
	"github.com/rizquuula/Constellate/internal/agent/app/session"
	"github.com/rizquuula/Constellate/internal/agent/domain/terminal"
	"github.com/rizquuula/Constellate/internal/transport"
)

// SessionManager is the consumer-side interface the client uses to manage PTY
// sessions. *session.Manager satisfies this interface structurally.
type SessionManager interface {
	Open(sessionID string, spec session.PTYSpec) (pid int, err error)
	Attach(sessionID string, stream io.ReadWriteCloser, in io.Reader) error
	Resize(sessionID string, cols, rows int) error
	Close(sessionID string) error
	// Activities returns per-session activity signals at unix-second timestamp now.
	Activities(now int64) []terminal.SessionActivity
}

// MetricsSampler is the consumer-side interface for sampling host metrics.
// sysmetrics.Collector satisfies this structurally.
type MetricsSampler interface {
	Sample() (transport.Metrics, bool)
}

// errNotConnected is returned by sendControl when no encoder is set.
var errNotConnected = errors.New("hubclient: not connected")

// stubSessions is used when Config.Sessions is nil to avoid panics.
type stubSessions struct{}

func (stubSessions) Open(_ string, _ session.PTYSpec) (int, error) {
	return 0, errors.New("hubclient: no session manager configured")
}
func (stubSessions) Attach(_ string, _ io.ReadWriteCloser, _ io.Reader) error {
	return errors.New("hubclient: no session manager configured")
}
func (stubSessions) Resize(_ string, _, _ int) error {
	return errors.New("hubclient: no session manager configured")
}
func (stubSessions) Close(_ string) error {
	return errors.New("hubclient: no session manager configured")
}
func (stubSessions) Activities(_ int64) []terminal.SessionActivity { return nil }

// Config holds all parameters needed to create a Client.
type Config struct {
	HubURL            string
	AgentKey          ed25519.PrivateKey
	MachineID         string
	InstanceID        string
	Name              string
	HeartbeatInterval time.Duration
	HTTPClient        *http.Client
	Log               *slog.Logger
	Sessions          SessionManager
	Metrics           MetricsSampler
}

// snapshotToggle is a consumer-side interface for enabling/disabling the
// snapshot producer. *snapshot.Producer satisfies this structurally.
type snapshotToggle interface {
	SetEnabled(bool)
}

// Client manages a persistent, auto-reconnecting connection to the hub.
type Client struct {
	hubURL            string
	agentKey          ed25519.PrivateKey
	httpClient        *http.Client
	machineID         string
	instanceID        string
	name              string
	heartbeatInterval time.Duration
	log               *slog.Logger
	sessions          SessionManager
	metrics           MetricsSampler

	mu      sync.Mutex
	ctrlEnc *transport.Encoder

	// yamux session for the current connection; nil when disconnected.
	// Used by SendSnapshot to lazily open the snapshot stream.
	muxSess *yamux.Session

	// snapStream / snapEnc are the lazily-opened snapshot stream for the
	// current connection. Guarded by mu; cleared on disconnect.
	snapStream net.Conn
	snapEnc    *transport.Encoder

	// toggle is the snapshot producer; may be nil when not wired.
	toggle snapshotToggle
}

// New creates a Client from cfg. Zero HeartbeatInterval defaults to 5s; nil
// Log defaults to a discard logger; nil Sessions defaults to a stub that
// returns errors (graceful degradation rather than panic).
func New(cfg Config) *Client {
	if cfg.HeartbeatInterval == 0 {
		cfg.HeartbeatInterval = 5 * time.Second
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if cfg.Sessions == nil {
		cfg.Sessions = stubSessions{}
	}
	return &Client{
		hubURL:            cfg.HubURL,
		agentKey:          cfg.AgentKey,
		httpClient:        cfg.HTTPClient,
		machineID:         cfg.MachineID,
		instanceID:        cfg.InstanceID,
		name:              cfg.Name,
		heartbeatInterval: cfg.HeartbeatInterval,
		log:               cfg.Log,
		sessions:          cfg.Sessions,
		metrics:           cfg.Metrics,
	}
}

// setCtrlEnc stores the current control encoder under the mutex.
func (c *Client) setCtrlEnc(enc *transport.Encoder) {
	c.mu.Lock()
	c.ctrlEnc = enc
	c.mu.Unlock()
}

// clearCtrlEnc clears the current control encoder under the mutex.
func (c *Client) clearCtrlEnc() {
	c.mu.Lock()
	c.ctrlEnc = nil
	c.mu.Unlock()
}

// sendControl encodes msg on the current control encoder. Returns
// errNotConnected when not connected.
func (c *Client) sendControl(msg any) error {
	c.mu.Lock()
	enc := c.ctrlEnc
	c.mu.Unlock()
	if enc == nil {
		return errNotConnected
	}
	return enc.Encode(msg)
}

// SessionExited implements session.Notifier. It sends a SessionExited frame to
// the hub. If the client is not connected, it logs at debug level — the hub
// will reconcile on reconnect.
func (c *Client) SessionExited(sessionID string, exitCode int) {
	if err := c.sendControl(transport.NewSessionExited(sessionID, exitCode)); err != nil {
		if errors.Is(err, errNotConnected) {
			c.log.Debug("SessionExited: not connected, hub will reconcile on reconnect",
				"sessionID", sessionID, "exitCode", exitCode)
			return
		}
		c.log.Warn("SessionExited: send failed", "sessionID", sessionID, "err", err)
	}
}

// SetSnapshotToggle wires the snapshot producer so that EnableSnaps messages
// from the hub can start/stop it.
func (c *Client) SetSnapshotToggle(t snapshotToggle) {
	c.mu.Lock()
	c.toggle = t
	c.mu.Unlock()
}

// SendSnapshot encodes one session's screen and writes it to the snapshot
// stream. It lazily opens the stream on the first call per connection.
// If the client is not connected, the snapshot is silently dropped (the
// producer will retry on the next tick once reconnected).
// Called from a single goroutine (the producer ticker), but the stream/session
// fields are guarded by mu because connectOnce/serve mutate them.
// Network I/O (OpenStream + header write) happens WITHOUT holding mu to avoid
// contending with reconnect teardown.
func (c *Client) SendSnapshot(s terminal.SessionScreen) error {
	// Read sess and current encoder under mu, then release.
	c.mu.Lock()
	sess := c.muxSess
	enc := c.snapEnc
	c.mu.Unlock()

	if sess == nil {
		return nil // not connected; drop silently
	}

	// If we don't have an encoder yet, open the stream without holding mu.
	if enc == nil {
		stream, err := sess.OpenStream()
		if err != nil {
			return fmt.Errorf("hubclient: open snapshot stream: %w", err)
		}
		newEnc := transport.NewEncoder(stream)
		if err := newEnc.Encode(transport.NewSnapStreamHeader()); err != nil {
			_ = stream.Close()
			return fmt.Errorf("hubclient: write snap stream header: %w", err)
		}
		// Re-acquire mu and store only if the session hasn't been replaced by a reconnect.
		c.mu.Lock()
		if c.muxSess != sess {
			// A reconnect happened while we were opening — discard our stream.
			c.mu.Unlock()
			_ = stream.Close()
			return nil
		}
		if c.snapEnc != nil {
			// Another goroutine (shouldn't happen — single producer) beat us; discard ours.
			c.mu.Unlock()
			_ = stream.Close()
			enc = c.snapEnc
		} else {
			c.snapStream = stream
			c.snapEnc = newEnc
			enc = newEnc
			c.mu.Unlock()
		}
	}

	snap := encodeSnapshot(s, c.machineID)
	if err := enc.Encode(snap); err != nil {
		c.mu.Lock()
		c.clearSnapStream()
		c.mu.Unlock()
		return fmt.Errorf("hubclient: write snapshot: %w", err)
	}
	return nil
}

// clearSnapStream closes and nils the snapshot stream fields. Must be called
// with c.mu held.
func (c *Client) clearSnapStream() {
	if c.snapStream != nil {
		_ = c.snapStream.Close()
		c.snapStream = nil
		c.snapEnc = nil
	}
}

// SendRawSnapshot relays a pre-encoded transport.Snapshot from the host to the
// hub snapshot stream, stamping the machineID before sending. This is called by
// hostclient when relaying snapshots from the session-host, avoiding a
// double-encode round-trip through terminal.SessionScreen.
func (c *Client) SendRawSnapshot(s transport.Snapshot) error {
	c.mu.Lock()
	sess := c.muxSess
	enc := c.snapEnc
	c.mu.Unlock()

	if sess == nil {
		return nil // not connected; drop silently
	}

	if enc == nil {
		stream, err := sess.OpenStream()
		if err != nil {
			return fmt.Errorf("hubclient: open snapshot stream: %w", err)
		}
		newEnc := transport.NewEncoder(stream)
		if err := newEnc.Encode(transport.NewSnapStreamHeader()); err != nil {
			_ = stream.Close()
			return fmt.Errorf("hubclient: write snap stream header: %w", err)
		}
		c.mu.Lock()
		if c.muxSess != sess {
			c.mu.Unlock()
			_ = stream.Close()
			return nil
		}
		if c.snapEnc != nil {
			c.mu.Unlock()
			_ = stream.Close()
			enc = c.snapEnc
		} else {
			c.snapStream = stream
			c.snapEnc = newEnc
			enc = newEnc
			c.mu.Unlock()
		}
	}

	s.MachineID = c.machineID
	if err := enc.Encode(s); err != nil {
		c.mu.Lock()
		c.clearSnapStream()
		c.mu.Unlock()
		return fmt.Errorf("hubclient: write raw snapshot: %w", err)
	}
	return nil
}

const (
	backoffInitial = 500 * time.Millisecond
	backoffFactor  = 2.0
	backoffCap     = 30 * time.Second
	backoffJitter  = 0.2
)

// Run enters the reconnect loop. It runs until ctx is canceled, returning
// ctx.Err() on clean shutdown.
func (c *Client) Run(ctx context.Context) error {
	backoff := backoffInitial

	for {
		connected, err := c.connectOnce(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if connected {
			// Successfully established — reset backoff.
			backoff = backoffInitial
		}

		if err != nil {
			// Apply jitter: ±20%.
			jitter := 1 + (rand.Float64()*2-1)*backoffJitter
			wait := time.Duration(float64(backoff) * jitter)
			c.log.Warn("disconnected, retrying",
				"machineID", c.machineID,
				"err", err,
				"next_backoff", wait.Round(time.Millisecond),
			)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}

		// Advance backoff for the next failure (capped).
		next := time.Duration(float64(backoff) * backoffFactor)
		if next > backoffCap {
			next = backoffCap
		}
		// Only advance backoff if we didn't just connect successfully (which
		// already reset it). If we just reset, backoff == backoffInitial so the
		// advance applies correctly on the next iteration.
		if !connected {
			backoff = next
		}
	}
}

// connectOnce dials the hub, completes the handshake, then drives the
// heartbeat/read/accept loops until the connection breaks or ctx is canceled.
// connected is true when Hello was sent successfully.
func (c *Client) connectOnce(ctx context.Context) (connected bool, err error) {
	if c.agentKey == nil {
		return false, fmt.Errorf("hubclient: no agent key — not enrolled")
	}
	bearerToken := transport.BuildAgentToken(c.machineID, c.agentKey, time.Now().Unix())

	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	conn, _, err := websocket.Dial(dialCtx, c.hubURL, &websocket.DialOptions{
		HTTPClient: c.httpClient,
		HTTPHeader: http.Header{
			"Authorization": {"Bearer " + bearerToken},
		},
	})
	cancel()
	if err != nil {
		return false, err
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	connCtx, connCancel := context.WithCancel(ctx)
	defer connCancel()

	netConn := websocket.NetConn(connCtx, conn, websocket.MessageBinary)

	sess, err := transport.Client(netConn)
	if err != nil {
		return false, err
	}
	defer func() { _ = sess.Close() }()

	ctrl, err := sess.OpenStream()
	if err != nil {
		return false, err
	}

	// Create the single encoder for the control stream; reused by serve.
	ctrlEnc := transport.NewEncoder(ctrl)
	if err := sendHello(ctrlEnc, c.machineID, c.instanceID, c.name); err != nil {
		return false, err
	}

	// Store the yamux session so SendSnapshot can open streams on it.
	c.mu.Lock()
	c.muxSess = sess
	c.mu.Unlock()

	// Clear session and snapshot stream when this connection ends.
	defer func() {
		c.mu.Lock()
		c.muxSess = nil
		c.clearSnapStream()
		c.mu.Unlock()
	}()

	// From here the handshake succeeded.
	c.log.Info("connected to hub", "machineID", c.machineID, "hub", c.hubURL)

	return true, c.serve(connCtx, sess, ctrl, ctrlEnc)
}

// serve drives the heartbeat, control-read, and accept-stream loops after the
// handshake is complete. enc is the already-constructed encoder for ctrl and
// must be the only encoder writing to ctrl. It returns the first error from any loop.
func (c *Client) serve(ctx context.Context, sess *yamux.Session, ctrl net.Conn, enc *transport.Encoder) error {
	c.setCtrlEnc(enc)
	defer c.clearCtrlEnc()

	errc := make(chan error, 3)

	// Heartbeat goroutine.
	go func() {
		ticker := time.NewTicker(c.heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				errc <- ctx.Err()
				return
			case <-ticker.C:
				now := time.Now().Unix()
				activities := c.sessions.Activities(now)
				var stats []transport.SessionStat
				if len(activities) > 0 {
					stats = make([]transport.SessionStat, len(activities))
					for i, a := range activities {
						stats[i] = transport.SessionStat{
							ID:       a.ID,
							Activity: string(a.Activity),
						}
					}
				}
				var m *transport.Metrics
				if c.metrics != nil {
					if sampled, ok := c.metrics.Sample(); ok {
						m = &sampled
					}
				}
				if err := enc.Encode(transport.NewHeartbeat(now, stats, m)); err != nil {
					errc <- err
					return
				}
			}
		}
	}()

	// Control-read goroutine.
	go func() {
		dec := transport.NewDecoder(ctrl)
		for {
			frame, err := dec.Next()
			if err != nil {
				errc <- err
				return
			}
			c.handleControlFrame(enc, frame)
		}
	}()

	// Accept-stream goroutine (hub-opened data streams).
	go func() {
		for {
			stream, err := sess.AcceptStream()
			if err != nil {
				errc <- err
				return
			}
			go c.handleDataStream(stream)
		}
	}()

	err := <-errc
	// Close ctrl and sess so the other goroutines unblock and exit cleanly.
	_ = ctrl.Close()
	_ = sess.Close()
	return err
}
