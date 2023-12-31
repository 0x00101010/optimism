package conductor

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/hashicorp/go-multierror"
	"github.com/hashicorp/raft"
	"github.com/pkg/errors"

	"github.com/ethereum-optimism/optimism/op-conductor/client"
	"github.com/ethereum-optimism/optimism/op-conductor/consensus"
	"github.com/ethereum-optimism/optimism/op-conductor/health"
	opp2p "github.com/ethereum-optimism/optimism/op-node/p2p"
	"github.com/ethereum-optimism/optimism/op-service/cliapp"
	opclient "github.com/ethereum-optimism/optimism/op-service/client"
	"github.com/ethereum-optimism/optimism/op-service/sources"
)

var (
	ErrResumeTimeout      = errors.New("timeout to resume conductor")
	ErrPauseTimeout       = errors.New("timeout to pause conductor")
	ErrUnsafeHeadMismarch = errors.New("unsafe head mismatch")
)

// New creates a new OpConductor instance.
func New(ctx context.Context, cfg *Config, log log.Logger, version string) (*OpConductor, error) {
	return NewOpConductor(ctx, cfg, log, version, nil, nil, nil)
}

// NewOpConductor creates a new OpConductor instance.
func NewOpConductor(
	ctx context.Context,
	cfg *Config,
	log log.Logger,
	version string,
	ctrl client.SequencerControl,
	cons consensus.Consensus,
	hm health.HealthMonitor,
) (*OpConductor, error) {
	if err := cfg.Check(); err != nil {
		return nil, errors.Wrap(err, "invalid config")
	}

	oc := &OpConductor{
		log:          log,
		version:      version,
		cfg:          cfg,
		pauseCh:      make(chan struct{}),
		pauseDoneCh:  make(chan struct{}),
		resumeCh:     make(chan struct{}),
		resumeDoneCh: make(chan struct{}),
		stepCh:       make(chan struct{}, 1),
		ctrl:         ctrl,
		cons:         cons,
		hm:           hm,
	}
	// explicitly set all atomic.Bool values
	oc.leader.Store(false)
	oc.healthy.Store(false)
	oc.sequencerActive.Store(false)
	oc.paused.Store(false)
	oc.stopped.Store(false)

	err := oc.init(ctx)
	if err != nil {
		log.Error("failed to initialize OpConductor", "err", err)
		// ensure we always close the resources if we fail to initialize the conductor.
		if closeErr := oc.Stop(ctx); closeErr != nil {
			return nil, multierror.Append(err, closeErr)
		}
	}

	return oc, nil
}

func (c *OpConductor) init(ctx context.Context) error {
	c.log.Info("initializing OpConductor", "version", c.version)
	if err := c.initSequencerControl(ctx); err != nil {
		return errors.Wrap(err, "failed to initialize sequencer control")
	}
	if err := c.initConsensus(ctx); err != nil {
		return errors.Wrap(err, "failed to initialize consensus")
	}
	if err := c.initHealthMonitor(ctx); err != nil {
		return errors.Wrap(err, "failed to initialize health monitor")
	}
	return nil
}

func (c *OpConductor) initSequencerControl(ctx context.Context) error {
	if c.ctrl != nil {
		return nil
	}

	ec, err := opclient.NewRPC(ctx, c.log, c.cfg.ExecutionRPC)
	if err != nil {
		return errors.Wrap(err, "failed to create geth rpc client")
	}
	execCfg := sources.L2ClientDefaultConfig(&c.cfg.RollupCfg, true)
	// TODO: Add metrics tracer here. tracked by https://github.com/ethereum-optimism/protocol-quest/issues/45
	exec, err := sources.NewEthClient(ec, c.log, nil, &execCfg.EthClientConfig)
	if err != nil {
		return errors.Wrap(err, "failed to create geth client")
	}

	nc, err := opclient.NewRPC(ctx, c.log, c.cfg.NodeRPC)
	if err != nil {
		return errors.Wrap(err, "failed to create node rpc client")
	}
	node := sources.NewRollupClient(nc)
	c.ctrl = client.NewSequencerControl(exec, node)
	return nil
}

func (c *OpConductor) initConsensus(ctx context.Context) error {
	if c.cons != nil {
		return nil
	}

	serverAddr := fmt.Sprintf("%s:%d", c.cfg.ConsensusAddr, c.cfg.ConsensusPort)
	cons, err := consensus.NewRaftConsensus(c.log, c.cfg.RaftServerID, serverAddr, c.cfg.RaftStorageDir, c.cfg.RaftBootstrap, &c.cfg.RollupCfg)
	if err != nil {
		return errors.Wrap(err, "failed to create raft consensus")
	}
	c.cons = cons
	// if started in bootstrap mode, this current node will be the leader.
	if c.cfg.RaftBootstrap {
		c.leader.Store(true)
	}
	return nil
}

func (c *OpConductor) initHealthMonitor(ctx context.Context) error {
	if c.hm != nil {
		return nil
	}

	nc, err := opclient.NewRPC(ctx, c.log, c.cfg.NodeRPC)
	if err != nil {
		return errors.Wrap(err, "failed to create node rpc client")
	}
	node := sources.NewRollupClient(nc)

	pc, err := rpc.DialContext(ctx, c.cfg.NodeRPC)
	if err != nil {
		return errors.Wrap(err, "failed to create p2p rpc client")
	}
	p2p := opp2p.NewClient(pc)

	c.hm = health.NewSequencerHealthMonitor(
		c.log,
		c.cfg.HealthCheck.Interval,
		c.cfg.HealthCheck.SafeInterval,
		c.cfg.HealthCheck.MinPeerCount,
		&c.cfg.RollupCfg,
		node,
		p2p,
	)

	return nil
}

// OpConductor represents a full conductor instance and its resources, it does:
//  1. performs health checks on sequencer
//  2. participate in consensus protocol for leader election
//  3. and control sequencer state based on leader and sequencer health status.
//
// OpConductor has three states:
//  1. running: it is running normally, which executes control loop and participates in leader election.
//  2. paused: control loop (sequencer start/stop) is paused, but it still participates in leader election.
//     it is paused for disaster recovery situation
//  3. stopped: it is stopped, which means it is not participating in leader election and control loop. OpConductor cannot be started again from stopped mode.
type OpConductor struct {
	log     log.Logger
	version string
	cfg     *Config

	ctrl client.SequencerControl
	cons consensus.Consensus
	hm   health.HealthMonitor

	leader          atomic.Bool
	healthy         atomic.Bool
	sequencerActive atomic.Bool

	wg             sync.WaitGroup
	pauseCh        chan struct{}
	pauseDoneCh    chan struct{}
	resumeCh       chan struct{}
	resumeDoneCh   chan struct{}
	stepCh         chan struct{}
	paused         atomic.Bool
	stopped        atomic.Bool
	shutdownCtx    context.Context
	shutdownCancel context.CancelFunc
}

var _ cliapp.Lifecycle = (*OpConductor)(nil)

// Start implements cliapp.Lifecycle.
func (oc *OpConductor) Start(ctx context.Context) error {
	oc.log.Info("starting OpConductor")

	if err := oc.hm.Start(); err != nil {
		return errors.Wrap(err, "failed to start health monitor")
	}

	oc.shutdownCtx, oc.shutdownCancel = context.WithCancel(ctx)
	oc.wg.Add(1)
	go oc.loop()

	oc.log.Info("OpConductor started")
	return nil
}

// Stop implements cliapp.Lifecycle.
func (oc *OpConductor) Stop(ctx context.Context) error {
	oc.log.Info("stopping OpConductor")

	var result *multierror.Error

	// close control loop
	oc.shutdownCancel()
	oc.wg.Wait()

	// stop health check
	if err := oc.hm.Stop(); err != nil {
		result = multierror.Append(result, errors.Wrap(err, "failed to stop health monitor"))
	}

	if err := oc.cons.Shutdown(); err != nil {
		result = multierror.Append(result, errors.Wrap(err, "failed to shutdown consensus"))
	}

	if result.ErrorOrNil() != nil {
		oc.log.Error("failed to stop OpConductor", "err", result.ErrorOrNil())
		return result.ErrorOrNil()
	}

	oc.stopped.Store(true)
	oc.log.Info("OpConductor stopped")
	return nil
}

// Stopped implements cliapp.Lifecycle.
func (oc *OpConductor) Stopped() bool {
	return oc.stopped.Load()
}

// Pause pauses the control loop of OpConductor, but still allows it to participate in leader election.
func (oc *OpConductor) Pause(ctx context.Context) error {
	if oc.Paused() {
		return nil
	}

	select {
	case oc.pauseCh <- struct{}{}:
		<-oc.pauseDoneCh
		return nil
	case <-ctx.Done():
		return ErrPauseTimeout
	}
}

// Resume resumes the control loop of OpConductor.
func (oc *OpConductor) Resume(ctx context.Context) error {
	if !oc.Paused() {
		return nil
	}

	select {
	case oc.resumeCh <- struct{}{}:
		<-oc.resumeDoneCh
		return nil
	case <-ctx.Done():
		return ErrResumeTimeout
	}
}

// Paused returns true if OpConductor is paused.
func (oc *OpConductor) Paused() bool {
	return oc.paused.Load()
}

func (oc *OpConductor) loop() {
	defer oc.wg.Done()
	healthUpdate := oc.hm.Subscribe()
	leaderUpdate := oc.cons.LeaderCh()

	for {
		select {
		case <-oc.shutdownCtx.Done():
			return
		case <-oc.pauseCh:
			oc.waitForResumeOrShutdown()
		case leader := <-leaderUpdate:
			oc.handleLeaderUpdate(leader)
		case healthy := <-healthUpdate:
			oc.handleHealthUpdate(healthy)
		case <-oc.stepCh:
			oc.step()
		}
	}
}

func (oc *OpConductor) waitForResumeOrShutdown() {
	oc.paused.Store(true)
	oc.pauseDoneCh <- struct{}{}
	for {
		select {
		case <-oc.shutdownCtx.Done():
			return
		case <-oc.resumeCh:
			oc.paused.Store(false)
			oc.resumeDoneCh <- struct{}{}
			return
		}
	}
}

// handleLeaderUpdate handles leadership update from consensus.
// whenever leadership changed, we delegate the handling to the control loop step.
func (oc *OpConductor) handleLeaderUpdate(leader bool) {
	oc.log.Info(fmt.Sprintf("Leadership status changed at %s", oc.cons.ServerID()), "leader", leader)
	oc.leader.Store(leader)
	oc.stepCh <- struct{}{}
}

// handleHealthUpdate handles health update from health monitor.
// whenever health status changed, we delegate the handling to the control loop step.
func (oc *OpConductor) handleHealthUpdate(healthy bool) {
	if !healthy {
		oc.log.Error("Sequencer is unhealthy", "server", oc.cons.ServerID())
	}

	if healthy != oc.healthy.Load() {
		oc.healthy.Store(healthy)
		// health status changed, queue a step to handle it.
		oc.stepCh <- struct{}{}
	}
}

// transferLeader tries to transfer leadership to another server.
func (oc *OpConductor) transferLeader() error {
	// TransferLeader here will do round robin to try to transfer leadership to the next healthy node.
	err := oc.cons.TransferLeader()
	if err == nil {
		oc.leader.Store(false)
		return nil // success
	}

	switch {
	case errors.Is(err, raft.ErrLeadershipTransferInProgress):
		// Leadership transfer is already in progress, do nothing, this error will only occur when current node is still the leader.
		oc.log.Warn("leadership transfer is already in progress")
		return nil
	case errors.Is(err, raft.ErrNotLeader):
		// This node is not the leader, do nothing.
		oc.log.Warn("cannot transfer leadership since current server is not the leader")
		return nil
	default:
		oc.log.Error("failed to transfer leadership", "err", err)
		return err
	}
}

// step tries to bring the sequencer to the desired state, a retry will be queued if any step failed.
func (oc *OpConductor) step() {
	var err error
	// exhaust all cases below for completeness, 3 state, 8 cases.
	switch status := struct{ leader, healthy, active bool }{oc.cons.Leader(), oc.healthy.Load(), oc.sequencerActive.Load()}; {
	case !status.leader && !status.healthy && !status.active:
		// normal case, although not healthy, log it
		oc.log.Error("server (follower) is not healthy", "server", oc.cons.ServerID())
	case !status.leader && !status.healthy && status.active:
		// stop sequencer
		err = oc.stopSequencer()
	case !status.leader && status.healthy && !status.active:
		// normal follower, do nothing
	case !status.leader && status.healthy && status.active:
		// stop sequencer, this happens when current server steps down as leader.
		err = oc.stopSequencer()
	case status.leader && !status.healthy && !status.active:
		// transfer leadership to another node
		err = oc.transferLeader()
	case status.leader && !status.healthy && status.active:
		// stop sequencer, transfer leadership to another node
		if err = oc.stopSequencer(); err != nil {
			break
		}
		err = oc.transferLeader()
	case status.leader && status.healthy && !status.active:
		// start sequencer
		err = oc.startSequencer()
	case status.leader && status.healthy && status.active:
		// normal leader, do nothing
	}

	if err != nil {
		oc.log.Error("failed to execute step, queueing another one to retry", "err", err)
		oc.stepCh <- struct{}{}
	}
}

func (oc *OpConductor) stopSequencer() error {
	oc.log.Info("stopping sequencer", "server", oc.cons.ServerID(), "leader", oc.cons.Leader(), "healthy", oc.healthy.Load(), "active", oc.sequencerActive.Load())

	if _, err := oc.ctrl.StopSequencer(oc.shutdownCtx); err != nil {
		return err
	}
	oc.sequencerActive.Store(false)
	return nil
}

func (oc *OpConductor) startSequencer() error {
	oc.log.Info("starting sequencer", "server", oc.cons.ServerID(), "leader", oc.cons.Leader(), "healthy", oc.healthy.Load(), "active", oc.sequencerActive.Load())

	// When starting sequencer, we need to make sure that the current node has the latest unsafe head from the consensus protocol
	// If not, then we wait for the unsafe head to catch up or gossip it to op-node manually from op-conductor.
	unsafeInCons := oc.cons.LatestUnsafePayload()
	unsafeInNode, err := oc.ctrl.LatestUnsafeBlock(oc.shutdownCtx)
	if err != nil {
		return errors.Wrap(err, "failed to get latest unsafe block from EL during startSequencer phase")
	}

	if unsafeInCons.BlockHash != unsafeInNode.Hash() {
		oc.log.Warn(
			"latest unsafe block in consensus is not the same as the one in node",
			"consensus_hash", unsafeInCons.BlockHash,
			"consensus_block_num", unsafeInCons.BlockNumber,
			"node_hash", unsafeInNode.Hash(),
			"node_block_num", unsafeInNode.NumberU64(),
		)

		// tries to gossip the unsafe head to op-node

		return ErrUnsafeHeadMismarch // return error to allow retry
	}

	return oc.ctrl.StartSequencer(oc.shutdownCtx, unsafeInCons.BlockHash)
}
