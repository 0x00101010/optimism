package consensus

import (
	"context"

	"github.com/ethereum-optimism/optimism/op-service/eth"
)

// Consensus defines the consensus interface for leadership election.
type Consensus interface {
	// AddVoter adds a voting member into the cluster, voter is elegible to become leader.
	AddVoter(ctx context.Context, id, addr string) error
	// AddNonVoter adds a non-voting member into the cluster, non-voter is not elegible to become leader.
	AddNonVoter(ctx context.Context, id, addr string) error
	// DemoteVoter demotes a voting member into a non-voting member, if leader is being demoted, it will cause a new leader election.
	DemoteVoter(ctx context.Context, id string) error
	// RemoveServer removes a member (both voter or non-voter) from the cluster, if leader is being removed, it will cause a new leader election.
	RemoveServer(ctx context.Context, id string) error

	// LeaderCh returns a channel that will be notified when leadership status changes (true = leader, false = follower)
	LeaderCh() <-chan bool
	// Leader returns if it is the leader of the cluster.
	Leader(ctx context.Context) bool
	// ServerID returns the server ID of the consensus.
	ServerID() string
	// TransferLeader triggers leadership transfer to another member in the cluster.
	TransferLeader(ctx context.Context) error
	// TransferLeaderTo triggers leadership transfer to a specific member in the cluster.
	TransferLeaderTo(ctx context.Context, id, addr string) error

	// CommitPayload commits latest unsafe payload to the cluster FSM.
	CommitUnsafePayload(ctx context.Context, payload eth.ExecutionPayload) error
	// LatestUnsafeBlock returns the latest unsafe payload from FSM.
	LatestUnsafePayload(ctx context.Context) eth.ExecutionPayload
	// Shutdown shuts down the consensus protocol client.
	Shutdown() error
}
