// Package tgclient wraps gotd/td into a small, MCP-friendly surface: log in
// once, list dialogs, list forum topics and read message history for a time
// window — with clickable deep links back to each message.
package tgclient

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/gotd/contrib/middleware/floodwait"
	"github.com/gotd/contrib/middleware/ratelimit"
	"github.com/gotd/log/logzap"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/time/rate"

	"github.com/20grizz03/telegram-mcp/internal/config"
)

// Client is a thread-safe wrapper around a single gotd client run.
type Client struct {
	cfg      config.Config
	tg       *telegram.Client
	api      *tg.Client
	sender   *message.Sender
	uploader *uploader.Uploader
	log      *zap.Logger
	loc      *time.Location
	waiter   *floodwait.Waiter

	mu        sync.Mutex
	peers     map[int64]peerInfo // id -> resolved input peer + metadata
	lastChats []Chat             // dialog-ordered snapshot from the last refresh
}

// peerInfo is everything needed to address a peer and build links for it.
type peerInfo struct {
	input     tg.InputPeerClass
	kind      PeerKind
	username  string
	channelID int64 // raw channel id (for private links), 0 for non-channels
	isForum   bool
	title     string
}

// New builds a gotd client with flood-wait + rate-limit middleware and a logger
// pinned to stderr (stdout is reserved for the MCP JSON-RPC stream).
func New(cfg config.Config) (*Client, error) {
	logger := zap.New(zapcore.NewCore(
		zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig()),
		zapcore.Lock(os.Stderr),
		zap.WarnLevel,
	))

	loc := time.Local

	waiter := floodwait.NewWaiter().WithMaxRetries(3)

	c := &Client{
		cfg:    cfg,
		log:    logger,
		loc:    loc,
		waiter: waiter,
		peers:  make(map[int64]peerInfo),
	}

	c.tg = telegram.NewClient(cfg.AppID, cfg.AppHash, telegram.Options{
		SessionStorage: &telegram.FileSessionStorage{Path: cfg.SessionPath()},
		Logger:         logzap.New(logger),
		Middlewares: []telegram.Middleware{
			waiter,
			// Be polite to Telegram: ~5 req/s with a small burst.
			ratelimit.New(rate.Every(time.Millisecond*200), 5),
		},
	})
	c.api = c.tg.API()
	c.uploader = uploader.NewUploader(c.api)
	c.sender = message.NewSender(c.api)

	return c, nil
}

// Run starts the gotd background loop (including the flood waiter) and invokes f
// once connected. It blocks until f returns or the context is cancelled.
func (c *Client) Run(ctx context.Context, f func(ctx context.Context) error) error {
	return c.waiter.Run(ctx, func(ctx context.Context) error {
		return c.tg.Run(ctx, func(ctx context.Context) error {
			return f(ctx)
		})
	})
}

// Location returns the location used to render message timestamps.
func (c *Client) Location() *time.Location { return c.loc }

// Self returns the currently authorized account.
func (c *Client) Self(ctx context.Context) (*tg.User, error) { return c.tg.Self(ctx) }
