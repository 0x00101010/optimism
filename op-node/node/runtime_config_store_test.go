package node_test

import (
	"testing"

	"github.com/ethereum-optimism/optimism/op-node/node"
	"github.com/ethereum-optimism/optimism/op-service/eth"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"
)

func TestRuntimeConfigStore(t *testing.T) {
	capacity := 4
	store := node.NewRuntimeConfigStore(capacity)

	// Test Add
	cfg1 := mockRuntimeConfigData(1, "0x001", 1, 1, 1, 1)
	store.Add(cfg1)

	res, found := store.Get(1)
	require.True(t, found)
	require.Equal(t, cfg1, res)

	res = store.Latest()
	require.Equal(t, cfg1, res)

	// Add more, test Get
	cfg2 := mockRuntimeConfigData(2, "0x001", 1, 1, 1, 1)
	cfg3 := mockRuntimeConfigData(3, "0x001", 1, 1, 1, 1)

	store.Add(cfg2)
	res, found = store.Get(2)
	require.True(t, found)
	require.Equal(t, cfg2, res)

	store.Add(cfg3)
	res, found = store.Get(3)
	require.True(t, found)
	require.Equal(t, cfg3, res)

	_, found = store.Get(4)
	require.False(t, found)

	// Latest
	res = store.Latest()
	require.Equal(t, cfg3, res)

	cfg4 := mockRuntimeConfigData(4, "0x001", 1, 2, 3, 4)
	store.Add(cfg4)
	res = store.Latest()
	require.Equal(t, cfg4, res)

	// Test capacity
	cfg5 := mockRuntimeConfigData(5, "0x001", 1, 2, 3, 4)
	store.Add(cfg5)
	res = store.Latest()
	require.Equal(t, cfg5, res)

	res, found = store.Get(1) // 1 should be evicted now
	require.False(t, found)
	require.Equal(t, node.RuntimeConfigData{}, res)
}

func mockRuntimeConfigData(blkNum uint64, unsafeBlockSigner string, major, minor, patch, preRelease uint32) node.RuntimeConfigData {
	return node.NewRuntimeConfigData(
		eth.L1BlockRef{Number: blkNum},
		eth.SystemConfig{UnsafeBlockSigner: common.HexToAddress(unsafeBlockSigner)},
		params.ProtocolVersionV0{
			Build:      [8]byte{},
			Major:      major,
			Minor:      minor,
			Patch:      patch,
			PreRelease: preRelease,
		}.Encode(),
		params.ProtocolVersionV0{
			Build:      [8]byte{},
			Major:      major,
			Minor:      minor,
			Patch:      patch,
			PreRelease: preRelease,
		}.Encode(),
	)
}
