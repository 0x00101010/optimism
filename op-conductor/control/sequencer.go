package control

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
)

// SequencerControl defines the interface for controlling the sequencer.
type SequencerControl interface {
	StartSequencer(ctx context.Context) error
	StopSequencer(ctx context.Context) (common.Hash, error)
	StartBatcher(ctx context.Context) error
	StopBatcher(ctx context.Context) error
}
