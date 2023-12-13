package consensus

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"time"

	"github.com/ethereum/go-ethereum/log"
	"github.com/hashicorp/raft"
	boltdb "github.com/hashicorp/raft-boltdb"

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
	rollupCfg     rollup.Config
}

// NewRaftConsensus creates a new RaftConsensus instance.
func NewRaftConsensus(log log.Logger, serverID, serverAddr, serverPort, storageDir string, bootstrap bool, rollupCfg rollup.Config) (*RaftConsensus, error) {
	rc := raft.DefaultConfig()
	rc.LocalID = raft.ServerID(serverID)

	baseDir := filepath.Join(storageDir, serverID)

	var err error
	logStorePath := filepath.Join(baseDir, "raft-log.db")
	logStore, err := boltdb.NewBoltStore(logStorePath)
	if err != nil {
		return nil, fmt.Errorf(`boltdb.NewBoltStore(%q): %v`, logStorePath, err)
	}

	stableStorePath := filepath.Join(baseDir, "raft-stable.db")
	stableStore, err := boltdb.NewBoltStore(stableStorePath)
	if err != nil {
		return nil, fmt.Errorf(`boltdb.NewBoltStore(%q): %v`, stableStorePath, err)
	}

	snapshotStore, err := raft.NewFileSnapshotStoreWithLogger(baseDir, 1, rc.Logger)
	if err != nil {
		return nil, fmt.Errorf(`raft.NewFileSnapshotStore(%q): %v`, baseDir, err)
	}

	// // TODO: make transport credentials configurable for better security.
	// transportMgr := transport.New(raft.ServerAddress(serverAddr), []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())})
	// transport := transportMgr.Transport()
	addr, err := net.ResolveTCPAddr("tcp", serverAddr)
	if err != nil {
		return nil, err
	}

	maxConnPool := 10
	timeout := 5 * time.Second
	transport, err := raft.NewTCPTransportWithLogger(serverPort, addr, maxConnPool, timeout, rc.Logger)
	if err != nil {
		return nil, err
	}

	fsm := &unsafeHeadTracker{}

	r, err := raft.NewRaft(rc, fsm, logStore, stableStore, snapshotStore, transport)
	if err != nil {
		log.Error("failed to create raft", "err", err)
		return nil, err
	}

	// If boostrap = true, start raft in bootstrap mode, this will allow the current node to elect itself as leader when there's no other participants
	// and allow other nodes to join the cluster.
	if bootstrap {
		cfg := raft.Configuration{
			Servers: []raft.Server{
				{
					ID:       rc.LocalID,
					Address:  raft.ServerAddress(serverAddr),
					Suffrage: raft.Voter,
				},
			},
		}

		f := r.BootstrapCluster(cfg)
		if err := f.Error(); err != nil {
			return nil, err
		}
	}

	return &RaftConsensus{
		log:           log,
		r:             r,
		serverID:      raft.ServerID(serverID),
		unsafeTracker: &unsafeHeadTracker{},
		rollupCfg:     rollupCfg,
	}, nil
}

// AddNonVoter implements Consensus, it tries to add a non-voting member into the cluster.
func (rc *RaftConsensus) AddNonVoter(id string, addr string) error {
	if err := rc.r.AddNonvoter(raft.ServerID(id), raft.ServerAddress(addr), 0, defaultTimeout).Error(); err != nil {
		rc.log.Error("failed to add non-voter", "id", id, "addr", addr, "err", err)
		return err
	}
	return nil
}

// AddVoter implements Consensus, it tries to add a voting member into the cluster.
func (rc *RaftConsensus) AddVoter(id string, addr string) error {
	if err := rc.r.AddVoter(raft.ServerID(id), raft.ServerAddress(addr), 0, defaultTimeout).Error(); err != nil {
		rc.log.Error("failed to add voter", "id", id, "addr", addr, "err", err)
		return err
	}
	return nil
}

// DemoteVoter implements Consensus, it tries to demote a voting member into a non-voting member in the cluster.
func (rc *RaftConsensus) DemoteVoter(id string) error {
	if err := rc.r.DemoteVoter(raft.ServerID(id), 0, defaultTimeout).Error(); err != nil {
		rc.log.Error("failed to demote voter", "id", id, "err", err)
		return err
	}
	return nil
}

// Leader implements Consensus, it returns true if it is the leader of the cluster.
func (rc *RaftConsensus) Leader() bool {
	_, id := rc.r.LeaderWithID()
	return id == rc.serverID
}

// LeaderCh implements Consensus, it returns a channel that will be notified when leadership status changes (true = leader, false = follower).
func (rc *RaftConsensus) LeaderCh() <-chan bool {
	return rc.r.LeaderCh()
}

// RemoveServer implements Consensus, it tries to remove a member (both voter or non-voter) from the cluster, if leader is being removed, it will cause a new leader election.
func (rc *RaftConsensus) RemoveServer(id string) error {
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
func (rc *RaftConsensus) TransferLeader() error {
	if err := rc.r.LeadershipTransfer().Error(); err != nil {
		// Expected error if not leader
		if errors.Is(err, raft.ErrNotLeader) {
			return nil
		}

		rc.log.Error("failed to transfer leadership", "err", err)
		return err
	}
	return nil
}

// TransferLeaderTo implements Consensus, it triggers leadership transfer to a specific member in the cluster.
func (rc *RaftConsensus) TransferLeaderTo(id string, addr string) error {
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
func (rc *RaftConsensus) CommitUnsafePayload(payload eth.ExecutionPayload) error {
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
func (rc *RaftConsensus) LatestUnsafePayload() eth.ExecutionPayload {
	return rc.unsafeTracker.UnsafeHead()
}
