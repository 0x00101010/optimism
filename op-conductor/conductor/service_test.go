package conductor_test

import (
	"context"
	"math/big"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/log"
	"github.com/stretchr/testify/require"

	clientmocks "github.com/ethereum-optimism/optimism/op-conductor/client/mocks"
	"github.com/ethereum-optimism/optimism/op-conductor/conductor"
	consensusmocks "github.com/ethereum-optimism/optimism/op-conductor/consensus/mocks"
	healthmocks "github.com/ethereum-optimism/optimism/op-conductor/health/mocks"
	"github.com/ethereum-optimism/optimism/op-node/rollup"
	"github.com/ethereum-optimism/optimism/op-service/eth"
	"github.com/ethereum-optimism/optimism/op-service/testlog"
)

func mockConfig(t *testing.T) conductor.Config {
	now := uint64(time.Now().Unix())
	dir, err := os.MkdirTemp("/tmp", "")
	require.NoError(t, err)
	return conductor.Config{
		ConsensusAddr:  "127.0.0.1",
		ConsensusPort:  50050,
		RaftServerID:   "SequencerA",
		RaftStorageDir: dir,
		RaftBootstrap:  true,
		NodeRPC:        "http://node:8545",
		ExecutionRPC:   "http://geth:8545",
		HealthCheck: conductor.HealthCheckConfig{
			Interval:     1,
			SafeInterval: 5,
			MinPeerCount: 1,
		},
		RollupCfg: rollup.Config{
			Genesis: rollup.Genesis{
				L1: eth.BlockID{
					Hash:   [32]byte{1, 2},
					Number: 100,
				},
				L2: eth.BlockID{
					Hash:   [32]byte{2, 3},
					Number: 0,
				},
				L2Time: now,
				SystemConfig: eth.SystemConfig{
					BatcherAddr: [20]byte{1},
					Overhead:    [32]byte{1},
					Scalar:      [32]byte{1},
					GasLimit:    30000000,
				},
			},
			BlockTime:               2,
			MaxSequencerDrift:       600,
			SeqWindowSize:           3600,
			ChannelTimeout:          300,
			L1ChainID:               big.NewInt(1),
			L2ChainID:               big.NewInt(2),
			RegolithTime:            nil,
			CanyonTime:              &now,
			DeltaTime:               nil,
			EclipseTime:             nil,
			FjordTime:               nil,
			InteropTime:             nil,
			BatchInboxAddress:       [20]byte{1, 2},
			DepositContractAddress:  [20]byte{2, 3},
			L1SystemConfigAddress:   [20]byte{3, 4},
			ProtocolVersionsAddress: [20]byte{4, 5},
		},
	}
}

// TestControlLoop tests the control loop, and functionalities around it like Pause / Resume / Start / Stop.
func TestControlLoop(t *testing.T) {
	ctx := context.Background()
	cfg := mockConfig(t)
	log := testlog.Logger(t, log.LvlInfo)
	version := "v0.0.1"
	mockCtrl := &clientmocks.SequencerControl{}
	mockCons := &consensusmocks.Consensus{}
	mockHmon := &healthmocks.HealthMonitor{}

	// Scenario 1
	svc, err := conductor.NewOpConductor(ctx, &cfg, log, version, mockCtrl, mockCons, mockHmon)
	require.NoError(t, err)

	// Start
	healthUpdateCh := make(chan bool)
	mockHmon.EXPECT().Start().Return(nil)
	mockHmon.EXPECT().Subscribe().Return(healthUpdateCh)
	leaderCh := make(chan bool)
	mockCons.EXPECT().LeaderCh().Return(leaderCh)
	err = svc.Start(ctx)
	require.NoError(t, err)
	require.False(t, svc.Stopped())

	// Stopped
	require.False(t, svc.Stopped())

	// Pause
	err = svc.Pause(ctx)
	require.NoError(t, err)
	require.True(t, svc.Paused())

	var huConsumed atomic.Bool
	go func() {
		healthUpdateCh <- true
		huConsumed.Store(true)
	}()

	mockCons.EXPECT().ServerID().Return("SequencerA")
	var lcConsumed atomic.Bool
	go func() {
		leaderCh <- true
		lcConsumed.Store(true)
	}()

	// make sure other channels are not consumed during pause
	time.Sleep(100 * time.Millisecond)
	require.False(t, huConsumed.Load())
	require.False(t, lcConsumed.Load())

	// Pause again
	err = svc.Pause(ctx)
	require.NoError(t, err)
	require.True(t, svc.Paused())

	// Resume
	err = svc.Resume(ctx)
	require.NoError(t, err)
	require.False(t, svc.Paused())

	// make sure health update channel is consumed after resume
	time.Sleep(100 * time.Millisecond)
	require.True(t, huConsumed.Load())
	require.True(t, lcConsumed.Load())

	// Resume again
	err = svc.Resume(ctx)
	require.NoError(t, err)
	require.False(t, svc.Paused())

	// Stop
	mockHmon.EXPECT().Stop().Return(nil)
	mockCons.EXPECT().Shutdown().Return(nil)
	err = svc.Stop(ctx)
	require.NoError(t, err)
	require.True(t, svc.Stopped())

	// Scenario 2
	svc, err = conductor.NewOpConductor(ctx, &cfg, log, version, mockCtrl, mockCons, mockHmon)
	require.NoError(t, err)

	// Start
	err = svc.Start(ctx)
	require.NoError(t, err)
	require.False(t, svc.Stopped())

	// Pause
	err = svc.Pause(ctx)
	require.NoError(t, err)
	require.True(t, svc.Paused())

	// Stop can stop directly from paused state
	err = svc.Stop(ctx)
	require.NoError(t, err)
	require.True(t, svc.Stopped())
}
