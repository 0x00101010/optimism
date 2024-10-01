package l2

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"

	preimage "github.com/ethereum-optimism/optimism/op-preimage"
)

const (
	HintL2BlockHeader  = "l2-block-header"
	HintL2Transactions = "l2-transactions"
	HintL2Code         = "l2-code"
	HintL2StateNode    = "l2-state-node"
	HintL2Output       = "l2-output"
	HintL2AccountProof = "l2-account-proof"
)

type BlockHeaderHint common.Hash

var _ preimage.Hint = BlockHeaderHint{}

func (l BlockHeaderHint) Hint() string {
	return HintL2BlockHeader + " " + (common.Hash)(l).String()
}

type TransactionsHint common.Hash

var _ preimage.Hint = TransactionsHint{}

func (l TransactionsHint) Hint() string {
	return HintL2Transactions + " " + (common.Hash)(l).String()
}

type CodeHint common.Hash

var _ preimage.Hint = CodeHint{}

func (l CodeHint) Hint() string {
	return HintL2Code + " " + (common.Hash)(l).String()
}

type StateNodeHint common.Hash

var _ preimage.Hint = StateNodeHint{}

func (l StateNodeHint) Hint() string {
	return HintL2StateNode + " " + (common.Hash)(l).String()
}

type L2OutputHint common.Hash

var _ preimage.Hint = L2OutputHint{}

func (l L2OutputHint) Hint() string {
	return HintL2Output + " " + (common.Hash)(l).String()
}

type L2AccountProofHint []byte

var _ preimage.Hint = L2AccountProofHint{}

func (l L2AccountProofHint) Hint() string {
	return HintL2AccountProof + " " + hexutil.Encode(l)
}
