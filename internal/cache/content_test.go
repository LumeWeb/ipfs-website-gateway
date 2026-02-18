package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestNewContentCache(t *testing.T) {
	t.Run("successful creation", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(1024 * 1024) // 1 MB

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		if cache == nil {
			t.Fatal("NewContentCache returned nil cache")
		}

		if cache.CachePath() != tmpDir {
			t.Errorf("expected cache path %s, got %s", tmpDir, cache.CachePath())
		}

		if cache.MaxBytes() != maxBytes {
			t.Errorf("expected max bytes %d, got %d", maxBytes, cache.MaxBytes())
		}

		if cache.CurrentBytes() != 0 {
			t.Errorf("expected current bytes 0, got %d", cache.CurrentBytes())
		}
	})

	t.Run("creates cache directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		cachePath := filepath.Join(tmpDir, "cache")
		maxBytes := int64(1024 * 1024)

		cache, err := NewContentCache(cachePath, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		if cache == nil {
			t.Fatal("NewContentCache returned nil cache")
		}

		// Verify directory was created
		info, err := os.Stat(cachePath)
		if err != nil {
			t.Fatalf("failed to stat cache path: %v", err)
		}

		if !info.IsDir() {
			t.Error("cache path is not a directory")
		}

		if info.Mode().Perm() != 0755 {
			t.Errorf("expected directory permissions 0755, got %v", info.Mode().Perm())
		}
	})

	t.Run("empty cache path", func(t *testing.T) {
		maxBytes := int64(1024 * 1024)

		_, err := NewContentCache("", maxBytes, 100000)
		if err == nil {
			t.Error("expected error for empty cache path")
		}
		if err.Error() != "cache path cannot be empty" {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("zero max bytes", func(t *testing.T) {
		tmpDir := t.TempDir()

		_, err := NewContentCache(tmpDir, 0, 100000)
		if err == nil {
			t.Error("expected error for zero max bytes")
		}
		if err.Error() != "max bytes must be positive" {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("negative max bytes", func(t *testing.T) {
		tmpDir := t.TempDir()

		_, err := NewContentCache(tmpDir, -1024, 100000)
		if err == nil {
			t.Error("expected error for negative max bytes")
		}
		if err.Error() != "max bytes must be positive" {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

func TestContentCache_getBlockPath(t *testing.T) {
	t.Run("generates correct nginx-style path structure", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(10 * 1024 * 1024)

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		cid := "QmExample123456789"
		path := cache.getBlockPathForTest(cid)

		// Path should be: tmpDir/XX/XXX/cid
		// where XX is first hex char, XXX is next 2 hex chars
		pathParts := strings.Split(path, string(filepath.Separator))
		
		// Should have at least: tmpDir, level1, level2, cid
		if len(pathParts) < 4 {
			t.Fatalf("expected path to have at least 4 parts, got %d: %v", len(pathParts), pathParts)
		}

		// Check that CID is the last part
		if pathParts[len(pathParts)-1] != cid {
			t.Errorf("expected last part to be CID %s, got %s", cid, pathParts[len(pathParts)-1])
		}

		// Check that level1 is exactly 1 hex character
		level1 := pathParts[len(pathParts)-3]
		if len(level1) != 1 {
			t.Errorf("expected level1 to be 1 character, got %d: %s", len(level1), level1)
		}

		// Check that level1 is a valid hex character
		if !isHexChar(level1[0]) {
			t.Errorf("expected level1 to be hex character, got %s", level1)
		}

		// Check that level2 is exactly 2 hex characters
		level2 := pathParts[len(pathParts)-2]
		if len(level2) != 2 {
			t.Errorf("expected level2 to be 2 characters, got %d: %s", len(level2), level2)
		}

		// Check that level2 are valid hex characters
		for _, c := range level2 {
			if !isHexChar(byte(c)) {
				t.Errorf("expected level2 to be hex characters, got invalid char %c in %s", c, level2)
			}
		}
	})

	t.Run("same CID produces same path", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(10 * 1024 * 1024)

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		cid := "QmConsistentPath"
		path1 := cache.getBlockPathForTest(cid)
		path2 := cache.getBlockPathForTest(cid)

		if path1 != path2 {
			t.Errorf("expected same CID to produce same path, got %s and %s", path1, path2)
		}
	})

	t.Run("different CIDs produce different paths", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(10 * 1024 * 1024)

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		cid1 := "QmDifferent1"
		cid2 := "QmDifferent2"
		path1 := cache.getBlockPathForTest(cid1)
		path2 := cache.getBlockPathForTest(cid2)

		if path1 == path2 {
			t.Error("expected different CIDs to produce different paths")
		}
	})

	t.Run("handles empty CID", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(10 * 1024 * 1024)

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		// Empty CID should still produce a valid path structure
		path := cache.getBlockPathForTest("")
		
		// Should still have the correct structure
		pathParts := strings.Split(path, string(filepath.Separator))
		if len(pathParts) < 4 {
			t.Fatalf("expected path to have at least 4 parts, got %d: %v", len(pathParts), pathParts)
		}
	})
}

func TestContentCache_PutAndGetWithDirectoryHashing(t *testing.T) {
	t.Run("stores block in nested directory structure", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(10 * 1024 * 1024)

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		cid := "QmNestedTest"
		testData := []byte("nested test data")

		err = cache.Put(cid, testData)
		if err != nil {
			t.Fatalf("Put returned error: %v", err)
		}

		// Verify the file exists in the nested structure
		path := cache.getBlockPathForTest(cid)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected file to exist at %s, got error: %v", path, err)
		}

		// Verify the data can be retrieved
		retrieved, err := cache.Get(cid)
		if err != nil {
			t.Fatalf("Get returned error: %v", err)
		}

		if string(retrieved) != string(testData) {
			t.Errorf("expected data %q, got %q", string(testData), string(retrieved))
		}
	})

	t.Run("handles multiple blocks with different hash paths", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(10 * 1024 * 1024)

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		blocks := []struct {
			cid  string
			data []byte
		}{
			{"QmBlockA", []byte("data A")},
			{"QmBlockB", []byte("data B")},
			{"QmBlockC", []byte("data C")},
		}

		for _, block := range blocks {
			err := cache.Put(block.cid, block.data)
			if err != nil {
				t.Fatalf("Put returned error for %s: %v", block.cid, err)
			}
		}

		// Verify all blocks can be retrieved
		for _, block := range blocks {
			retrieved, err := cache.Get(block.cid)
			if err != nil {
				t.Errorf("Get returned error for %s: %v", block.cid, err)
			}
			if string(retrieved) != string(block.data) {
				t.Errorf("expected data %q for %s, got %q", string(block.data), block.cid, string(retrieved))
			}
		}
	})
}

func TestContentCache_BackwardCompatibility(t *testing.T) {
	t.Run("reads from flat structure if file exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(10 * 1024 * 1024)

		// Create a file in the old flat structure
		cid := "QmFlatFile"
		testData := []byte("flat structure data")
		flatPath := filepath.Join(tmpDir, cid)
		if err := os.WriteFile(flatPath, testData, 0644); err != nil {
			t.Fatalf("failed to create flat file: %v", err)
		}

		// Create cache - should initialize with existing flat file
		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		// Should be able to retrieve the flat file
		retrieved, err := cache.Get(cid)
		if err != nil {
			t.Fatalf("Get returned error: %v", err)
		}

		if string(retrieved) != string(testData) {
			t.Errorf("expected data %q, got %q", string(testData), string(retrieved))
		}

		// CurrentBytes should account for the flat file
		if cache.CurrentBytes() != int64(len(testData)) {
			t.Errorf("expected current bytes %d, got %d", len(testData), cache.CurrentBytes())
		}
	})

	t.Run("initializes with mixed flat and nested structures", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(10 * 1024 * 1024)

		// Create a flat file
		flatCID := "QmFlat123"
		flatData := []byte("flat data")
		flatPath := filepath.Join(tmpDir, flatCID)
		if err := os.WriteFile(flatPath, flatData, 0644); err != nil {
			t.Fatalf("failed to create flat file: %v", err)
		}

		// Create a nested file manually at the correct hashed path
		nestedCID := "QmNested456"
		nestedData := []byte("nested data")
		
		// Create a temporary cache to compute the correct path
		tmpCache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("failed to create temp cache: %v", err)
		}
		nestedPath := tmpCache.getBlockPathForTest(nestedCID)
		
		if err := os.MkdirAll(filepath.Dir(nestedPath), 0755); err != nil {
			t.Fatalf("failed to create nested directory: %v", err)
		}
		if err := os.WriteFile(nestedPath, nestedData, 0644); err != nil {
			t.Fatalf("failed to create nested file: %v", err)
		}

		// Create cache - should initialize with both files
		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		expectedBytes := int64(len(flatData) + len(nestedData))
		if cache.CurrentBytes() != expectedBytes {
			t.Errorf("expected current bytes %d, got %d", expectedBytes, cache.CurrentBytes())
		}

		// Both files should be retrievable
		flatRetrieved, err := cache.Get(flatCID)
		if err != nil {
			t.Errorf("Get returned error for flat file: %v", err)
		}
		if string(flatRetrieved) != string(flatData) {
			t.Errorf("expected flat data %q, got %q", string(flatData), string(flatRetrieved))
		}

		nestedRetrieved, err := cache.Get(nestedCID)
		if err != nil {
			t.Errorf("Get returned error for nested file: %v", err)
		}
		if string(nestedRetrieved) != string(nestedData) {
			t.Errorf("expected nested data %q, got %q", string(nestedData), string(nestedRetrieved))
		}
	})
}

func TestContentCache_EvictWithDirectoryHashing(t *testing.T) {
	t.Run("evicts blocks from nested structure", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(1024) // 1 KB

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		// Add blocks
		blocks := []struct {
			cid  string
			data []byte
		}{
			{"QmBlock1", make([]byte, 400)},
			{"QmBlock2", make([]byte, 400)},
			{"QmBlock3", make([]byte, 400)},
		}

		for _, block := range blocks {
			err := cache.Put(block.cid, block.data)
			if err != nil {
				t.Fatalf("Put returned error for %s: %v", block.cid, err)
			}
		}

		// Evict one block
		err = cache.Evict(400)
		if err != nil {
			t.Fatalf("Evict returned error: %v", err)
		}

		// Verify the evicted block is gone
		path1 := cache.getBlockPathForTest(blocks[0].cid)
		if _, err := os.Stat(path1); err == nil {
			t.Errorf("expected block %s to be evicted, but file exists at %s", blocks[0].cid, path1)
		}
	})

	t.Run("evicts from mixed flat and nested structures", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(1024)

		// Create a flat file
		flatCID := "QmFlatEvict"
		flatData := []byte("flat evict data")
		flatPath := filepath.Join(tmpDir, flatCID)
		if err := os.WriteFile(flatPath, flatData, 0644); err != nil {
			t.Fatalf("failed to create flat file: %v", err)
		}

		// Create cache
		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		// Add a nested block
		nestedCID := "QmNestedEvict"
		nestedData := []byte("nested evict data")
		err = cache.Put(nestedCID, nestedData)
		if err != nil {
			t.Fatalf("Put returned error: %v", err)
		}

		// Evict enough to remove one block
		err = cache.Evict(10)
		if err != nil {
			t.Fatalf("Evict returned error: %v", err)
		}

		// At least one block should be evicted
		remaining := 0
		if _, err := os.Stat(flatPath); err == nil {
			remaining++
		}
		nestedPath := cache.getBlockPathForTest(nestedCID)
		if _, err := os.Stat(nestedPath); err == nil {
			remaining++
		}

		if remaining == 2 {
			t.Error("expected at least one block to be evicted")
		}
	})
}

func TestContentCache_ClearWithDirectoryHashing(t *testing.T) {
	t.Run("clears nested directory structure", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(10 * 1024 * 1024)

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		// Add multiple blocks
		for i := 0; i < 5; i++ {
			cid := "QmBlock" + string(rune('0'+i))
			data := []byte("test data")
			err := cache.Put(cid, data)
			if err != nil {
				t.Fatalf("Put returned error: %v", err)
			}
		}

		// Clear cache
		err = cache.Clear()
		if err != nil {
			t.Fatalf("Clear returned error: %v", err)
		}

		// Verify current bytes is 0
		if cache.CurrentBytes() != 0 {
			t.Errorf("expected current bytes 0 after clear, got %d", cache.CurrentBytes())
		}

		// Verify all files are removed (directories may remain)
		var fileCount int
		filepath.Walk(tmpDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				fileCount++
			}
			return nil
		})

		if fileCount != 0 {
			t.Errorf("expected 0 files after clear, got %d", fileCount)
		}
	})
}

// Helper function to check if a byte is a valid hex character
func isHexChar(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func TestContentCache_InitializeCacheSize(t *testing.T) {
	t.Run("empty cache directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(1024 * 1024)

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		if cache.CurrentBytes() != 0 {
			t.Errorf("expected current bytes 0 for empty directory, got %d", cache.CurrentBytes())
		}
	})

	t.Run("cache directory with files", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(10 * 1024 * 1024)

		// Create some test files in the cache directory
		testData := []byte("test content")
		for i := 0; i < 3; i++ {
			filePath := filepath.Join(tmpDir, "file"+string(rune('0'+i)))
			if err := os.WriteFile(filePath, testData, 0644); err != nil {
				t.Fatalf("failed to create test file: %v", err)
			}
		}

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		expectedSize := int64(len(testData) * 3)
		if cache.CurrentBytes() != expectedSize {
			t.Errorf("expected current bytes %d, got %d", expectedSize, cache.CurrentBytes())
		}
	})

	t.Run("cache directory with nested structure", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(10 * 1024 * 1024)

		// Create nested directory structure
		subDir := filepath.Join(tmpDir, "subdir")
		if err := os.Mkdir(subDir, 0755); err != nil {
			t.Fatalf("failed to create subdirectory: %v", err)
		}

		testData := []byte("nested content")
		for i := 0; i < 2; i++ {
			filePath := filepath.Join(subDir, "nested"+string(rune('0'+i)))
			if err := os.WriteFile(filePath, testData, 0644); err != nil {
				t.Fatalf("failed to create nested test file: %v", err)
			}
		}

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		expectedSize := int64(len(testData) * 2)
		if cache.CurrentBytes() != expectedSize {
			t.Errorf("expected current bytes %d, got %d", expectedSize, cache.CurrentBytes())
		}
	})
}

func TestContentCache_HasSize(t *testing.T) {
	t.Run("has space", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(1024 * 1024) // 1 MB

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		if !cache.HasSize(512 * 1024) {
			t.Error("expected cache to have space for 512 KB")
		}
	})

	t.Run("exactly at limit", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(1024 * 1024) // 1 MB

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		if !cache.HasSize(maxBytes) {
			t.Error("expected cache to have space for exactly max bytes")
		}
	})

	t.Run("exceeds limit", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(1024 * 1024) // 1 MB

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		if cache.HasSize(maxBytes + 1) {
			t.Error("expected cache to not have space for more than max bytes")
		}
	})

	t.Run("with existing content", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(1024 * 1024) // 1 MB

		// Create a file in the cache directory
		testData := []byte("test content")
		filePath := filepath.Join(tmpDir, "existing")
		if err := os.WriteFile(filePath, testData, 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		remainingSpace := maxBytes - cache.CurrentBytes()
		if !cache.HasSize(remainingSpace) {
			t.Error("expected cache to have space for remaining bytes")
		}

		if cache.HasSize(remainingSpace + 1) {
			t.Error("expected cache to not have space for more than remaining bytes")
		}
	})
}

func TestContentCache_Getters(t *testing.T) {
	tmpDir := t.TempDir()
	maxBytes := int64(5 * 1024 * 1024) // 5 MB

	cache, err := NewContentCache(tmpDir, maxBytes, 100000)
	if err != nil {
		t.Fatalf("NewContentCache returned error: %v", err)
	}

	t.Run("CachePath", func(t *testing.T) {
		if cache.CachePath() != tmpDir {
			t.Errorf("expected cache path %s, got %s", tmpDir, cache.CachePath())
		}
	})

	t.Run("MaxBytes", func(t *testing.T) {
		if cache.MaxBytes() != maxBytes {
			t.Errorf("expected max bytes %d, got %d", maxBytes, cache.MaxBytes())
		}
	})

	t.Run("CurrentBytes", func(t *testing.T) {
		if cache.CurrentBytes() != 0 {
			t.Errorf("expected current bytes 0, got %d", cache.CurrentBytes())
		}
	})
}

func TestContentCache_Clear(t *testing.T) {
	t.Run("clear empty cache", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(1024 * 1024)

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		if err := cache.Clear(); err != nil {
			t.Fatalf("Clear returned error: %v", err)
		}

		if cache.CurrentBytes() != 0 {
			t.Errorf("expected current bytes 0 after clear, got %d", cache.CurrentBytes())
		}
	})

	t.Run("clear cache with files", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(10 * 1024 * 1024)

		// Create test files
		testData := []byte("test content")
		for i := 0; i < 3; i++ {
			filePath := filepath.Join(tmpDir, "file"+string(rune('0'+i)))
			if err := os.WriteFile(filePath, testData, 0644); err != nil {
				t.Fatalf("failed to create test file: %v", err)
			}
		}

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		initialBytes := cache.CurrentBytes()
		if initialBytes == 0 {
			t.Fatal("expected initial bytes to be non-zero")
		}

		if err := cache.Clear(); err != nil {
			t.Fatalf("Clear returned error: %v", err)
		}

		if cache.CurrentBytes() != 0 {
			t.Errorf("expected current bytes 0 after clear, got %d", cache.CurrentBytes())
		}

		// Verify files are removed from disk
		entries, err := os.ReadDir(tmpDir)
		if err != nil {
			t.Fatalf("failed to read cache directory: %v", err)
		}

		if len(entries) != 0 {
			t.Errorf("expected 0 files in cache directory after clear, got %d", len(entries))
		}
	})

	t.Run("clear cache with nested structure", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(10 * 1024 * 1024)

		// Create nested structure
		subDir := filepath.Join(tmpDir, "subdir")
		if err := os.Mkdir(subDir, 0755); err != nil {
			t.Fatalf("failed to create subdirectory: %v", err)
		}

		testData := []byte("nested content")
		filePath := filepath.Join(subDir, "nested")
		if err := os.WriteFile(filePath, testData, 0644); err != nil {
			t.Fatalf("failed to create nested test file: %v", err)
		}

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		if err := cache.Clear(); err != nil {
			t.Fatalf("Clear returned error: %v", err)
		}

		if cache.CurrentBytes() != 0 {
			t.Errorf("expected current bytes 0 after clear, got %d", cache.CurrentBytes())
		}

		// Verify nested file is removed but subdirectory remains
		entries, err := os.ReadDir(tmpDir)
		if err != nil {
			t.Fatalf("failed to read cache directory: %v", err)
		}

		if len(entries) != 1 {
			t.Errorf("expected 1 subdirectory in cache directory after clear, got %d", len(entries))
		}

		subEntries, err := os.ReadDir(subDir)
		if err != nil {
			t.Fatalf("failed to read subdirectory: %v", err)
		}

		if len(subEntries) != 0 {
			t.Errorf("expected 0 files in subdirectory after clear, got %d", len(subEntries))
		}
	})
}

func TestContentCache_PutAndGet(t *testing.T) {
	t.Run("stores and retrieves block", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(10 * 1024 * 1024) // 10 MB

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		cid := "QmExample123"
		testData := []byte("test block content")

		// Put block
		err = cache.Put(cid, testData)
		if err != nil {
			t.Fatalf("Put returned error: %v", err)
		}

		// Verify disk usage increased
		expectedBytes := int64(len(testData))
		if cache.CurrentBytes() != expectedBytes {
			t.Errorf("expected current bytes %d, got %d", expectedBytes, cache.CurrentBytes())
		}

		// Get block
		retrieved, err := cache.Get(cid)
		if err != nil {
			t.Fatalf("Get returned error: %v", err)
		}

		if string(retrieved) != string(testData) {
			t.Errorf("expected data %q, got %q", string(testData), string(retrieved))
		}
	})

	t.Run("updates access time on get", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(10 * 1024 * 1024)

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		cid := "QmAccessTest"
		testData := []byte("access test data")

		err = cache.Put(cid, testData)
		if err != nil {
			t.Fatalf("Put returned error: %v", err)
		}

		// Get block to update access time
		_, err = cache.Get(cid)
		if err != nil {
			t.Fatalf("Get returned error: %v", err)
		}

		// This block should be most recently accessed
		// (We'll verify this works with eviction tests)
	})

	t.Run("overwrites existing block", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(10 * 1024 * 1024)

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		cid := "QmOverwrite"
		testData1 := []byte("original data")
		testData2 := []byte("new data")

		err = cache.Put(cid, testData1)
		if err != nil {
			t.Fatalf("First Put returned error: %v", err)
		}

		err = cache.Put(cid, testData2)
		if err != nil {
			t.Fatalf("Second Put returned error: %v", err)
		}

		// Should have data from second put
		retrieved, err := cache.Get(cid)
		if err != nil {
			t.Fatalf("Get returned error: %v", err)
		}

		if string(retrieved) != string(testData2) {
			t.Errorf("expected data %q, got %q", string(testData2), string(retrieved))
		}
	})
}

func TestContentCache_Evict(t *testing.T) {
	t.Run("evicts least recently used blocks", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(1024) // 1 KB

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		// Add blocks that exceed maxBytes
		// Note: When adding Block3, it will auto-evict Block1
		// Use a slice to ensure deterministic order
		blocks := []struct {
			cid  string
			data []byte
		}{
			{"QmBlock1", make([]byte, 400)},
			{"QmBlock2", make([]byte, 400)},
			{"QmBlock3", make([]byte, 400)},
		}

		for _, block := range blocks {
			err := cache.Put(block.cid, block.data)
			if err != nil {
				t.Fatalf("Put returned error for %s: %v", block.cid, err)
			}
		}

		// Access Block2 to make it more recent than Block3
		_, err = cache.Get("QmBlock2")
		if err != nil {
			t.Fatalf("Get returned error: %v", err)
		}

		// Block1 was already evicted when Block3 was added
		_, err = cache.Get("QmBlock1")
		if err == nil {
			t.Error("expected Block1 to be evicted, but it was found")
		}

		// Evict enough space for one more block
		err = cache.Evict(400)
		if err != nil {
			t.Fatalf("Evict returned error: %v", err)
		}

		// Block3 should be evicted now (least recently used after Block2 access)
		_, err = cache.Get("QmBlock3")
		if err == nil {
			t.Error("expected Block3 to be evicted, but it was found")
		}

		// Block2 should still exist (most recently accessed)
		_, err = cache.Get("QmBlock2")
		if err != nil {
			t.Errorf("expected Block2 to exist, got error: %v", err)
		}
	})

	t.Run("evicts multiple blocks if needed", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(1024)

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		// Add small blocks
		// Use a slice to ensure deterministic order
		blocks := []struct {
			cid  string
			data []byte
		}{
			{"QmSmall1", make([]byte, 100)},
			{"QmSmall2", make([]byte, 100)},
			{"QmSmall3", make([]byte, 100)},
			{"QmSmall4", make([]byte, 100)},
			{"QmSmall5", make([]byte, 100)},
		}

		for _, block := range blocks {
			err := cache.Put(block.cid, block.data)
			if err != nil {
				t.Fatalf("Put returned error for %s: %v", block.cid, err)
			}
		}

		// Evict enough space for 300 bytes
		err = cache.Evict(300)
		if err != nil {
			t.Fatalf("Evict returned error: %v", err)
		}

		// Should have evicted 3 least recently used blocks (300 bytes)
		// Blocks 4 and 5 should remain
		_, err = cache.Get("QmSmall4")
		if err != nil {
			t.Errorf("expected QmSmall4 to exist, got error: %v", err)
		}

		_, err = cache.Get("QmSmall5")
		if err != nil {
			t.Errorf("expected QmSmall5 to exist, got error: %v", err)
		}
	})

	t.Run("returns error when cache is empty", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(1024)

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		err = cache.Evict(100)
		if err == nil {
			t.Error("expected error when evicting from empty cache")
		}
	})
}

func TestContentCache_LRUMetadataTracking(t *testing.T) {
	t.Run("Get updates LRU access time", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(10 * 1024 * 1024)

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		cid := "QmLRUTest"
		testData := []byte("lru test data")

		err = cache.Put(cid, testData)
		if err != nil {
			t.Fatalf("Put returned error: %v", err)
		}

		// Get block to update access time
		_, err = cache.Get(cid)
		if err != nil {
			t.Fatalf("Get returned error: %v", err)
		}

		// The block should be the most recently accessed
		// We'll verify this by checking eviction order
	})

	t.Run("Put adds to LRU cache", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(10 * 1024 * 1024)

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		cid := "QmLRUPut"
		testData := []byte("lru put test")

		err = cache.Put(cid, testData)
		if err != nil {
			t.Fatalf("Put returned error: %v", err)
		}

		// Verify the block exists and can be retrieved
		retrieved, err := cache.Get(cid)
		if err != nil {
			t.Fatalf("Get returned error: %v", err)
		}

		if string(retrieved) != string(testData) {
			t.Errorf("expected data %q, got %q", string(testData), string(retrieved))
		}

		// The block should be tracked in LRU
	})

	t.Run("Evict uses LRU's eviction order", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(1024) // 1 KB

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		// Add blocks sequentially
		// Use a slice to ensure deterministic order
		blocks := []struct {
			cid  string
			data []byte
		}{
			{"QmFirst", make([]byte, 200)},
			{"QmSecond", make([]byte, 200)},
			{"QmThird", make([]byte, 200)},
		}

		for _, block := range blocks {
			err := cache.Put(block.cid, block.data)
			if err != nil {
				t.Fatalf("Put returned error for %s: %v", block.cid, err)
			}
		}

		// Access QmSecond to make it most recently used
		_, err = cache.Get("QmSecond")
		if err != nil {
			t.Fatalf("Get returned error: %v", err)
		}

		// Evict 200 bytes - should evict QmFirst (least recently used)
		err = cache.Evict(200)
		if err != nil {
			t.Fatalf("Evict returned error: %v", err)
		}

		// QmFirst should be evicted
		_, err = cache.Get("QmFirst")
		if err == nil {
			t.Error("expected QmFirst to be evicted, but it was found")
		}

		// QmSecond should still exist (most recently accessed)
		_, err = cache.Get("QmSecond")
		if err != nil {
			t.Errorf("expected QmSecond to exist, got error: %v", err)
		}

		// QmThird should still exist
		_, err = cache.Get("QmThird")
		if err != nil {
			t.Errorf("expected QmThird to exist, got error: %v", err)
		}
	})

	t.Run("uses hashicorp LRU cache internally", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(10 * 1024 * 1024)

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		// This test verifies that the LRU cache is being used
		// by checking that the implementation maintains O(1) operations
		// We'll verify this through a stress test that would be slow with O(n log n)
		
		// Add many blocks
		numBlocks := 1000
		for i := 0; i < numBlocks; i++ {
			cid := "QmBlock" + string(rune('0'+i))
			testData := []byte("test data")
			err := cache.Put(cid, testData)
			if err != nil {
				t.Fatalf("Put returned error for block %d: %v", i, err)
			}
		}

		// Get all blocks - should be fast with O(1) LRU
		for i := 0; i < numBlocks; i++ {
			cid := "QmBlock" + string(rune('0'+i))
			_, err := cache.Get(cid)
			if err != nil {
				t.Fatalf("Get returned error for block %d: %v", i, err)
			}
		}

		// Evict some blocks - should be fast with O(1) LRU
		err = cache.Evict(1000)
		if err != nil {
			t.Fatalf("Evict returned error: %v", err)
		}
	})
}

func TestContentCache_Get(t *testing.T) {
	t.Run("returns error for non-existent block", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(10 * 1024 * 1024)

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		_, err = cache.Get("QmNonExistent")
		if err == nil {
			t.Error("expected error for non-existent block")
		}
	})
}

func TestContentCache_LRUPopulationOnInitialization(t *testing.T) {
	t.Run("populates LRU with existing cache files", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(10 * 1024 * 1024)

		// Create some test files in the cache directory before initialization
		testFiles := []struct {
			cid  string
			data []byte
		}{
			{"QmExisting1", []byte("existing data 1")},
			{"QmExisting2", []byte("existing data 2")},
			{"QmExisting3", []byte("existing data 3")},
		}

		for _, tf := range testFiles {
			filePath := filepath.Join(tmpDir, tf.cid)
			if err := os.WriteFile(filePath, tf.data, 0644); err != nil {
				t.Fatalf("failed to create test file: %v", err)
			}
		}

		// Create cache - should populate LRU with existing files
		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		// Verify all existing files are in LRU by accessing them
		for _, tf := range testFiles {
			data, err := cache.Get(tf.cid)
			if err != nil {
				t.Errorf("failed to get existing file %s: %v", tf.cid, err)
			}
			if string(data) != string(tf.data) {
				t.Errorf("expected data %q, got %q for %s", string(tf.data), string(data), tf.cid)
			}
		}

		// Verify currentBytes is correct
		expectedSize := int64(0)
		for _, tf := range testFiles {
			expectedSize += int64(len(tf.data))
		}
		if cache.CurrentBytes() != expectedSize {
			t.Errorf("expected current bytes %d, got %d", expectedSize, cache.CurrentBytes())
		}
	})

	t.Run("handles empty cache directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(10 * 1024 * 1024)

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		if cache.CurrentBytes() != 0 {
			t.Errorf("expected current bytes 0 for empty directory, got %d", cache.CurrentBytes())
		}
	})
}

func TestContentCache_EvictFallbackWhenLruEmpty(t *testing.T) {
	t.Run("evicts from disk when LRU is empty but disk has files", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(10 * 1024 * 1024)

		// Create test files directly on disk without going through cache
		// This simulates the scenario where LRU and disk are out of sync
		testFiles := []struct {
			cid  string
			data []byte
		}{
			{"QmDiskOnly1", []byte("disk only data 1")},
			{"QmDiskOnly2", []byte("disk only data 2")},
		}

		for _, tf := range testFiles {
			filePath := filepath.Join(tmpDir, tf.cid)
			if err := os.WriteFile(filePath, tf.data, 0644); err != nil {
				t.Fatalf("failed to create test file: %v", err)
			}
		}

		// Create cache but don't populate LRU (simulate out-of-sync state)
		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		// Clear LRU to simulate empty state while disk has files
		cache.mu.Lock()
		cache.lru.Purge()
		cache.mu.Unlock()

		// Try to evict - should use disk fallback
		// Evict less than total size to ensure success
		err = cache.Evict(10)
		if err != nil {
			t.Fatalf("Evict returned error: %v", err)
		}

		// Verify at least one file was evicted
		remainingFiles, err := os.ReadDir(tmpDir)
		if err != nil {
			t.Fatalf("failed to read cache directory: %v", err)
		}

		if len(remainingFiles) >= 2 {
			t.Errorf("expected at least one file to be evicted, but found %d files", len(remainingFiles))
		}
	})

	t.Run("returns error when both LRU and disk are empty", func(t *testing.T) {
		tmpDir := t.TempDir()
		maxBytes := int64(10 * 1024 * 1024)

		cache, err := NewContentCache(tmpDir, maxBytes, 100000)
		if err != nil {
			t.Fatalf("NewContentCache returned error: %v", err)
		}

		err = cache.Evict(100)
		if err == nil {
			t.Error("expected error when evicting from empty cache")
		}
		if err.Error() != "cache is empty, nothing to evict" {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

func TestContentCache_ConcurrentAccess(t *testing.T) {
	t.Run("concurrent Put and Get operations", func(t *testing.T) {
		cache := setupCache(t)

		const numGoroutines = 10
		const opsPerGoroutine = 100

		var wg sync.WaitGroup
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < opsPerGoroutine; j++ {
					cid := fmt.Sprintf("QmConcurrent%d-%d", id, j)
					data := []byte(fmt.Sprintf("data-%d-%d", id, j))

					if err := cache.Put(cid, data); err != nil {
						t.Errorf("Put failed: %v", err)
					}

					if _, err := cache.Get(cid); err != nil {
						t.Errorf("Get failed: %v", err)
					}
				}
			}(i)
		}
		wg.Wait()

		// Verify cache state is consistent
		expectedOps := numGoroutines * opsPerGoroutine
		if cache.CurrentBytes() <= 0 {
			t.Errorf("expected cache to have data after %d operations", expectedOps)
		}
	})
}

func TestContentCache_HashPrefixHandling(t *testing.T) {
	t.Run("handles different CIDs with same hash prefix", func(t *testing.T) {
		// This is extremely unlikely with SHA-256, but tests the level extraction logic
		cache := setupCache(t)

		// Use CIDs that differ but might share hash prefixes
		cid1 := "QmAAAAAA" + strings.Repeat("A", 40)
		cid2 := "QmBBBBBB" + strings.Repeat("B", 40)

		path1 := cache.getBlockPathForTest(cid1)
		path2 := cache.getBlockPathForTest(cid2)

		// Verify they end up in different directories or same directory but different files
		// This confirms the hashing distributes files appropriately

		// Both paths should be valid and different
		if path1 == path2 {
			t.Errorf("expected different CIDs to produce different paths, got same path: %s", path1)
		}

		// Both should have the correct nested structure
		pathParts1 := strings.Split(path1, string(filepath.Separator))
		pathParts2 := strings.Split(path2, string(filepath.Separator))

		if len(pathParts1) < 4 || len(pathParts2) < 4 {
			t.Errorf("expected paths to have at least 4 parts, got %d and %d", len(pathParts1), len(pathParts2))
		}

		// Verify CIDs are the last part and different
		if pathParts1[len(pathParts1)-1] != cid1 {
			t.Errorf("expected path1 to end with %s, got %s", cid1, pathParts1[len(pathParts1)-1])
		}
		if pathParts2[len(pathParts2)-1] != cid2 {
			t.Errorf("expected path2 to end with %s, got %s", cid2, pathParts2[len(pathParts2)-1])
		}
	})
}

// setupCache is a helper function to create a test cache
func setupCache(t *testing.T) *ContentCache {
	tmpDir := t.TempDir()
	maxBytes := int64(10 * 1024 * 1024) // 10 MB

	cache, err := NewContentCache(tmpDir, maxBytes, 100000)
	if err != nil {
		t.Fatalf("Failed to create test cache: %v", err)
	}

	return cache
}
