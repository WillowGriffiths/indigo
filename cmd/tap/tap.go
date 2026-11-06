package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/identity/apidir"
	"github.com/bluesky-social/indigo/cmd/tap/models"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Tap struct {
	db     *gorm.DB
	logger *slog.Logger

	firehose *FirehoseProcessor
	events   *EventManager
	repos    *RepoManager
	resyncer *Resyncer
	crawler  *Crawler

	server *TapServer
	outbox *Outbox

	outboxOnly bool
}

type TapConfig struct {
	DatabaseURL                string
	DBMaxConns                 int
	PLCURL                     string
	RelayUrl                   string
	FirehoseParallelism        int
	ResyncParallelism          int
	OutboxParallelism          int
	FirehoseCursorSaveInterval time.Duration
	NoReplay                   bool
	RepoFetchTimeout           time.Duration
	IdentityCacheSize          int
	EventCacheSize             int
	FullNetworkMode            bool
	SignalCollection           string
	DisableAcks                bool
	WebhookURL                 string
	CollectionFilters          []string // e.g., ["app.bsky.feed.post", "app.bsky.graph.*"]
	OutboxOnly                 bool
	AdminPassword              string
	RetryTimeout               time.Duration
	// If set, all handle resolution is routed through a service implementing the
	// `com.atproto.identity.resolveHandle` XRPC endpoint (eg, a local bluepages
	// or mock in a dev environment). DID resolution is unaffected.
	HandleResolveURL string
}

func NewTap(config TapConfig) (*Tap, error) {
	db, err := SetupDatabase(config.DatabaseURL, config.DBMaxConns)
	if err != nil {
		return nil, err
	}

	bdir := identity.BaseDirectory{
		PLCURL:                config.PLCURL,
		TryAuthoritativeDNS:   false,
		SkipDNSDomainSuffixes: []string{".bsky.social"},
	}
	if config.HandleResolveURL != "" {
		// route all handle resolution through a dedicated service implementing
		// the `com.atproto.identity.resolveHandle` XRPC endpoint (eg, a local
		// mock in a dev environment). DID resolution still uses PLC/did:web.
		bdir.HandleResolver = apidir.NewAPIDirectory(config.HandleResolveURL)
		slog.Default().Info("routing handle resolution through remote service", "url", config.HandleResolveURL)
	}
	cdir := identity.NewCacheDirectory(&bdir, config.IdentityCacheSize, time.Hour*24, time.Minute*2, time.Minute*5)

	logger := slog.Default().With("system", "tap")

	evtMngr := NewEventManager(logger, db, &config)

	repoMngr := NewRepoManager(logger, db, evtMngr, cdir)

	resyncer := NewResyncer(logger, db, repoMngr, evtMngr, &config)

	firehose := NewFirehoseProcessor(logger, db, evtMngr, repoMngr, &config)

	crawler := NewCrawler(logger, db, &config)

	outbox := NewOutbox(logger, evtMngr, &config)

	server := NewTapServer(logger, db, outbox, cdir, firehose, crawler, &config)

	t := &Tap{
		db:     db,
		logger: slog.Default().With("system", "tap"),

		firehose:   firehose,
		events:     evtMngr,
		repos:      repoMngr,
		resyncer:   resyncer,
		crawler:    crawler,
		server:     server,
		outbox:     outbox,
		outboxOnly: config.OutboxOnly,
	}

	if err := t.resyncer.resetPartiallyResynced(context.Background()); err != nil {
		return nil, err
	}

	return t, nil
}

// Run starts internal background workers for resync, cursor saving, and outbox delivery.
func (t *Tap) Run(ctx context.Context) {
	go t.events.LoadEvents(ctx)

	if !t.outboxOnly {
		go t.resyncer.run(ctx)
	}

	go t.outbox.Run(ctx)
}

func (t *Tap) CloseDb(ctx context.Context) error {
	t.logger.Info("shutting down tap")

	sqlDB, err := t.db.DB()
	if err != nil {
		t.logger.Error("error getting sql db", "error", err)
		return err
	}

	if err := sqlDB.Close(); err != nil {
		t.logger.Error("error closing sqlite db", "error", err)
		return err
	}

	return nil
}

func SetupDatabase(dbUrl string, maxConns int) (*gorm.DB, error) {
	// Setup database connection (supports both SQLite and Postgres)
	var dialector gorm.Dialector
	isSqlite := false

	if strings.HasPrefix(dbUrl, "sqlite://") {
		sqlitePath := dbUrl[len("sqlite://"):]
		dialector = sqlite.Open(sqlitePath)
		isSqlite = true
	} else if strings.HasPrefix(dbUrl, "postgresql://") || strings.HasPrefix(dbUrl, "postgres://") {
		dialector = postgres.Open(dbUrl)
	} else {
		return nil, fmt.Errorf("unsupported database URL scheme: must start with sqlite://, postgres://, or postgresql://")
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}

	if isSqlite {
		db.Exec("PRAGMA journal_mode=WAL;")
		db.Exec("PRAGMA synchronous=NORMAL;")
		db.Exec("PRAGMA busy_timeout=10000;")
		db.Exec("PRAGMA temp_store=MEMORY;")
		db.Exec("PRAGMA cache_size=8000;")
		db.Exec("PRAGMA wal_autocheckpoint=3000;")
	} else {
		// Configure connection pool
		sqlDB, err := db.DB()
		if err != nil {
			return nil, err
		}
		sqlDB.SetMaxOpenConns(maxConns)
		sqlDB.SetMaxIdleConns(maxConns)
		sqlDB.SetConnMaxIdleTime(time.Hour)

	}

	if err := db.AutoMigrate(&models.Repo{}, &models.RepoRecord{}, &models.OutboxBuffer{}, &models.ResyncBuffer{}, &models.FirehoseCursor{}, &models.ListReposCursor{}, &models.CollectionCursor{}); err != nil {
		return nil, err
	}

	return db, nil
}
