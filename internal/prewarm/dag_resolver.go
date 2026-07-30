package prewarm

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/samber/lo"
	ipfs "go.lumeweb.com/ipfs-sdk"
)

// DAGNode represents a single block in a resolved DAG.
type DAGNode struct {
	CID      string
	Size     int
	Children []string
}

// DAGResolver resolves the complete block graph for a root CID
// in a single batch operation. This is the DAG-bypass path that
// avoids N per-block roundtrips via bitswap DAG walking.
type DAGResolver interface {
	ResolveDAG(ctx context.Context, cid string) ([]DAGNode, error)
}

// sdkDAGResolver adapts ipfs.DAGService to the DAGResolver interface.
type sdkDAGResolver struct {
	svc ipfs.DAGService
}

// NewDAGResolver creates a DAGResolver backed by the SDK's DAGService.
func NewDAGResolver(svc ipfs.DAGService) DAGResolver {
	return &sdkDAGResolver{svc: svc}
}

// NewDAGResolverFromConfig creates a DAGResolver from portal connection config.
// This is a convenience for main.go wiring — it creates an SDK client and
// extracts the DAG service.
func NewDAGResolverFromConfig(baseURL, secret string, timeout time.Duration) (DAGResolver, error) {
	client, err := ipfs.NewClient(baseURL, "", ipfs.WithGatewaySecret(secret))
	if err != nil {
		return nil, fmt.Errorf("create ipfs-sdk client for DAG resolver: %w", err)
	}
	client.SetHTTPClient(&http.Client{Timeout: timeout})
	return NewDAGResolver(client.DAG()), nil
}

func (r *sdkDAGResolver) ResolveDAG(ctx context.Context, cid string) ([]DAGNode, error) {
	resp, err := r.svc.ResolveDAG(ctx, cid)
	if err != nil {
		return nil, fmt.Errorf("resolve DAG for %s: %w", cid, err)
	}
	if resp == nil {
		return nil, nil
	}

	return lo.Map(resp.Nodes, func(n ipfs.DAGBlockNodeResponse, _ int) DAGNode {
		return DAGNode{
			CID:      n.Cid,
			Size:     n.Size,
			Children: n.Children,
		}
	}), nil
}
