package ipfs

import (
	"context"
	"fmt"
	"io"

	"github.com/ipfs/boxo/ipld/merkledag"
	fsio "github.com/ipfs/boxo/ipld/unixfs/io"
	"github.com/ipfs/go-cid"
	format "github.com/ipfs/go-ipld-format"
	"go.uber.org/zap"
)

// Fetcher handles UnixFS content retrieval from IPFS.
// It provides methods to fetch files and directories from IPFS,
// with support for path resolution and index.html fallback for directories.
type Fetcher struct {
	node   *Node
	logger *zap.Logger
}

// NewFetcher creates a new Fetcher instance with the provided IPFS node and logger.
//
// The node parameter must be a valid Node instance with an initialized Blockservice.
// The logger is used for logging fetch operations and errors.
func NewFetcher(node *Node, logger *zap.Logger) *Fetcher {
	return &Fetcher{
		node:   node,
		logger: logger,
	}
}

// FetchUnixFile retrieves a UnixFS file from IPFS and returns a reader for the content.
//
// The method resolves the CID and optionally traverses the UnixFS path to locate
// the desired file. If the path resolves to a directory, it attempts to serve
// index.html as a fallback (useful for single-page webapps).
//
// Parameters:
//   - ctx: Context for the operation, used for cancellation and timeouts
//   - c: The content identifier (CID) of the IPFS content
//   - path: A slice of path components to traverse within the UnixFS structure
//
// Returns:
//   - io.ReadSeekCloser: A readable, seekable, and closeable stream of the file content
//   - string: The filename to use for Content-Disposition header
//   - error: An error if the content cannot be fetched or resolved
func (f *Fetcher) FetchUnixFile(ctx context.Context, c cid.Cid, path []string) (io.ReadSeekCloser, string, error) {
	rsc, filename, _, err := f.FetchUnixFileWithRange(ctx, c, path, "")
	return rsc, filename, err
}

// FetchUnixFileWithRange retrieves a UnixFS file from IPFS with support for HTTP range requests.
//
// This method extends FetchUnixFile by accepting an optional HTTP Range header.
// If a valid range is provided, the reader is positioned at the start of the range
// and will return only the requested bytes.
//
// Parameters:
//   - ctx: Context for the operation, used for cancellation and timeouts
//   - c: The content identifier (CID) of the IPFS content
//   - path: A slice of path components to traverse within the UnixFS structure
//   - rangeHeader: An optional HTTP Range header string (e.g., "bytes=0-1023")
//
// Returns:
//   - io.ReadSeekCloser: A readable, seekable, and closeable stream of the file content
//   - string: The filename to use for Content-Disposition header
//   - *HTTPRange: The validated range information, or nil if no range was requested
//   - error: An error if the content cannot be fetched or the range is invalid
func (f *Fetcher) FetchUnixFileWithRange(ctx context.Context, c cid.Cid, path []string, rangeHeader string) (io.ReadSeekCloser, string, *HTTPRange, error) {
	// Parse the range header if provided
	parsedRange, err := ParseRangeHeader(rangeHeader)
	if err != nil {
		return nil, "", nil, fmt.Errorf("invalid range header: %w", err)
	}

	// Create a DAG service from the blockservice
	dagService := merkledag.NewDAGService(f.node.Blockservice)
	dagSess := merkledag.NewSession(ctx, dagService)

	// Fetch the root node for the CID
	rootNode, err := dagSess.Get(ctx, c)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to get root node %s: %w", c, err)
	}

	// Helper function to serve index.html from a directory node
	serveIndex := func(node format.Node) (io.ReadSeekCloser, string, error) {
		f.logger.Debug("serving directory index.html")
		dir, err := fsio.NewDirectoryFromNode(dagService, node)
		if err != nil {
			return nil, "", fmt.Errorf("failed to create directory from node: %w", err)
		}
		index, err := dir.Find(ctx, "index.html")
		if err != nil {
			return nil, "", fmt.Errorf("failed to find index.html: %w", err)
		}
		dr, err := fsio.NewDagReader(ctx, index, dagSess)
		return dr, "index.html", err
	}

	// Traverse the UnixFS path if path components are provided
	var child format.Node
	if len(path) > 0 {
		traversed, err := f.traverseUnixFSPath(ctx, dagService, rootNode, path)
		if err != nil {
			// If path resolution fails, attempt to serve index.html from root
			f.logger.Debug("path resolution failed, attempting index.html fallback",
				zap.Strings("path", path),
				zap.Error(err),
			)
			rsc, filename, err := serveIndex(rootNode)
			return rsc, filename, nil, err
		}
		child = traversed
	} else {
		child = rootNode
	}

	// Check if the resolved node is a directory
	if _, err := fsio.NewDirectoryFromNode(dagService, child); err == nil {
		// Serve index.html from the directory
		rsc, filename, err := serveIndex(child)
		return rsc, filename, nil, err
	}

	// The node is a file, create a reader for it
	dr, err := fsio.NewDagReader(ctx, child, dagSess)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to create dag reader: %w", err)
	}

	// Get file size for range validation
	fileSize := int64(dr.Size())

	// Validate and normalize the range against the file size
	normalizedRange, err := parsedRange.Validate(fileSize)
	if err != nil {
		_ = dr.Close()
		return nil, "", nil, fmt.Errorf("invalid range: %w", err)
	}

	// If a range was requested, seek to the start position
	var reader io.ReadSeekCloser = dr
	if normalizedRange != nil {
		_, err = dr.Seek(normalizedRange.Start, io.SeekStart)
		if err != nil {
			_ = dr.Close()
			return nil, "", nil, fmt.Errorf("failed to seek to range start: %w", err)
		}
		// Wrap the reader to limit reads to the range length
		reader = newRangeReader(dr, normalizedRange.Length())
	}

	// Determine filename from path
	filename := ""
	if len(path) > 0 {
		filename = path[len(path)-1]
	}

	return reader, filename, normalizedRange, nil
}

// traverseUnixFSPath recursively traverses a UnixFS directory structure
// following the provided path components.
//
// This method handles both directory traversal and file resolution within
// the IPFS merkledag structure.
func (f *Fetcher) traverseUnixFSPath(ctx context.Context, dagService format.DAGService, parent format.Node, path []string) (format.Node, error) {
	if len(path) == 0 {
		return parent, nil
	}

	// Create a directory from the current node
	dir, err := fsio.NewDirectoryFromNode(dagService, parent)
	if err != nil {
		return nil, fmt.Errorf("failed to create directory from node: %w", err)
	}

	// Find the child node for the current path component
	child, err := dir.Find(ctx, path[0])
	if err != nil {
		return nil, fmt.Errorf("failed to find child %q: %w", path[0], err)
	}

	// Recursively traverse the remaining path
	return f.traverseUnixFSPath(ctx, dagService, child, path[1:])
}
