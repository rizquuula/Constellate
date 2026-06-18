// Package localhost implements the host-side primary adapter that accepts
// connect-process connections over a Unix domain socket. It runs the existing
// transport.Server/yamux over the UDS net.Conn and dispatches local-protocol
// control frames into the session.Manager, mirroring the dispatch logic in
// hubclient/control.go.
package localhost

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/rizquuula/Constellate/internal/agent/app/session"
	"github.com/rizquuula/Constellate/internal/agent/domain/terminal"
	"github.com/rizquuula/Constellate/internal/transport"
)

// SessionManager is the consumer-side view the localhost server needs. Mirrors
// hubclient.SessionManager so both adapters share the same Manager.
type SessionManager interface {
	Open(sessionID string, spec session.PTYSpec) (pid int, err error)
	Attach(sessionID string, stream io.ReadWriteCloser, in io.Reader) error
	Resize(sessionID string, cols, rows int) error
	Close(sessionID string) error
	// Sessions returns a snapshot of open session info (id+pid).
	Sessions() []session.SessionInfo
	// Activities returns per-session activity signals at unix-second timestamp now.
	Activities(now int64) []terminal.SessionActivity
}

// SnapshotToggle is the consumer-side interface for enabling/disabling the
// snapshot producer. *snapshot.Producer satisfies this structurally.
type SnapshotToggle interface {
	SetEnabled(bool)
}

// connectSink is a per-connection snapshot sink that opens yamux streams on
// demand to forward Snapshot frames to connect. It is safe for concurrent use
// (though in practice only the snapshot producer goroutine calls SendSnapshot).
type connectSink struct {
	mu     sync.Mutex
	sess   *yamux.Session
	stream net.Conn
	enc    *transport.Encoder
	log    *slog.Logger
}

// SendSnapshot opens a local snapshot stream on the first call (per connection)
// and writes the snapshot frame. If the session is nil (no connect attached),
// the snapshot is silently dropped.
// Network I/O (OpenStream + header write) happens WITHOUT holding mu to avoid
// stalling attach()/detach() during a connect reconnect.
func (s *connectSink) SendSnapshot(screen terminal.SessionScreen) error {
	// Capture sess and current encoder under lock, then release.
	s.mu.Lock()
	sess := s.sess
	enc := s.enc
	s.mu.Unlock()

	if sess == nil {
		return nil // no connect attached; drop silently
	}

	// If we don't have an encoder yet, open the stream without holding mu.
	if enc == nil {
		stream, err := sess.OpenStream()
		if err != nil {
			return fmt.Errorf("localhost: open snapshot stream: %w", err)
		}
		newEnc := transport.NewEncoder(stream)
		if err := newEnc.Encode(transport.NewSnapStreamHeader()); err != nil {
			_ = stream.Close()
			return fmt.Errorf("localhost: write snap stream header: %w", err)
		}
		// Re-acquire mu and store only if the session hasn't been replaced.
		s.mu.Lock()
		if s.sess != sess {
			// A reconnect happened while we were opening — discard our stream.
			s.mu.Unlock()
			_ = stream.Close()
			return nil
		}
		if s.enc != nil {
			// Another call beat us (shouldn't happen — single producer); discard ours.
			s.mu.Unlock()
			_ = stream.Close()
			enc = s.enc
		} else {
			s.stream = stream
			s.enc = newEnc
			enc = newEnc
			s.mu.Unlock()
		}
	}

	snap := encodeLocalSnapshot(screen)
	if err := enc.Encode(snap); err != nil {
		// Stream broken; close and nil so we reopen on next call — but only if
		// we're still the active encoder. A concurrent detach()/reconnect may
		// have already swapped or cleared s.stream/s.enc, in which case it owns
		// the cleanup and s.stream may already be nil.
		s.mu.Lock()
		if s.enc == enc {
			if s.stream != nil {
				_ = s.stream.Close()
			}
			s.stream = nil
			s.enc = nil
		}
		s.mu.Unlock()
		return fmt.Errorf("localhost: write snapshot: %w", err)
	}
	return nil
}

// attach wires a new yamux session for outbound snapshot streams. Any prior
// stream is closed. Called when a new connect client connects.
func (s *connectSink) attach(sess *yamux.Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stream != nil {
		_ = s.stream.Close()
		s.stream = nil
		s.enc = nil
	}
	s.sess = sess
}

// detach removes the yamux session reference. Called when a connect client
// disconnects. After this, SendSnapshot drops silently until attach is called.
func (s *connectSink) detach() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stream != nil {
		_ = s.stream.Close()
		s.stream = nil
		s.enc = nil
	}
	s.sess = nil
}

// encodeLocalSnapshot builds a transport.Snapshot from a terminal.SessionScreen.
// Reuses the same encoding logic as hubclient/snapshot_encode.go but without
// the machineID field (the hub fills it in when relaying). The machineID is set
// to empty here; connect sets it when forwarding to the hub snapshot stream.
func encodeLocalSnapshot(s terminal.SessionScreen) transport.Snapshot {
	scr := s.Screen
	lines := make([]transport.SnapLine, scr.Rows)
	for y := 0; y < scr.Rows; y++ {
		row := scr.Cells[y]
		if len(row) == 0 {
			lines[y] = transport.SnapLine{}
			continue
		}
		var runs []transport.SnapRun
		runFG := row[0].FG
		runBG := row[0].BG
		runAttrs := row[0].Attrs
		runText := []rune{row[0].Rune}
		flush := func() {
			if len(runText) == 0 {
				return
			}
			runs = append(runs, transport.SnapRun{
				Text:  string(runText),
				FG:    runFG,
				BG:    runBG,
				Attrs: runAttrs,
			})
		}
		for x := 1; x < len(row); x++ {
			c := row[x]
			if c.FG == runFG && c.BG == runBG && c.Attrs == runAttrs {
				runText = append(runText, c.Rune)
			} else {
				flush()
				runFG = c.FG
				runBG = c.BG
				runAttrs = c.Attrs
				runText = []rune{c.Rune}
			}
		}
		flush()
		for len(runs) > 0 {
			last := runs[len(runs)-1]
			if last.FG == 0 && last.BG == 0 && last.Attrs == 0 && isAllSpaces(last.Text) {
				runs = runs[:len(runs)-1]
			} else {
				break
			}
		}
		lines[y] = transport.SnapLine{Runs: runs}
	}
	return transport.Snapshot{
		Type:      transport.TypeSnapshot,
		SessionID: s.ID,
		MachineID: "", // connect fills this in when relaying to the hub
		Cols:      scr.Cols,
		Rows:      scr.Rows,
		Cursor: transport.Cursor{
			X:       scr.Cursor.X,
			Y:       scr.Cursor.Y,
			Visible: scr.Cursor.Visible,
		},
		Lines: lines,
		Rev:   s.Rev,
	}
}

func isAllSpaces(s string) bool {
	for _, r := range s {
		if r != ' ' {
			return false
		}
	}
	return true
}

// statInterval is how often the host pushes LocalStat frames to connect.
const statInterval = 5 * time.Second

// Server listens on a UDS and serves one connect client at a time.
// Each accepted connection gets its own yamux session.
type Server struct {
	instanceID string
	sessions   SessionManager
	snaps      SnapshotToggle // may be nil when no producer is wired
	sink       *connectSink
	log        *slog.Logger
}

// New creates a Server. instanceID must be the durable identity generated once
// at session-host start; it is sent to every connecting client in HostInfo.
func New(instanceID string, sessions SessionManager, log *slog.Logger) *Server {
	return &Server{
		instanceID: instanceID,
		sessions:   sessions,
		sink:       &connectSink{log: log},
		log:        log,
	}
}

// SetSnapshotToggle wires the snapshot producer toggle. Must be called before
// Serve if snapshot production is desired.
func (s *Server) SetSnapshotToggle(t SnapshotToggle) {
	s.snaps = t
}

// Sink returns the connectSink, which implements snapshot.SnapshotSink.
// Pass this as the sink to snapshot.New so the producer sends through the
// current connect connection.
func (s *Server) Sink() *connectSink {
	return s.sink
}

// Serve accepts connections from ln and handles each in its own goroutine.
// It returns when ln is closed (or another accept error occurs).
func (s *Server) Serve(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			// A closed listener is the normal shutdown path.
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go s.handleConn(conn)
	}
}

// handleConn wraps conn in a yamux server session (host == server side here;
// connect is the yamux client) and runs the control + data-stream loops.
func (s *Server) handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	// For the local UDS protocol the host acts as the yamux *server* (accepts
	// streams) and connect acts as the yamux *client* (opens streams).
	sess, err := transport.Server(conn)
	if err != nil {
		s.log.Warn("localhost: yamux server failed", "err", err)
		return
	}
	defer func() { _ = sess.Close() }()

	// The connect side opens the control stream first.
	ctrl, err := sess.AcceptStream()
	if err != nil {
		s.log.Warn("localhost: accept control stream failed", "err", err)
		return
	}
	defer func() { _ = ctrl.Close() }()

	dec := transport.NewDecoder(ctrl)
	enc := transport.NewEncoder(ctrl)

	// Handshake: read HostHello from connect.
	frame, err := dec.Next()
	if err != nil {
		s.log.Warn("localhost: read HostHello failed", "err", err)
		return
	}
	if frame.Type != transport.TypeHostHello {
		s.log.Warn("localhost: expected HostHello, got unexpected type", "type", frame.Type)
		return
	}
	hello, err := transport.Unmarshal[transport.HostHello](frame)
	if err != nil {
		s.log.Warn("localhost: decode HostHello failed", "err", err)
		return
	}

	// Negotiate local protocol version (min of both sides).
	negotiated := hello.LocalProtocol
	if transport.LocalProtocolVersion < negotiated {
		negotiated = transport.LocalProtocolVersion
	}

	// Reply with HostInfo.
	info := transport.NewHostInfo(s.instanceID, negotiated, sessionStubs(s.sessions.Sessions()))
	if err := enc.Encode(info); err != nil {
		s.log.Warn("localhost: send HostInfo failed", "err", err)
		return
	}

	// v2+ only: attach the connect session to the snapshot sink so the producer
	// can send snapshots to this connection. A v1 connect must not receive
	// snapshot streams. Detach when the connection ends.
	if negotiated >= 2 {
		s.sink.attach(sess)
		defer s.sink.detach()
	}

	s.log.Info("localhost: connect client attached",
		"instanceID", s.instanceID, "negotiatedProto", negotiated)

	// Dispatch control frames from connect; accept data streams in parallel;
	// and push LocalStat frames to connect periodically.
	errc := make(chan error, 3)

	go func() {
		for {
			f, err := dec.Next()
			if err != nil {
				errc <- err
				return
			}
			s.handleControlFrame(enc, f, negotiated)
		}
	}()

	go func() {
		for {
			stream, err := sess.AcceptStream()
			if err != nil {
				errc <- err
				return
			}
			go s.handleDataStream(stream)
		}
	}()

	// v2+ only: periodic LocalStat sender — push activity signals to connect so
	// it can fold them into the hub Heartbeat. A v1 connect does not know this
	// frame type.
	if negotiated >= 2 {
		go func() {
			ticker := time.NewTicker(statInterval)
			defer ticker.Stop()
			for range ticker.C {
				now := time.Now().Unix()
				acts := s.sessions.Activities(now)
				localActs := make([]transport.LocalSessionActivity, len(acts))
				for i, a := range acts {
					localActs[i] = transport.LocalSessionActivity{
						ID:       a.ID,
						Activity: string(a.Activity),
					}
				}
				if err := enc.Encode(transport.NewLocalStat(localActs)); err != nil {
					errc <- err
					return
				}
			}
		}()
	}

	if err := <-errc; err != nil && !errors.Is(err, io.EOF) {
		s.log.Debug("localhost: connection closed", "err", err)
	}
}

// handleControlFrame processes a single inbound frame from connect, mirroring
// the dispatch logic in hubclient/control.go. negotiated is the protocol version
// agreed during handshake; it is used to advertise the correct version in replies.
func (s *Server) handleControlFrame(enc *transport.Encoder, frame transport.Frame, negotiated int) {
	switch frame.Type {
	case transport.TypeOpenSession:
		msg, err := transport.Unmarshal[transport.OpenSession](frame)
		if err != nil {
			s.log.Warn("localhost: decode OpenSession failed", "err", err)
			return
		}
		pid, err := s.sessions.Open(msg.SessionID, session.PTYSpec{
			Shell:     msg.Shell,
			Cwd:       msg.Cwd,
			Cols:      msg.Cols,
			Rows:      msg.Rows,
			CreateDir: msg.CreateDir,
		})
		if err != nil {
			s.log.Warn("localhost: open session failed", "sessionID", msg.SessionID, "err", err)
			code := "open_failed"
			if errors.Is(err, session.ErrCwdNotFound) {
				code = "cwd_not_found"
			}
			_ = enc.Encode(transport.NewError(msg.SessionID, code, err.Error()))
			return
		}
		s.log.Info("localhost: session opened", "sessionID", msg.SessionID, "pid", pid)
		_ = enc.Encode(transport.NewSessionOpened(msg.SessionID, pid))

	case transport.TypeResize:
		msg, err := transport.Unmarshal[transport.Resize](frame)
		if err != nil {
			s.log.Warn("localhost: decode Resize failed", "err", err)
			return
		}
		if err := s.sessions.Resize(msg.SessionID, msg.Cols, msg.Rows); err != nil {
			s.log.Warn("localhost: resize failed", "sessionID", msg.SessionID, "err", err)
		}

	case transport.TypeCloseSession:
		msg, err := transport.Unmarshal[transport.CloseSession](frame)
		if err != nil {
			s.log.Warn("localhost: decode CloseSession failed", "err", err)
			return
		}
		if err := s.sessions.Close(msg.SessionID); err != nil {
			s.log.Warn("localhost: close session failed", "sessionID", msg.SessionID, "err", err)
		}

	case transport.TypeEnableSnaps:
		msg, err := transport.Unmarshal[transport.EnableSnaps](frame)
		if err != nil {
			s.log.Warn("localhost: decode EnableSnaps failed", "err", err)
			return
		}
		if s.snaps != nil {
			s.snaps.SetEnabled(msg.Enabled)
		}
		s.log.Debug("localhost: EnableSnaps received", "enabled", msg.Enabled)

	case transport.TypeListSessions:
		// Respond with the current session list in a HostInfo frame, using the
		// negotiated version rather than the host's own version.
		info := transport.NewHostInfo(s.instanceID, negotiated, sessionStubs(s.sessions.Sessions()))
		if err := enc.Encode(info); err != nil {
			s.log.Warn("localhost: send HostInfo (ListSessions) failed", "err", err)
		}

	case transport.TypeError:
		e, err := transport.Unmarshal[transport.Error](frame)
		if err != nil {
			s.log.Warn("localhost: decode Error frame failed", "err", err)
			return
		}
		s.log.Warn("localhost: connect reported error", "code", e.Code, "message", e.Message)

	default:
		s.log.Debug("localhost: unknown frame type, ignoring", "type", frame.Type)
	}
}

// sessionStubs converts []session.SessionInfo (app-layer type) to
// []transport.SessionStub (wire-layer type) at the adapter boundary.
func sessionStubs(infos []session.SessionInfo) []transport.SessionStub {
	out := make([]transport.SessionStub, len(infos))
	for i, inf := range infos {
		out[i] = transport.SessionStub{ID: inf.ID, PID: inf.PID}
	}
	return out
}

// handleDataStream processes a connect-opened data stream by reading the attach
// header and forwarding the stream to Manager.Attach. This mirrors
// hubclient/streams.go but with connect as the stream opener.
func (s *Server) handleDataStream(stream net.Conn) {
	defer func() { _ = stream.Close() }()

	hdr, br, err := transport.ReadAttachHeader(stream)
	if err != nil {
		s.log.Warn("localhost: data stream: read attach header failed", "err", err)
		return
	}

	s.log.Debug("localhost: data stream: attaching", "sessionID", hdr.SessionID)
	if err := s.sessions.Attach(hdr.SessionID, stream, br); err != nil {
		s.log.Debug("localhost: data stream: attach returned", "sessionID", hdr.SessionID, "err", err)
	}
	s.log.Debug("localhost: data stream: detached", "sessionID", hdr.SessionID)
}
