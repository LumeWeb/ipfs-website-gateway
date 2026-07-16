package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

// ContentCache wraps a Boxo blockstore for caching IPFS blocks on disk.
// It monitors disk usage and enforces a maximum bytes threshold by implementing
// an eviction policy when the threshold is reached.
//
// This implementation provides a disk-based cache with LRU eviction for IPFS blocks.
// It stores blocks as files in the cache directory and tracks access times for
// least-recently-used eviction decisions.
type ContentCache struct {
	// cachePath is the directory path where cached blocks are stored on disk
	cachePath string

	// maxBytes is the maximum amount of disk space the cache is allowed to use
	maxBytes int64

	// mu protects concurrent access to cache state
	mu sync.RWMutex

	// currentBytes tracks the current disk usage of the cache
	currentBytes int64

	// lru tracks CIDs and their access times for O(1) eviction decisions
	// The LRU library provides thread-safe operations, but we still use mu
	// to coordinate with disk operations and maintain consistency
	lru *lru.Cache[string, time.Time]

	// onEvict is called for each CID evicted from the cache. Optional; if nil,
	// no callback fires. Used by the Prewarmer to clear warmed entries so
	// evicted sites become eligible for re-warming.
	onEvict func(cid string)
}

// NewContentCache creates a new ContentCache with the specified cache path, max bytes, and LRU size.
//
// The cachePath parameter specifies the directory where cached IPFS blocks will be stored.
// The directory will be created if it does not exist.
//
// The maxBytes parameter specifies the maximum amount of disk space the cache may use.
// When the cache reaches this threshold, blocks will be evicted according to the
// eviction policy (LRU).
//
// The lruSize parameter specifies the maximum number of entries to track in the LRU cache.
// A larger value provides more accurate eviction decisions at the cost of memory.
//
// Returns an error if cachePath is empty, maxBytes is not positive, lruSize is not positive,
// or if the cache directory cannot be created.
func NewContentCache(cachePath string, maxBytes int64, lruSize int) (*ContentCache, error) {
	if cachePath == "" {
		return nil, errors.New("cache path cannot be empty")
	}

	if maxBytes <= 0 {
		return nil, errors.New("max bytes must be positive")
	}

	if lruSize <= 0 {
		return nil, errors.New("lru size must be positive")
	}

	// Create cache directory if it doesn't exist
	if err := os.MkdirAll(cachePath, 0755); err != nil {
		return nil, err
	}

	// Initialize LRU cache for tracking access times
	lruCache, err := lru.New[string, time.Time](lruSize)
	if err != nil {
		return nil, err
	}

	cc := &ContentCache{
		cachePath:     cachePath,
		maxBytes:      maxBytes,
		currentBytes:  0,
		lru:           lruCache,
	}

	// Initialize currentBytes from existing cache directory
	if err := cc.initializeCacheSize(); err != nil {
		return nil, err
	}

	return cc, nil
}

// SetOnEvict registers a callback invoked for each CID evicted from the cache.
// The callback receives the CID string of the evicted block. This is used by
// the Prewarmer to clear warmed entries so evicted sites become re-warmable.
// Must be called before any Put/Evict operations. Not safe for concurrent
// use with active cache operations.
func (cc *ContentCache) SetOnEvict(fn func(cid string)) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.onEvict = fn
}

// getBlockPath returns the full path for a cached block using nginx-style directory hashing.
// The path structure is: <cachePath>/<level1>/<level2>/<cid>
// where level1 is the first hex character of the SHA-256 hash of the CID
// and level2 is the next two hex characters (levels=1:2).
// This reduces the number of files per directory from millions to ~732 on average.
func (cc *ContentCache) getBlockPath(cid string) string {
	// Compute SHA-256 hash of the CID
	hash := sha256.Sum256([]byte(cid))
	hashStr := hex.EncodeToString(hash[:])

	// Extract hash-based directory levels (nginx levels=1:2)
	level1 := hashStr[0:1]  // First hex character (16 possible values)
	level2 := hashStr[1:3]  // Next two hex characters (256 possible values)

	// Construct path: cachePath/level1/level2/cid
	return filepath.Join(cc.cachePath, level1, level2, cid)
}

// getBlockPathForTest is a test helper that exposes getBlockPath for testing.
// This function is only used in tests to verify the directory hashing logic.
func (cc *ContentCache) getBlockPathForTest(cid string) string {
	return cc.getBlockPath(cid)
}

// initializeCacheSize scans the cache directory and calculates the current disk usage.
// This is called during cache initialization to populate currentBytes with the
// actual size of existing cached blocks, and populates the LRU cache with existing CIDs.
func (cc *ContentCache) initializeCacheSize() error {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	var totalSize int64

	err := filepath.Walk(cc.cachePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			totalSize += info.Size()
			// Extract CID from filename and add to LRU cache
			// This ensures LRU and disk stay synchronized on initialization
			cid := filepath.Base(path)
			cc.lru.Add(cid, time.Now())
		}
		return nil
	})

	if err != nil {
		return err
	}

	cc.currentBytes = totalSize
	return nil
}

// Get retrieves a block from the cache by its CID.
//
// It checks if the block exists in the cache directory and returns its contents.
// If the block is found, the access time is updated for LRU eviction tracking.
// Returns an error if the block is not found or cannot be read.
func (cc *ContentCache) Get(cid string) ([]byte, error) {
	if cid == "" {
		return nil, errors.New("CID cannot be empty")
	}

	cc.mu.Lock()
	defer cc.mu.Unlock()

	// Try the new hashed path first
	blockPath := cc.getBlockPath(cid)
	
	// Check if block exists in hashed path
	if _, err := os.Stat(blockPath); err == nil {
		// Block found in hashed structure
	} else if os.IsNotExist(err) {
		// Not in hashed path, try legacy flat structure for backward compatibility.
		// This allows seamless migration from flat to hashed structure.
		legacyPath := filepath.Join(cc.cachePath, cid)
		if _, err := os.Stat(legacyPath); err != nil {
			if os.IsNotExist(err) {
				return nil, errors.New("block not found in cache")
			}
			return nil, err
		}
		// Found in legacy flat structure, use that path
		blockPath = legacyPath
	} else {
		// Some other error
		return nil, err
	}

	// Read block data
	data, err := os.ReadFile(blockPath)
	if err != nil {
		return nil, err
	}

	// Update access time in LRU cache for eviction tracking
	cc.lru.Add(cid, time.Now())

	return data, nil
}

// Put stores a block in the cache with the given CID.
//
// It stores the block as a file in the cache directory and updates disk usage tracking.
// If adding the block would exceed maxBytes, it triggers the eviction policy to free space.
// If a block with the same CID already exists, it is overwritten and the size difference is accounted for.
func (cc *ContentCache) Put(cid string, block []byte) error {
	if cid == "" {
		return errors.New("CID cannot be empty")
	}

	if len(block) == 0 {
		return errors.New("block cannot be empty")
	}

	blockSize := int64(len(block))

	cc.mu.Lock()
	defer cc.mu.Unlock()

	// Check if block already exists in either hashed or flat structure
	// Try hashed path first
	blockPath := cc.getBlockPath(cid)
	oldSize := int64(0)
	if info, err := os.Stat(blockPath); err == nil {
		oldSize = info.Size()
	} else if os.IsNotExist(err) {
		// Not in hashed path, check legacy flat structure
		legacyPath := filepath.Join(cc.cachePath, cid)
		if info, err := os.Stat(legacyPath); err == nil {
			oldSize = info.Size()
			// Will overwrite the old flat file with new hashed location
		}
	}

	// Check if we need to evict space
	if !cc.hasSpaceLocked(blockSize - oldSize) {
		bytesToFree := (blockSize - oldSize) - (cc.maxBytes - cc.currentBytes)
		if err := cc.evictLocked(bytesToFree); err != nil {
			return err
		}
	}

	// Create nested directory structure if needed
	if err := os.MkdirAll(filepath.Dir(blockPath), 0755); err != nil {
		return err
	}

	// Write block to file
	if err := os.WriteFile(blockPath, block, 0644); err != nil {
		return err
	}

	// Update current bytes
	cc.currentBytes += (blockSize - oldSize)

	// Add to LRU cache for tracking
	cc.lru.Add(cid, time.Now())

	return nil
}

// HasSize checks if the cache has space available for a block of the specified size.
// It returns true if adding the block would not exceed maxBytes, false otherwise.
func (cc *ContentCache) HasSize(size int64) bool {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	return cc.currentBytes+size <= cc.maxBytes
}

// hasSpaceLocked is the internal version of HasSize that assumes the lock is held.
func (cc *ContentCache) hasSpaceLocked(size int64) bool {
	return cc.currentBytes+size <= cc.maxBytes
}

// CurrentBytes returns the current disk usage of the cache in bytes.
func (cc *ContentCache) CurrentBytes() int64 {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	return cc.currentBytes
}

// MaxBytes returns the maximum allowed disk usage for the cache in bytes.
func (cc *ContentCache) MaxBytes() int64 {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	return cc.maxBytes
}

// CachePath returns the directory path where cached blocks are stored.
func (cc *ContentCache) CachePath() string {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	return cc.cachePath
}

// Evict removes blocks from the cache to free up space.
//
// It implements an LRU (Least Recently Used) eviction policy, removing the least
// recently accessed blocks until the specified amount of space is freed.
// Returns an error if the cache is empty or if blocks cannot be removed.
func (cc *ContentCache) Evict(bytesToFree int64) error {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	return cc.evictLocked(bytesToFree)
}

// evictLocked is the internal version of Evict that assumes the lock is held.
func (cc *ContentCache) evictLocked(bytesToFree int64) error {
	if cc.currentBytes == 0 {
		return errors.New("cache is empty, nothing to evict")
	}

	bytesFreed := int64(0)

	// Evict blocks using LRU's GetOldest() for O(1) access to least recently used
	for bytesFreed < bytesToFree {
		cid, _, ok := cc.lru.GetOldest()
		if !ok {
			// LRU cache is empty, but disk might still have entries
			// This can happen during initialization or if LRU and disk get out of sync
			// Fall back to disk-based eviction
			return cc.evictFromDiskLocked(bytesToFree - bytesFreed)
		}

		// Try hashed path first, then legacy flat path
		blockPath := cc.getBlockPath(cid)
		info, err := os.Stat(blockPath)
		if err != nil {
			// Not in hashed path, try legacy flat structure
			if os.IsNotExist(err) {
				blockPath = filepath.Join(cc.cachePath, cid)
				info, err = os.Stat(blockPath)
				if err != nil {
					// File doesn't exist in either location, remove from LRU and continue
					cc.lru.Remove(cid)
					continue
				}
			} else {
				// Some other error, remove from LRU and continue
				cc.lru.Remove(cid)
				continue
			}
		}

		// Remove from LRU cache BEFORE removing file to prevent re-adding
		cc.lru.Remove(cid)

		// Remove the file from disk
		if err := os.Remove(blockPath); err != nil {
			// Log warning but continue
			continue
		}

		// Update disk usage tracking
		cc.currentBytes -= info.Size()
		bytesFreed += info.Size()

		// Notify eviction callback (if set) so consumers can clear
		// derived state (e.g. Prewarmer.warmed entries).
		if cc.onEvict != nil {
			cc.onEvict(cid)
		}
	}

	if bytesFreed < bytesToFree {
		return errors.New("could not free enough space from cache")
	}

	return nil
}

// evictFromDiskLocked evicts blocks by scanning the disk directory directly.
// This is a fallback method used when the LRU cache is empty but disk has files.
// It handles both the new nested directory structure (levels=1:2) and legacy flat structure.
// It assumes the lock is held.
func (cc *ContentCache) evictFromDiskLocked(bytesToFree int64) error {
	if cc.currentBytes == 0 {
		return errors.New("cache is empty, nothing to evict")
	}

	bytesFreed := int64(0)

	// Walk the entire cache directory tree to find all files
	err := filepath.Walk(cc.cachePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and the root cache path itself
		if info.IsDir() || path == cc.cachePath {
			return nil
		}

		if bytesFreed >= bytesToFree {
			return nil
		}

		// Remove the file from disk
		if err := os.Remove(path); err != nil {
			// Continue on error
			return nil
		}

		// Update disk usage tracking
		cc.currentBytes -= info.Size()
		bytesFreed += info.Size()

		// Notify eviction callback (if set). The CID is the filename
		// basename in both hashed and legacy flat layouts.
		if cc.onEvict != nil {
			cc.onEvict(filepath.Base(path))
		}

		return nil
	})

	if err != nil {
		return err
	}

	if bytesFreed < bytesToFree {
		return errors.New("could not free enough space from cache")
	}

	return nil
}

// Clear removes all cached blocks from the cache directory.
// This resets the cache to an empty state and resets currentBytes to 0.
func (cc *ContentCache) Clear() error {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	// Remove all files in cache directory
	err := filepath.Walk(cc.cachePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			if err := os.Remove(path); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		return err
	}

	// Clear LRU cache
	cc.lru.Purge()

	cc.currentBytes = 0
	return nil
}
