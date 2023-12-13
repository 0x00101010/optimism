package consensus

import (
	"bytes"
	"context"
	"time"

	"github.com/ethereum/go-ethereum/log"
	"github.com/hashicorp/raft"

	"github.com/ethereum-optimism/optimism/op-node/rollup"
	"github.com/ethereum-optimism/optimism/op-service/eth"
)

const defaultTimeout = 5 * time.Second

var _ Consensus = (*RaftConsensus)(nil)

// RaftConsensus implements Consensus using raft protocol.
type RaftConsensus struct {
	log           log.Logger
	r             *raft.Raft
	serverID      raft.ServerID
	unsafeTracker *unsafeHeadTracker
	rollupCfg     *rollup.Config
}

// AddNonVoter implements Consensus, it tries to add a non-voting member into the cluster.
func (rc *RaftConsensus) AddNonVoter(ctx context.Context, id string, addr string) error {
	if err := rc.r.AddNonvoter(raft.ServerID(id), raft.ServerAddress(addr), 0, defaultTimeout).Error(); err != nil {
		rc.log.Error("failed to add non-voter", "id", id, "addr", addr, "err", err)
		return err
	}
	return nil
}

// AddVoter implements Consensus, it tries to add a voting member into the cluster.
func (rc *RaftConsensus) AddVoter(ctx context.Context, id string, addr string) error {
	if err := rc.r.AddVoter(raft.ServerID(id), raft.ServerAddress(addr), 0, defaultTimeout).Error(); err != nil {
		rc.log.Error("failed to add voter", "id", id, "addr", addr, "err", err)
		return err
	}
	return nil
}

// DemoteVoter implements Consensus, it tries to demote a voting member into a non-voting member in the cluster.
func (rc *RaftConsensus) DemoteVoter(ctx context.Context, id string) error {
	if err := rc.r.DemoteVoter(raft.ServerID(id), 0, defaultTimeout).Error(); err != nil {
		rc.log.Error("failed to demote voter", "id", id, "err", err)
		return err
	}
	return nil
}

// Leader implements Consensus, it returns true if it is the leader of the cluster.
func (rc *RaftConsensus) Leader(ctx context.Context) bool {
	_, id := rc.r.LeaderWithID()
	return id == rc.serverID
}

// LeaderCh implements Consensus, it returns a channel that will be notified when leadership status changes (true = leader, false = follower).
func (rc *RaftConsensus) LeaderCh() <-chan bool {
	return rc.r.LeaderCh()
}

// RemoveServer implements Consensus, it tries to remove a member (both voter or non-voter) from the cluster, if leader is being removed, it will cause a new leader election.
func (rc *RaftConsensus) RemoveServer(ctx context.Context, id string) error {
	if err := rc.r.RemoveServer(raft.ServerID(id), 0, defaultTimeout).Error(); err != nil {
		rc.log.Error("failed to remove voter", "id", id, "err", err)
		return err
	}
	return nil
}

// ServerID implements Consensus, it returns the server ID of the current server.
func (rc *RaftConsensus) ServerID() string {
	return string(rc.serverID)
}

// TransferLeader implements Consensus, it triggers leadership transfer to another member in the cluster.
func (rc *RaftConsensus) TransferLeader(ctx context.Context) error {
	if err := rc.r.LeadershipTransfer().Error(); err != nil {
		rc.log.Error("failed to transfer leadership", "err", err)
		return err
	}
	return nil
}

// TransferLeaderTo implements Consensus, it triggers leadership transfer to a specific member in the cluster.
func (rc *RaftConsensus) TransferLeaderTo(ctx context.Context, id string, addr string) error {
	if err := rc.r.LeadershipTransferToServer(raft.ServerID(id), raft.ServerAddress(addr)).Error(); err != nil {
		rc.log.Error("failed to transfer leadership to server", "id", id, "addr", addr, "err", err)
		return err
	}
	return nil
}

// Shutdown implements Consensus, it shuts down the consensus protocol client.
func (rc *RaftConsensus) Shutdown() error {
	if err := rc.r.Shutdown().Error(); err != nil {
		rc.log.Error("failed to shutdown raft", "err", err)
		return err
	}
	return nil
}

// CommitUnsafePayload implements Consensus, it commits latest unsafe payload to the cluster FSM.
func (rc *RaftConsensus) CommitUnsafePayload(ctx context.Context, payload eth.ExecutionPayload) error {
	blockVersion := eth.BlockV1
	expectedBlockTime := rc.rollupCfg.TimestampForBlock(uint64(payload.BlockNumber))
	if rc.rollupCfg.IsCanyon(expectedBlockTime) {
		blockVersion = eth.BlockV2
	}

	data := unsafeHeadData{
		version: blockVersion,
		payload: payload,
	}

	var buf bytes.Buffer
	if _, err := data.MarshalSSZ(&buf); err != nil {
		return err
	}

	f := rc.r.Apply(buf.Bytes(), defaultTimeout)
	if err := f.Error(); err != nil {
		return err
	}

	return nil
}

// LatestUnsafePayload implements Consensus, it returns the latest unsafe payload from FSM.
func (rc *RaftConsensus) LatestUnsafePayload(ctx context.Context) eth.ExecutionPayload {
	return rc.unsafeTracker.UnsafeHead()
}
