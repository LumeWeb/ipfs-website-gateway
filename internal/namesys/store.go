package namesys

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	leveldb "github.com/ipfs/go-ds-leveldb"
	ds "github.com/ipfs/go-datastore"
	dsq "github.com/ipfs/go-datastore/query"
	dsns "github.com/ipfs/go-datastore/namespace"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/ipfs/boxo/namesys"
	"github.com/ipfs/boxo/path"
	"go.uber.org/zap"
)

const (
	stalePrefix    = "/ipns-stale/"
	ipnsDataPrefix = "/ipns-data/"
)

type persistentStaleEntry struct {
	Path     string `json:"p"`
	TTL      int64  `json:"t"`
	LastMod  int64  `json:"m"`
	CachedAt int64  `json:"c"`
}

type EvictFunc func(key string)

type IPNSStore struct {
	ldb      ds.Datastore
	ds       ds.Datastore
	staleDS  ds.Datastore
	stale    *lru.Cache[string, staleEntry]
	freshTTL time.Duration
	timeout  time.Duration
	logger   *zap.Logger
	onEvict  EvictFunc
}

func NewIPNSStore(cachePath string, freshTTL time.Duration, timeout time.Duration, maxSize int, logger *zap.Logger) (*IPNSStore, error) {
	if maxSize <= 0 {
		maxSize = 128
	}

	dbPath := filepath.Join(cachePath, "ipns-store")
	if err := os.MkdirAll(dbPath, 0755); err != nil {
		return nil, err
	}

	ldb, err := leveldb.NewDatastore(dbPath, nil)
	if err != nil {
		return nil, err
	}

	s := &IPNSStore{
		ldb:      ldb,
		ds:       dsns.Wrap(ldb, ds.NewKey(ipnsDataPrefix)),
		staleDS:  dsns.Wrap(ldb, ds.NewKey(stalePrefix)),
		freshTTL: freshTTL,
		timeout:  timeout,
		logger:   logger,
	}

	staleCache, err := lru.NewWithEvict[string, staleEntry](maxSize, func(key string, _ staleEntry) {
		if s.onEvict != nil {
			s.onEvict(key)
		}
	})
	if err != nil {
		_ = ldb.Close()
		return nil, err
	}

	s.stale = staleCache

	if err := s.loadStaleFromDisk(context.Background()); err != nil {
		logger.Warn("failed to load stale entries from disk, starting fresh", zap.Error(err))
	}

	return s, nil
}

func (s *IPNSStore) Datastore() ds.Datastore {
	return s.ds
}

func (s *IPNSStore) SetOnEvict(fn EvictFunc) {
	s.onEvict = fn
}

func (s *IPNSStore) Keys() []string {
	return s.stale.Keys()
}

func (s *IPNSStore) loadStaleFromDisk(ctx context.Context) error {
	results, err := s.staleDS.Query(ctx, dsq.Query{Prefix: "/"})
	if err != nil {
		return err
	}
	defer func() { _ = results.Close() }()

	loaded := 0

	for entry := range results.Next() {
		if entry.Error != nil {
			continue
		}

		var pe persistentStaleEntry
		if err := json.Unmarshal(entry.Value, &pe); err != nil {
			continue
		}

		p, err := path.NewPath(pe.Path)
		if err != nil {
			continue
		}

		cachedAt := time.Unix(0, pe.CachedAt)

		s.stale.Add(pe.Path, staleEntry{
			result: namesys.Result{
				Path:    p,
				TTL:     time.Duration(pe.TTL),
				LastMod: time.Unix(0, pe.LastMod),
			},
			cachedAt: cachedAt,
		})
		loaded++
	}

	s.logger.Info("loaded stale IPNS entries from disk",
		zap.Int("loaded", loaded),
	)

	return nil
}

func (s *IPNSStore) GetStale(key string) (staleEntry, bool) {
	entry, ok := s.stale.Get(key)
	if !ok {
		return staleEntry{}, false
	}
	return entry, true
}

func (s *IPNSStore) PutStale(key string, entry staleEntry) {
	s.stale.Add(key, entry)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pe := persistentStaleEntry{
		Path:     entry.result.Path.String(),
		TTL:      int64(entry.result.TTL),
		LastMod:  entry.result.LastMod.UnixNano(),
		CachedAt: entry.cachedAt.UnixNano(),
	}
	data, err := json.Marshal(pe)
	if err != nil {
		s.logger.Debug("failed to marshal stale entry", zap.Error(err))
		return
	}

	if err := s.staleDS.Put(ctx, ds.NewKey(key), data); err != nil {
		s.logger.Debug("failed to persist stale entry", zap.Error(err))
	}
}

func (s *IPNSStore) DeleteStale(key string) {
	evicted := s.stale.Remove(key)
	if !evicted {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.staleDS.Delete(ctx, ds.NewKey(key)); err != nil && !errors.Is(err, ds.ErrNotFound) {
		s.logger.Debug("failed to delete stale entry from disk", zap.Error(err))
	}
}

func (s *IPNSStore) Close() error {
	return s.ldb.Close()
}
