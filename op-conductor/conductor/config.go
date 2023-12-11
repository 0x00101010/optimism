package conductor

import (
	"fmt"

	"github.com/urfave/cli/v2"

	"github.com/ethereum-optimism/optimism/op-conductor/flags"
	oplog "github.com/ethereum-optimism/optimism/op-service/log"
	opmetrics "github.com/ethereum-optimism/optimism/op-service/metrics"
	oppprof "github.com/ethereum-optimism/optimism/op-service/pprof"
	oprpc "github.com/ethereum-optimism/optimism/op-service/rpc"
)

type CLIConfig struct {
	// ConsensusAddr is the address to listen for consensus connections.
	ConsensusAddr string

	// ConsensusPort is the port to listen for consensus connections.
	ConsensusPort int

	// RaftServerID is the unique ID for this server used by raft consensus.
	RaftServerID string

	// RaftStorageDIR is the directory to store raft data.
	RaftStorageDIR string

	// NodeRPC is the HTTP provider URL for op-node.
	NodeRPC string

	// BatcherRPC is the HTTP provider URL for op-batcher.
	BatcherRPC string

	LogConfig     oplog.CLIConfig
	MetricsConfig opmetrics.CLIConfig
	PprofConfig   oppprof.CLIConfig
	RPC           oprpc.CLIConfig
}

// Check validates the CLIConfig.
func (c *CLIConfig) Check() error {
	if c.ConsensusAddr == "" {
		return fmt.Errorf("missing consensus address")
	}
	if c.ConsensusPort == 0 {
		return fmt.Errorf("missing consensus port")
	}
	if c.RaftServerID == "" {
		return fmt.Errorf("missing raft server ID")
	}
	if c.RaftStorageDIR == "" {
		return fmt.Errorf("missing raft storage directory")
	}
	if c.NodeRPC == "" {
		return fmt.Errorf("missing node RPC")
	}
	if c.BatcherRPC == "" {
		return fmt.Errorf("missing batcher RPC")
	}
	if err := c.MetricsConfig.Check(); err != nil {
		return err
	}
	if err := c.PprofConfig.Check(); err != nil {
		return err
	}
	if err := c.RPC.Check(); err != nil {
		return err
	}
	return nil
}

// NewConfig parses the Config from the provided flags or environment variables.
func NewConfig(ctx *cli.Context) *CLIConfig {
	return &CLIConfig{
		ConsensusAddr:  ctx.String(flags.ConsensusAddr.Name),
		ConsensusPort:  ctx.Int(flags.ConsensusPort.Name),
		RaftServerID:   ctx.String(flags.RaftServerID.Name),
		RaftStorageDIR: ctx.String(flags.RaftStorageDIR.Name),
		NodeRPC:        ctx.String(flags.NodeRPC.Name),
		BatcherRPC:     ctx.String(flags.BatcherRPC.Name),
		LogConfig:      oplog.ReadCLIConfig(ctx),
		MetricsConfig:  opmetrics.ReadCLIConfig(ctx),
		PprofConfig:    oppprof.ReadCLIConfig(ctx),
		RPC:            oprpc.ReadCLIConfig(ctx),
	}
}
