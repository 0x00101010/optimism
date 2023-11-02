package testutils

import "github.com/ethereum/go-ethereum/common"

type MockRuntimeConfig struct {
	P2PSeqAddress common.Address
}

func (m *MockRuntimeConfig) P2PSequencerAddress() common.Address {
	return m.P2PSeqAddress
}

func (m *MockRuntimeConfig) P2PSequencerAddressByL1BlockNumber(uint64) (common.Address, bool) {
	return m.P2PSeqAddress, true
}
