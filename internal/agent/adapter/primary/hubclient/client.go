package hubclient

import (
	"context"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/rizquuula/Constellate/internal/transport"
)

// Config holds all parameters needed to create a Client.
type Config struct {
	HubURL            string
	DevToken          string
	MachineID         string
	Name              string
	HeartbeatInterval time.Duration
	Log               *slog.Logger
}

// Client manages a persistent, auto-reconnecting connection to the hub.
type Client struct {
	hubURL            string
	devToken          string
	machineID         string
	name              string
	heartbeatInterval time.Duration
	log               *slog.Logger
}

// New creates a Client from cfg. Zero HeartbeatInterval defaults to 5s; nil
// Log defaults to a discard logger.
func New(cfg Config) *Client {
	if cfg.HeartbeatInterval == 0 {
		cfg.HeartbeatInterval = 5 * time.Second
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Client{
		hubURL:            cfg.HubURL,
		devToken:          cfg.DevToken,
		machineID:         cfg.MachineID,
		name:              cfg.Name,
		heartbeatInterval: cfg.HeartbeatInterval,
		log:               cfg.Log,
	}
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
// heartbeat/read loops until the connection breaks or ctx is canceled.
// connected is true when Hello was sent successfully.
func (c *Client) connectOnce(ctx context.Context) (connected bool, err error) {
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	conn, _, err := websocket.Dial(dialCtx, c.hubURL, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization": {"Bearer " + c.devToken},
		},
	})
	cancel()
	if err != nil {
		return false, err
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	connCtx, connCancel := context.WithCancel(ctx)
	defer connCancel()

	netConn := websocket.NetConn(connCtx, conn, websocket.MessageBinary)

	sess, err := transport.Client(netConn)
	if err != nil {
		return false, err
	}
	defer sess.Close()

	ctrl, err := sess.OpenStream()
	if err != nil {
		return false, err
	}

	enc := transport.NewEncoder(ctrl)
	if err := sendHello(enc, c.machineID, c.name); err != nil {
		return false, err
	}

	// From here the handshake succeeded.
	c.log.Info("connected to hub", "machineID", c.machineID, "hub", c.hubURL)

	errc := make(chan error, 2)

	// Heartbeat goroutine.
	go func() {
		ticker := time.NewTicker(c.heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-connCtx.Done():
				return
			case <-ticker.C:
				if err := enc.Encode(transport.NewHeartbeat(time.Now().Unix(), nil)); err != nil {
					errc <- err
					return
				}
			}
		}
	}()

	// Read goroutine.
	go func() {
		dec := transport.NewDecoder(ctrl)
		for {
			frame, err := dec.Next()
			if err != nil {
				errc <- err
				return
			}
			handleFrame(frame, c.machineID, c.log)
		}
	}()

	err = <-errc
	connCancel()
	return true, err
}
