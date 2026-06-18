// Package hostclient implements the connect-side secondary adapter that dials
// the session-host over a Unix domain socket. It implements hubclient.SessionManager
// by forwarding Open/Resize/Close to the host and opening local data streams for
// Attach. The InstanceID() method exposes the host's durable instanceID.
package hostclient

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"

	"github.com/hashicorp/yamux"
	"github.com/rizquuula/Constellate/internal/agent/app/session"
	"github.com/rizquuula/Constellate/internal/agent/domain/terminal"
	"github.com/rizquuula/Constellate/internal/transport"
)

// SnapshotSink accepts rendered session screens for forwarding to the hub.
// *hubclient.Client satisfies this structurally.
type SnapshotSink interface {
	SendSnapshot(s terminal.SessionScreen) error
}

// openReply carries the host's response to an OpenSession request.
type openReply struct {
	pid int
	err error
}

// Client dials the session-host UDS, performs the local handshake, and
// implements hubclient.SessionManager so hubclient can use it transparently.
//
// All control-stream reads are serialized through RunControlReader; Open
// registers a pending-reply channel before sending its request so that
// RunControlReader can deliver the reply without a race.
type Client struct {
	log           *slog.Logger
	instanceID    string
	localProtocol int // negotiated local protocol version (from HostInfo)
	hostSess      *yamux.Session
	ctrlEnc       *transport.Encoder

	// mu guards ctrlEnc, hostSess, pendingOpen, pendingOpenSessionID, notifier,
	// shuttingDown, snapSink, and latestActivities.
	mu                   sync.Mutex
	notifier             exitNotifier
	pendingOpen          chan openReply // non-nil while an Open call is in flight
	pendingOpenSessionID string        // sessionID of the in-flight Open; used to match Error frames
	shuttingDown         bool          // set by Shutdown before closing the session

	// snapSink receives Snapshots from the host snapshot stream and forwards
	// them to the hub. Set via SetSnapshotSink.
	snapSink SnapshotSink

	// latestActivities holds the most-recently received LocalStat activities
	// from the host. Updated by dispatch; read by Activities().
	latestActivities []terminal.SessionActivity

	// lost is closed by runControlReader when the host connection drops
	// unexpectedly (not a clean Shutdown). Connect selects on Lost() to detect
	// host death and restart itself.
	lost     chan struct{}
	lostOnce sync.Once
}

// exitNotifier is the consumer interface hubclient satisfies when it wants to
// receive session exit events forwarded from the host.
type exitNotifier interface {
	SessionExited(sessionID string, exitCode int)
}

// noopExitNotifier is used before SetNotifier is called.
type noopExitNotifier struct{}

func (noopExitNotifier) SessionExited(_ string, _ int) {}

// Dial connects to the host UDS at socketPath, performs the handshake, and
// returns a ready Client. The caller must call Shutdown when done.
func Dial(socketPath string, log *slog.Logger) (*Client, error) {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("hostclient: dial %q: %w", socketPath, err)
	}
	return handshake(conn, log)
}

// DialNetwork connects to the host over an arbitrary network+address (useful
// for tests that use TCP listeners instead of a real UDS).
func DialNetwork(network, addr string, log *slog.Logger) (*Client, error) {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	conn, err := net.Dial(network, addr)
	if err != nil {
		return nil, fmt.Errorf("hostclient: dial %s %q: %w", network, addr, err)
	}
	return handshake(conn, log)
}

// handshake wraps conn in yamux, opens the control stream, and performs the
// HostHello/HostInfo exchange. Used by both Dial and DialNetwork.
func handshake(conn net.Conn, log *slog.Logger) (*Client, error) {
	// connect is the yamux *client* (opens streams); host is the server (accepts).
	sess, err := transport.Client(conn)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("hostclient: yamux client: %w", err)
	}

	// Open the control stream — host will AcceptStream() this.
	ctrl, err := sess.OpenStream()
	if err != nil {
		_ = sess.Close()
		return nil, fmt.Errorf("hostclient: open control stream: %w", err)
	}

	enc := transport.NewEncoder(ctrl)
	dec := transport.NewDecoder(ctrl)

	// Send HostHello.
	if err := enc.Encode(transport.NewHostHello(transport.LocalProtocolVersion)); err != nil {
		_ = sess.Close()
		return nil, fmt.Errorf("hostclient: send HostHello: %w", err)
	}

	// Read HostInfo reply. This is the only read that happens outside
	// RunControlReader — it is safe because RunControlReader hasn't started yet.
	frame, err := dec.Next()
	if err != nil {
		_ = sess.Close()
		return nil, fmt.Errorf("hostclient: read HostInfo: %w", err)
	}
	if frame.Type != transport.TypeHostInfo {
		_ = sess.Close()
		return nil, fmt.Errorf("hostclient: expected HostInfo, got %q", frame.Type)
	}
	info, err := transport.Unmarshal[transport.HostInfo](frame)
	if err != nil {
		_ = sess.Close()
		return nil, fmt.Errorf("hostclient: decode HostInfo: %w", err)
	}

	log.Info("hostclient: connected to host",
		"instanceID", info.InstanceID,
		"sessions", len(info.Sessions),
		"proto", info.LocalProtocol)

	c := &Client{
		log:           log,
		instanceID:    info.InstanceID,
		localProtocol: info.LocalProtocol,
		hostSess:      sess,
		ctrlEnc:       enc,
		notifier:      noopExitNotifier{},
		lost:          make(chan struct{}),
	}

	// Start the control reader goroutine. It is the sole reader of dec after
	// handshake, so there are no decoder races.
	go c.runControlReader(dec)

	// v2+ only: start the accept-stream goroutine. The host opens snapshot streams
	// on this yamux session; we relay them to the hub sink. A v1 host does not
	// open snapshot streams.
	if info.LocalProtocol >= 2 {
		go c.runAcceptStreams(sess)
	}

	return c, nil
}

// InstanceID returns the durable instanceID generated by the host at startup.
// connect sources this and passes it to hubclient.Config.InstanceID so the hub
// sees a stable identity across connect restarts.
func (c *Client) InstanceID() string {
	return c.instanceID
}

// SetNotifier wires an exit notifier. hubclient calls this to receive
// SessionExited events forwarded from the host control stream.
func (c *Client) SetNotifier(n exitNotifier) {
	c.mu.Lock()
	c.notifier = n
	c.mu.Unlock()
}

// runControlReader is the internal goroutine that reads all frames from dec.
// It is the only goroutine that reads dec after handshake completes.
// On unexpected disconnect (not a clean Shutdown) it closes the lost channel
// so the caller can detect host death.
func (c *Client) runControlReader(dec *transport.Decoder) {
	for {
		frame, err := dec.Next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				c.log.Debug("hostclient: control read error", "err", err)
			}
			// If an Open is pending, unblock it with an error.
			c.mu.Lock()
			ch := c.pendingOpen
			c.pendingOpen = nil
			c.pendingOpenSessionID = ""
			shutting := c.shuttingDown
			c.mu.Unlock()
			if ch != nil {
				ch <- openReply{err: fmt.Errorf("hostclient: control stream closed: %w", err)}
			}
			// Signal unexpected host loss to connect. Skip on clean Shutdown so
			// a Ctrl-C / context-cancel does not trigger a false restart.
			if !shutting {
				c.lostOnce.Do(func() { close(c.lost) })
			}
			return
		}
		c.dispatch(frame)
	}
}

// dispatch routes a frame to the pending Open waiter or to the general handler.
//
// Error routing: a TypeError is delivered to the pending-Open channel ONLY when
// the frame's SessionID matches the in-flight Open's sessionID. A non-matching
// TypeError falls through to the general handler so that async host errors
// arriving during an in-flight Open are not mis-delivered to the Open waiter.
func (c *Client) dispatch(frame transport.Frame) {
	switch frame.Type {
	case transport.TypeSessionOpened:
		// Always an Open reply.
		c.mu.Lock()
		ch := c.pendingOpen
		c.pendingOpen = nil
		c.pendingOpenSessionID = ""
		c.mu.Unlock()

		if ch != nil {
			msg, err := transport.Unmarshal[transport.SessionOpened](frame)
			if err != nil {
				ch <- openReply{err: fmt.Errorf("hostclient: decode SessionOpened: %w", err)}
				return
			}
			ch <- openReply{pid: msg.PID}
			return
		}
		c.log.Warn("hostclient: received SessionOpened with no pending Open")
		return

	case transport.TypeError:
		// Decode first so we can inspect the SessionID.
		msg, err := transport.Unmarshal[transport.Error](frame)
		if err != nil {
			c.log.Warn("hostclient: decode Error frame failed", "err", err)
			return
		}

		// Route to the pending-Open channel only when sessionIDs match.
		c.mu.Lock()
		ch := c.pendingOpen
		pendingID := c.pendingOpenSessionID
		if ch != nil && msg.SessionID == pendingID {
			c.pendingOpen = nil
			c.pendingOpenSessionID = ""
		} else {
			ch = nil // fall through to general handler
		}
		c.mu.Unlock()

		if ch != nil {
			if msg.Code == "cwd_not_found" {
				ch <- openReply{err: session.ErrCwdNotFound}
				return
			}
			ch <- openReply{err: fmt.Errorf("hostclient: host open error: %s", msg.Message)}
			return
		}

		// General handler: log async/non-matching errors.
		c.log.Warn("hostclient: host error", "code", msg.Code, "message", msg.Message,
			"sessionID", msg.SessionID)
		return

	case transport.TypeSessionExited:
		msg, err := transport.Unmarshal[transport.SessionExited](frame)
		if err != nil {
			c.log.Warn("hostclient: decode SessionExited failed", "err", err)
			return
		}
		c.mu.Lock()
		n := c.notifier
		c.mu.Unlock()
		n.SessionExited(msg.SessionID, msg.ExitCode)

	case transport.TypeHostInfo:
		// ListSessions response; ignored in Phase 1 (no resync caller yet).
		c.log.Debug("hostclient: received HostInfo (ListSessions reply), ignoring")

	case transport.TypeLocalStat:
		msg, err := transport.Unmarshal[transport.LocalStat](frame)
		if err != nil {
			c.log.Warn("hostclient: decode LocalStat failed", "err", err)
			return
		}
		acts := make([]terminal.SessionActivity, len(msg.Activities))
		for i, a := range msg.Activities {
			acts[i] = terminal.SessionActivity{
				ID:       a.ID,
				Activity: terminal.Activity(a.Activity),
			}
		}
		c.mu.Lock()
		c.latestActivities = acts
		c.mu.Unlock()

	default:
		c.log.Debug("hostclient: unknown frame from host, ignoring", "type", frame.Type)
	}
}

// sendControl encodes msg on the control stream. Thread-safe.
func (c *Client) sendControl(msg any) error {
	c.mu.Lock()
	enc := c.ctrlEnc
	c.mu.Unlock()
	if enc == nil {
		return fmt.Errorf("hostclient: not connected")
	}
	return enc.Encode(msg)
}

// Open implements hubclient.SessionManager. It registers a pending-reply
// channel, sends OpenSession to the host, then waits for the reply delivered
// by runControlReader. Only one Open may be in flight at a time (hub control
// frames are processed serially by hubclient).
func (c *Client) Open(sessionID string, spec session.PTYSpec) (int, error) {
	ch := make(chan openReply, 1)

	c.mu.Lock()
	if c.pendingOpen != nil {
		c.mu.Unlock()
		return 0, fmt.Errorf("hostclient: Open already in flight")
	}
	c.pendingOpen = ch
	c.pendingOpenSessionID = sessionID
	c.mu.Unlock()

	if err := c.sendControl(transport.NewOpenSession(
		sessionID, spec.Cwd, spec.Shell, spec.Cols, spec.Rows, spec.CreateDir,
	)); err != nil {
		c.mu.Lock()
		c.pendingOpen = nil
		c.pendingOpenSessionID = ""
		c.mu.Unlock()
		return 0, fmt.Errorf("hostclient: send OpenSession: %w", err)
	}

	reply := <-ch
	return reply.pid, reply.err
}

// Attach implements hubclient.SessionManager. It opens a new yamux data stream
// to the host, writes the AttachHeader, and then bi-directionally copies bytes.
// This mirrors how the hub opens data streams to the agent (AttachHeader+raw).
func (c *Client) Attach(sessionID string, stream io.ReadWriteCloser, in io.Reader) error {
	c.mu.Lock()
	sess := c.hostSess
	c.mu.Unlock()
	if sess == nil {
		return fmt.Errorf("hostclient: not connected")
	}

	dataStream, err := sess.OpenStream()
	if err != nil {
		return fmt.Errorf("hostclient: open data stream: %w", err)
	}
	defer func() { _ = dataStream.Close() }()

	// Write attach header.
	enc := transport.NewEncoder(dataStream)
	if err := enc.Encode(transport.NewAttachHeader(sessionID)); err != nil {
		return fmt.Errorf("hostclient: write attach header: %w", err)
	}

	// Bidirectional copy: in→host and host→stream.
	errc := make(chan error, 2)
	go func() {
		_, err := io.Copy(dataStream, in)
		errc <- err
	}()
	go func() {
		_, err := io.Copy(stream, dataStream)
		errc <- err
	}()

	err = <-errc
	_ = dataStream.Close()
	// Drain second goroutine.
	<-errc
	return err
}

// Resize implements hubclient.SessionManager.
func (c *Client) Resize(sessionID string, cols, rows int) error {
	if err := c.sendControl(transport.NewResize(sessionID, cols, rows)); err != nil {
		return fmt.Errorf("hostclient: send Resize: %w", err)
	}
	return nil
}

// Close implements hubclient.SessionManager. It forwards a CloseSession
// command to the host.
func (c *Client) Close(sessionID string) error {
	if err := c.sendControl(transport.NewCloseSession(sessionID)); err != nil {
		return fmt.Errorf("hostclient: send CloseSession: %w", err)
	}
	return nil
}

// Activities implements hubclient.SessionManager. It returns the latest
// per-session activity signals received from the host via LocalStat frames.
// The now parameter is unused here (the host already computed activities at
// the time it sent the frame); it is kept for interface compatibility.
func (c *Client) Activities(_ int64) []terminal.SessionActivity {
	c.mu.Lock()
	acts := c.latestActivities
	c.mu.Unlock()
	return acts
}

// SetEnabled implements the snapshotToggle interface consumed by hubclient.
// It forwards the enable/disable flag to the host via the control stream so
// the host's snapshot producer starts or stops.
func (c *Client) SetEnabled(enabled bool) {
	if err := c.sendControl(transport.NewEnableSnaps(enabled)); err != nil {
		c.log.Warn("hostclient: forward EnableSnaps to host failed", "enabled", enabled, "err", err)
	}
}

// SetSnapshotSink wires the hub-side sink that receives Snapshot frames
// relayed from the host. hubclient.Client satisfies this.
func (c *Client) SetSnapshotSink(sink SnapshotSink) {
	c.mu.Lock()
	c.snapSink = sink
	c.mu.Unlock()
}

// Shutdown closes the underlying yamux session and connection. Use this to
// cleanly tear down the hostclient; it does not implement SessionManager.Close.
// It marks the shutdown as intentional so runControlReader does not signal
// unexpected host loss.
func (c *Client) Shutdown() error {
	c.mu.Lock()
	sess := c.hostSess
	c.hostSess = nil
	c.ctrlEnc = nil
	c.shuttingDown = true
	c.mu.Unlock()
	if sess != nil {
		return sess.Close()
	}
	return nil
}

// Lost returns a channel that is closed when the host connection drops
// unexpectedly (i.e. not via a call to Shutdown). Connect selects on this
// to detect host death and restart.
func (c *Client) Lost() <-chan struct{} {
	return c.lost
}

// runAcceptStreams accepts host-opened yamux streams. The host opens a snapshot
// stream (identified by a SnapStreamHeader first line) and this goroutine
// relays its frames to the hub via the wired SnapshotSink.
func (c *Client) runAcceptStreams(sess *yamux.Session) {
	for {
		stream, err := sess.AcceptStream()
		if err != nil {
			// Session closed — normal on shutdown or host death; the control
			// reader goroutine handles the lost signaling.
			return
		}
		go c.handleHostStream(stream)
	}
}

// handleHostStream reads the first frame from a host-opened stream. If it is
// a SnapStreamHeader, subsequent frames are Snapshot records that are decoded
// and forwarded to the hub sink.
func (c *Client) handleHostStream(stream net.Conn) {
	defer func() { _ = stream.Close() }()

	dec := transport.NewDecoder(stream)
	frame, err := dec.Next()
	if err != nil {
		c.log.Debug("hostclient: host stream: read header failed", "err", err)
		return
	}

	switch frame.Type {
	case transport.TypeSnapStream:
		// Relay snapshot frames to the hub sink.
		c.relaySnapshots(dec)
	default:
		c.log.Debug("hostclient: host stream: unknown header type, ignoring", "type", frame.Type)
	}
}

// relaySnapshots reads Snapshot frames from dec and forwards them to the hub
// via the wired SnapshotSink. It runs until the stream is closed.
func (c *Client) relaySnapshots(dec *transport.Decoder) {
	for {
		frame, err := dec.Next()
		if err != nil {
			return
		}
		if frame.Type != transport.TypeSnapshot {
			c.log.Debug("hostclient: snapshot relay: unexpected frame type", "type", frame.Type)
			continue
		}
		snap, err := transport.Unmarshal[transport.Snapshot](frame)
		if err != nil {
			c.log.Warn("hostclient: snapshot relay: decode failed", "err", err)
			continue
		}

		c.mu.Lock()
		sink := c.snapSink
		c.mu.Unlock()
		if sink == nil {
			continue
		}

		// Convert transport.Snapshot back to terminal.SessionScreen for the
		// hubclient.Client.SendSnapshot path which re-encodes it. Since the
		// host already encoded the snapshot, we forward the raw transport form
		// directly by passing it through a minimal SessionScreen wrapper.
		// SendSnapshot calls encodeSnapshot internally which re-encodes; to
		// avoid double-encoding we use a rawSnapshotSink wrapper below.
		// We use sendRawSnapshot instead to avoid the re-encode round-trip.
		if rs, ok := sink.(rawSnapshotSink); ok {
			if err := rs.SendRawSnapshot(snap); err != nil {
				c.log.Warn("hostclient: snapshot relay: send failed", "err", err)
			}
		}
	}
}

// rawSnapshotSink is an optional extension of SnapshotSink that accepts
// pre-encoded transport.Snapshot frames directly, avoiding a double-encode
// round-trip through terminal.SessionScreen.
type rawSnapshotSink interface {
	SendRawSnapshot(s transport.Snapshot) error
}
