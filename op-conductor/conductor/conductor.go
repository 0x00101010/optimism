package conductor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/log"
	"github.com/hashicorp/go-multierror"

	"github.com/ethereum-optimism/optimism/op-conductor/consensus"
	"github.com/ethereum-optimism/optimism/op-conductor/control"
	"github.com/ethereum-optimism/optimism/op-conductor/health"
	"github.com/ethereum-optimism/optimism/op-service/cliapp"
)

var (
	ErrResumeTimeout = errors.New("timeout to resume conductor")
	ErrPauseTimeout  = errors.New("timeout to pause conductor")
)

var _ cliapp.Lifecycle = (*OpConductor)(nil)

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

	cfg       Config
	consensus consensus.Consensus
	ctrl      control.SequencerControl
	hm        health.HealthMonitor

	wg             sync.WaitGroup
	pauseCh        chan struct{}
	unpauseCh      chan struct{}
	paused         atomic.Bool
	stopped        atomic.Bool
	shutdownCtx    context.Context
	shutdownCancel context.CancelFunc
}

// New creates a new OpConductor instance.
func New(ctx context.Context, cfg Config, log log.Logger, version string) (*OpConductor, error) {
	if err := cfg.Check(); err != nil {
		return nil, err
	}

	oc := &OpConductor{
		log:       log,
		version:   version,
		cfg:       cfg,
		pauseCh:   make(chan struct{}),
		unpauseCh: make(chan struct{}),
	}
	oc.shutdownCtx, oc.shutdownCancel = context.WithCancel(context.Background())

	if err := oc.init(); err != nil {
		log.Error("failed to initialize OpConductor", "err", err)
		if closeErr := oc.Stop(ctx); closeErr != nil {
			return nil, multierror.Append(err, closeErr)
		}
		return nil, err
	}

	return oc, nil
}

func (oc *OpConductor) init() error {
	oc.log.Info("Initializing OpConductor", "version", oc.version)
	if err := oc.initConsensus(); err != nil {
		return fmt.Errorf("failed to init consensus: %w", err)
	}
	if err := oc.initHealthMonitor(); err != nil {
		return fmt.Errorf("failed to init health monitor: %w", err)
	}
	if err := oc.initSequencerControl(); err != nil {
		return fmt.Errorf("failed to init sequencer control: %w", err)
	}
	// TODO: init metrics / rpc / pprof

	return nil
}

func (oc *OpConductor) initConsensus() error {
	c, err := consensus.NewRaftConsensus(
		oc.log,
		oc.cfg.RaftServerID,
		oc.cfg.ConsensusAddr,
		fmt.Sprint(oc.cfg.ConsensusPort),
		oc.cfg.RaftStorageDIR,
		oc.cfg.RaftBootstrap,
		oc.cfg.RollupCfg,
	)
	if err != nil {
		return err
	}

	oc.consensus = c
	return nil
}

func (oc *OpConductor) initHealthMonitor() error {
	return nil
}

func (oc *OpConductor) initSequencerControl() error {
	return nil
}

// Start implements cliapp.Lifecycle.
func (oc *OpConductor) Start(ctx context.Context) error {
	oc.log.Info("Starting OpConductor")

	oc.shutdownCtx, oc.shutdownCancel = context.WithCancel(ctx)
	oc.wg.Add(1)
	go oc.loop()

	oc.log.Info("OpConductor started")
	return nil
}

// Stop implements cliapp.Lifecycle.
func (oc *OpConductor) Stop(_ context.Context) error {
	oc.log.Info("Stopping OpConductor")

	// close control loop
	oc.shutdownCancel()
	oc.wg.Wait()

	// stop health check
	oc.hm.Stop()

	// stop consensus participation
	if err := oc.consensus.Shutdown(); err != nil {
		oc.log.Error("failed to shutdown consensus", "err", err)
		return err
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
	select {
	case oc.pauseCh <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ErrPauseTimeout
	}
}

// Resume resumes the control loop of OpConductor.
func (oc *OpConductor) Resume(ctx context.Context) error {
	select {
	case oc.unpauseCh <- struct{}{}:
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
	for {
		select {
		case <-oc.pauseCh:
			oc.waitForUnpauseOrShutdown()
		case <-oc.shutdownCtx.Done():
			return
		case leader := <-oc.consensus.LeaderCh():
			oc.log.Info(fmt.Sprintf("Leadership changed at %s", oc.consensus.ServerID()), "leader", leader)
			if leader {
				oc.handleBecomingLeader()
			} else {
				oc.handleSteppingDownAsLeader()
			}
		case healthy := <-oc.hm.Subscribe():
			oc.handleHealthUpdate(healthy)
		}
	}
}

func (oc *OpConductor) waitForUnpauseOrShutdown() {
	oc.paused.Store(true)
	for {
		select {
		case <-oc.unpauseCh:
			oc.paused.Store(false)
			return
		case <-oc.shutdownCtx.Done():
			return
		}
	}
}

func (oc *OpConductor) handleBecomingLeader() {
	// TODO: wait for sequencer to catch up to latest block stored in consensus, then start sequencer at that block.
	// potentially peer conductor with sequencer to pass the latest block if necessary.
}

func (oc *OpConductor) handleSteppingDownAsLeader() {
	ctx := context.Background()
	// stop sequencing first
	if _, err := oc.ctrl.StopSequencer(ctx); err != nil {
		oc.log.Error("failed to stop sequencer", "err", err)
	}
	// stop batcher
	if err := oc.ctrl.StopBatcher(ctx); err != nil {
		oc.log.Error("failed to stop batcher", "err", err)
	}
}

func (oc *OpConductor) handleHealthUpdate(healthy bool) {
	oc.log.Info(fmt.Sprintf("Health status at %s", oc.consensus.ServerID()), "healthy", healthy)
	// do nothing if healthy
	if healthy {
		return
	}

	if err := oc.consensus.TransferLeader(); err != nil {
		// This should be very rare cases, only when raft is shutting down or there's already a leadership transfer in progress does the error happen.
		// We should not panic here, but we should log the error and try again later.
		oc.log.Error("failed to transfer leadership", "err", err)
	}
}
