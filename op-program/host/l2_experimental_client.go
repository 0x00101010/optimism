package host

import (
	"context"

	"github.com/ethereum-optimism/optimism/op-program/host/prefetcher"
	"github.com/ethereum-optimism/optimism/op-service/client"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/stateless"
)

type L2ExperimentalClient struct {
	*L2Client
	client client.RPC
}

var _ prefetcher.L2Source = &L2ExperimentalClient{}

func NewL2ExperimentalClient(l2Client *L2Client, client client.RPC) *L2ExperimentalClient {
	return &L2ExperimentalClient{
		L2Client: l2Client,
		client:   client,
	}
}

// CodeByHash implements prefetcher.L2Source.
func (s *L2ExperimentalClient) CodeByHash(ctx context.Context, hash common.Hash) ([]byte, error) {
	panic("unsupported")
}

// NodeByHash implements prefetcher.L2Source.
func (s *L2ExperimentalClient) NodeByHash(ctx context.Context, hash common.Hash) ([]byte, error) {
	panic("unsupported")
}

func (s *L2ExperimentalClient) ExecutionWitness(ctx context.Context, blockHash common.Hash) (*stateless.Witness, error) {
	var witness stateless.Witness
	err := s.client.CallContext(ctx, &witness, "debug_executionWitness", blockHash)
	if err != nil {
		return nil, err
	}
	return &witness, nil
}
