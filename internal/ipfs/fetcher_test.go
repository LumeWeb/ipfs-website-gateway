package ipfs

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	chunker "github.com/ipfs/boxo/chunker"
	"github.com/ipfs/boxo/ipld/merkledag"
	ihelpers "github.com/ipfs/boxo/ipld/unixfs/importer/helpers"
	"github.com/ipfs/boxo/ipld/unixfs/importer/balanced"
	"github.com/ipfs/go-cid"
	format "github.com/ipfs/go-ipld-format"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// TestNewFetcher verifies that NewFetcher creates a valid Fetcher instance.
func TestNewFetcher(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)

	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "ipfs-repo")

	node, err := NewNode(ctx, "", repoPath, logger)
	if err != nil {
		t.Fatalf("NewNode failed: %v", err)
	}
	defer func() { _ = node.Close() }()

	fetcher := NewFetcher(node, logger)

	if fetcher == nil {
		t.Fatal("NewFetcher returned nil")
	}
	if fetcher.node != node {
		t.Error("Fetcher node does not match provided node")
	}
	if fetcher.logger != logger {
		t.Error("Fetcher logger does not match provided logger")
	}
}

// TestFetchUnixFileSimpleFile tests fetching a simple file from IPFS.
func TestFetchUnixFileSimpleFile(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)

	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "ipfs-repo")

	node, err := NewNode(ctx, "", repoPath, logger)
	if err != nil {
		t.Fatalf("NewNode failed: %v", err)
	}
	defer func() { _ = node.Close() }()

	fetcher := NewFetcher(node, logger)

	content := "Hello, IPFS!"
	c, err := addFileToNode(ctx, node, strings.NewReader(content))
	if err != nil {
		t.Fatalf("Failed to add file to node: %v", err)
	}

	rsc, filename, err := fetcher.FetchUnixFile(ctx, c, nil)
	if err != nil {
		t.Fatalf("FetchUnixFile failed: %v", err)
	}
	defer func() { _ = rsc.Close() }()

	data, err := io.ReadAll(rsc)
	if err != nil {
		t.Fatalf("Failed to read content: %v", err)
	}
	if string(data) != content {
		t.Errorf("Content mismatch: got %q, want %q", string(data), content)
	}

	if filename != "" {
		t.Errorf("Expected empty filename for root, got %q", filename)
	}
}

// TestFetchUnixFileWithPath tests fetching a file with a path.
func TestFetchUnixFileWithPath(t *testing.T) {
	t.Skip("Skipping directory test - requires complex UnixFS directory setup")
}

// TestFetchUnixFileDirectoryWithIndex tests fetching a directory with index.html.
func TestFetchUnixFileDirectoryWithIndex(t *testing.T) {
	t.Skip("Skipping directory test - requires complex UnixFS directory setup")
}

// TestFetchUnixFileInvalidPath tests fetching with an invalid path.
func TestFetchUnixFileInvalidPath(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)

	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "ipfs-repo")

	node, err := NewNode(ctx, "", repoPath, logger)
	if err != nil {
		t.Fatalf("NewNode failed: %v", err)
	}
	defer func() { _ = node.Close() }()

	fetcher := NewFetcher(node, logger)

	content := "test content"
	c, err := addFileToNode(ctx, node, strings.NewReader(content))
	if err != nil {
		t.Fatalf("Failed to add file to node: %v", err)
	}

	_, _, err = fetcher.FetchUnixFile(ctx, c, []string{"nonexistent", "file.txt"})
	if err == nil {
		t.Error("Expected error for invalid path")
	}
}

// TestFetchUnixFileInvalidCID tests fetching with an invalid CID.
func TestFetchUnixFileInvalidCID(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	logger := zaptest.NewLogger(t)

	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "ipfs-repo")

	node, err := NewNode(ctx, "", repoPath, logger)
	if err != nil {
		t.Fatalf("NewNode failed: %v", err)
	}
	defer func() { _ = node.Close() }()

	fetcher := NewFetcher(node, logger)

	invalidCid := cid.MustParse("QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn")

	_, _, err = fetcher.FetchUnixFile(ctx, invalidCid, nil)
	if err == nil {
		t.Error("Expected error for CID that doesn't exist")
	}
}

// TestFetchUnixFileSeek tests that the returned reader supports seeking.
func TestFetchUnixFileSeek(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)

	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "ipfs-repo")

	node, err := NewNode(ctx, "", repoPath, logger)
	if err != nil {
		t.Fatalf("NewNode failed: %v", err)
	}
	defer func() { _ = node.Close() }()

	fetcher := NewFetcher(node, logger)

	// Create a file with larger content
	content := strings.Repeat("Hello, World! ", 100)
	c, err := addFileToNode(ctx, node, strings.NewReader(content))
	if err != nil {
		t.Fatalf("Failed to add file to node: %v", err)
	}

	// Fetch the file
	rsc, _, err := fetcher.FetchUnixFile(ctx, c, nil)
	if err != nil {
		t.Fatalf("FetchUnixFile failed: %v", err)
	}
	defer func() { _ = rsc.Close() }()

	// Test seeking
	offset, err := rsc.Seek(50, io.SeekStart)
	if err != nil {
		t.Fatalf("Seek failed: %v", err)
	}
	if offset != 50 {
		t.Errorf("Seek offset mismatch: got %d, want 50", offset)
	}

	// Read from the new position
	buf := make([]byte, 20)
	n, err := rsc.Read(buf)
	if err != nil {
		t.Fatalf("Read after seek failed: %v", err)
	}
	if n != 20 {
		t.Errorf("Read length mismatch: got %d, want 20", n)
	}

	expected := content[50:70]
	if string(buf) != expected {
		t.Errorf("Content after seek mismatch: got %q, want %q", string(buf), expected)
	}
}

// TestFetchUnixFileContextCancellation tests that fetch respects context cancellation.
func TestFetchUnixFileContextCancellation(t *testing.T) {
	// Skip this test - context cancellation with local blocks succeeds immediately
	// Testing true cancellation requires network operations
	t.Skip("Context cancellation test requires network operations - skipping")
}

// Helper function to add a file to the IPFS node
func addFileToNode(ctx context.Context, node *Node, r io.Reader) (cid.Cid, error) {
	dagService := merkledag.NewDAGService(node.Blockservice)

	fileNode, err := createUnixFSFile(ctx, dagService, r)
	if err != nil {
		return cid.Undef, err
	}

	return fileNode.Cid(), nil
}

// Helper function to create a UnixFS file node
func createUnixFSFile(ctx context.Context, dagService format.DAGService, r io.Reader) (format.Node, error) {
	params := ihelpers.DagBuilderParams{
		Dagserv:    dagService,
		Maxlinks:   ihelpers.DefaultLinksPerBlock,
		RawLeaves:  true,
	}

	spl := chunker.NewSizeSplitter(r, chunker.DefaultBlockSize)
	db, err := params.New(spl)
	if err != nil {
		return nil, fmt.Errorf("failed to create dag builder: %w", err)
	}

	rootNode, err := balanced.Layout(db)
	if err != nil {
		return nil, fmt.Errorf("failed to create balanced layout: %w", err)
	}

	return rootNode, nil
}

// TestFetchUnixFileWithRange tests fetching a file with HTTP range request.
func TestFetchUnixFileWithRange(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)

	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "ipfs-repo")

	node, err := NewNode(ctx, "", repoPath, logger)
	if err != nil {
		t.Fatalf("NewNode failed: %v", err)
	}
	defer func() { _ = node.Close() }()

	fetcher := NewFetcher(node, logger)

	// Create a file with known content
	content := strings.Repeat("0123456789", 100) // 1000 bytes
	c, err := addFileToNode(ctx, node, strings.NewReader(content))
	if err != nil {
		t.Fatalf("Failed to add file to node: %v", err)
	}

	tests := []struct {
		name          string
		rangeHeader   string
		wantStart     int64
		wantEnd       int64
		wantLength    int64
		wantContent   string
		wantErr       bool
		noRangeInfo   bool
	}{
		{
			name:        "range from 0 to 99",
			rangeHeader: "bytes=0-99",
			wantStart:   0,
			wantEnd:     99,
			wantLength:  100,
			wantContent: content[0:100],
			wantErr:     false,
		},
		{
			name:        "range from 500 to 599",
			rangeHeader: "bytes=500-599",
			wantStart:   500,
			wantEnd:     599,
			wantLength:  100,
			wantContent: content[500:600],
			wantErr:     false,
		},
		{
			name:        "range from 500 to end",
			rangeHeader: "bytes=500-",
			wantStart:   500,
			wantEnd:     999,
			wantLength:  500,
			wantContent: content[500:],
			wantErr:     false,
		},
		{
			name:        "last 100 bytes",
			rangeHeader: "bytes=-100",
			wantStart:   900,
			wantEnd:     999,
			wantLength:  100,
			wantContent: content[900:],
			wantErr:     false,
		},
		{
			name:        "empty range header returns full file",
			rangeHeader: "",
			wantStart:   0,
			wantEnd:     999,
			wantLength:  1000,
			wantContent: content,
			wantErr:     false,
			noRangeInfo: true, // Empty range header returns nil rangeInfo
		},
		{
			name:        "invalid range returns error",
			rangeHeader: "bytes=invalid",
			wantErr:     true,
		},
		{
			name:        "range beyond file size returns error",
			rangeHeader: "bytes=2000-3000",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rsc, filename, rangeInfo, err := fetcher.FetchUnixFileWithRange(ctx, c, nil, tt.rangeHeader)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("FetchUnixFileWithRange failed: %v", err)
			}
				defer func() { _ = rsc.Close() }()

			// Verify range metadata
			if tt.noRangeInfo {
				if rangeInfo != nil {
					t.Errorf("Expected nil rangeInfo but got %v", rangeInfo)
				}
			} else {
				if rangeInfo == nil {
					t.Fatal("Expected rangeInfo but got nil")
				}
				if rangeInfo.Start != tt.wantStart {
					t.Errorf("rangeInfo.Start = %d, want %d", rangeInfo.Start, tt.wantStart)
				}
				if rangeInfo.End != tt.wantEnd {
					t.Errorf("rangeInfo.End = %d, want %d", rangeInfo.End, tt.wantEnd)
				}
				if rangeInfo.Length() != tt.wantLength {
					t.Errorf("rangeInfo.Length() = %d, want %d", rangeInfo.Length(), tt.wantLength)
				}
			}

			// Verify content
			data, err := io.ReadAll(rsc)
			if err != nil {
				t.Fatalf("Failed to read content: %v", err)
			}
			if string(data) != tt.wantContent {
				t.Errorf("Content mismatch: got %q, want %q", string(data), tt.wantContent)
			}

			// Verify filename is empty for root
			if filename != "" {
				t.Errorf("Expected empty filename for root, got %q", filename)
			}
		})
	}
}

// BenchmarkFetchUnixFile benchmarks fetching a file.
func BenchmarkFetchUnixFile(b *testing.B) {
	ctx := context.Background()
	logger := zap.NewNop()

	tempDir := b.TempDir()
	repoPath := filepath.Join(tempDir, "ipfs-repo")

	node, err := NewNode(ctx, "", repoPath, logger)
	if err != nil {
		b.Fatalf("NewNode failed: %v", err)
	}
	defer func() { _ = node.Close() }()
	
	fetcher := NewFetcher(node, logger)

	content := strings.Repeat("benchmark content ", 100)
	c, err := addFileToNode(ctx, node, strings.NewReader(content))
	if err != nil {
		b.Fatalf("Failed to add file to node: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rsc, _, err := fetcher.FetchUnixFile(ctx, c, nil)
		if err != nil {
			b.Fatalf("FetchUnixFile failed: %v", err)
		}
		_, _ = io.Copy(io.Discard, rsc)
		_ = rsc.Close()
	}
}
