package flags

import (
	"fmt"

	"github.com/urfave/cli/v2"

	opservice "github.com/ethereum-optimism/optimism/op-service"
	oplog "github.com/ethereum-optimism/optimism/op-service/log"
	opmetrics "github.com/ethereum-optimism/optimism/op-service/metrics"
	oppprof "github.com/ethereum-optimism/optimism/op-service/pprof"
	oprpc "github.com/ethereum-optimism/optimism/op-service/rpc"
)

const EnvVarPrefix = "OP_CONDUCTOR"

var (
	ConsensusAddr = &cli.StringFlag{
		Name:    "consensus.addr",
		Usage:   "Address to listen for consensus connections",
		EnvVars: opservice.PrefixEnvVar(EnvVarPrefix, "CONSENSUS_ADDR"),
		Value:   "127.0.0.1",
	}
	ConsensusPort = &cli.IntFlag{
		Name:    "consensus.port",
		Usage:   "Port to listen for consensus connections",
		EnvVars: opservice.PrefixEnvVar(EnvVarPrefix, "CONSENSUS_PORT"),
		Value:   50050,
	}
	RaftServerID = &cli.StringFlag{
		Name:    "raft.server.id",
		Usage:   "Unique ID for this server used by raft consensus",
		EnvVars: opservice.PrefixEnvVar(EnvVarPrefix, "SERVER_ID"),
	}
	RaftStorageDIR = &cli.StringFlag{
		Name:    "raft.storage.dir",
		Usage:   "Directory to store raft data",
		EnvVars: opservice.PrefixEnvVar(EnvVarPrefix, "RAFT_STORAGE_DIR"),
	}
	NodeRPC = &cli.StringFlag{
		Name:    "node.rpc",
		Usage:   "HTTP provider URL for op-node",
		EnvVars: opservice.PrefixEnvVar(EnvVarPrefix, "NODE_RPC"),
	}
	BatcherRPC = &cli.StringFlag{
		Name:    "batcher.rpc",
		Usage:   "HTTP provider URL for op-batcher",
		EnvVars: opservice.PrefixEnvVar(EnvVarPrefix, "BATCHER_RPC"),
	}
)

var requiredFlags = []cli.Flag{
	ConsensusAddr,
	ConsensusPort,
	RaftServerID,
	RaftStorageDIR,
	NodeRPC,
	BatcherRPC,
}

var optionalFlags = []cli.Flag{}

func init() {
	optionalFlags = append(optionalFlags, oprpc.CLIFlags(EnvVarPrefix)...)
	optionalFlags = append(optionalFlags, oplog.CLIFlags(EnvVarPrefix)...)
	optionalFlags = append(optionalFlags, opmetrics.CLIFlags(EnvVarPrefix)...)
	optionalFlags = append(optionalFlags, oppprof.CLIFlags(EnvVarPrefix)...)

	Flags = append(requiredFlags, optionalFlags...)
}

var Flags []cli.Flag

func CheckRequired(ctx *cli.Context) error {
	for _, f := range requiredFlags {
		if !ctx.IsSet(f.Names()[0]) {
			return fmt.Errorf("flag %s is required", f.Names()[0])
		}
	}
	return nil
}
