package node

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"

	"github.com/ethereum-optimism/optimism/op-node/p2p"
	"github.com/ethereum-optimism/optimism/op-node/rollup"
	"github.com/ethereum-optimism/optimism/op-service/eth"
)

var (
	// UnsafeBlockSignerAddressSystemConfigStorageSlot is the storage slot identifier of the unsafeBlockSigner
	// `address` storage value in the SystemConfig L1 contract. Computed as `keccak256("systemconfig.unsafeblocksigner")`
	UnsafeBlockSignerAddressSystemConfigStorageSlot = common.HexToHash("0x65a7ed542fb37fe237fdfbdd70b31598523fe5b32879e307bae27a0bd9581c08")

	// RequiredProtocolVersionStorageSlot is the storage slot that the required protocol version is stored at.
	// Computed as: `bytes32(uint256(keccak256("protocolversion.required")) - 1)`
	RequiredProtocolVersionStorageSlot = common.HexToHash("0x4aaefe95bd84fd3f32700cf3b7566bc944b73138e41958b5785826df2aecace0")

	// RecommendedProtocolVersionStorageSlot is the storage slot that the recommended protocol version is stored at.
	// Computed as: `bytes32(uint256(keccak256("protocolversion.recommended")) - 1)`
	RecommendedProtocolVersionStorageSlot = common.HexToHash("0xe314dfc40f0025322aacc0ba8ef420b62fb3b702cf01e0cdf3d829117ac2ff1a")
)

type ProtocolVersionUpdateListener func(ctx context.Context, required params.ProtocolVersion, recommended params.ProtocolVersion) error

type RuntimeCfgL1Source interface {
	ReadStorageAt(ctx context.Context, address common.Address, storageSlot common.Hash, blockHash common.Hash) (common.Hash, error)
	L1BlockRefByNumber(ctx context.Context, num uint64) (eth.L1BlockRef, error)
}

type ReadonlyRuntimeConfig interface {
	// Returns the latest P2P sequencer address in the config.
	P2PSequencerAddress() common.Address
	// Returns latest required protocol version.
	RequiredProtocolVersion() params.ProtocolVersion
	// Returns latest recommended protocol version.
	RecommendedProtocolVersion() params.ProtocolVersion
}

// RuntimeConfig maintains runtime-configurable options.
// These options are loaded based on initial loading + updates for every subsequent L1 block.
// Only the *latest* values are maintained however, the runtime config has no concept of chain history,
// does not require any archive data, and may be out of sync with the rollup derivation process.
type RuntimeConfig struct {
	mu sync.RWMutex

	log log.Logger

	l1Client  RuntimeCfgL1Source
	rollupCfg *rollup.Config

	store             runtimeConfigStore
	initialized       bool
	verifierConfDepth uint64
	pvuListener       ProtocolVersionUpdateListener
}

func NewRuntimeConfig(
	log log.Logger,
	l1Client RuntimeCfgL1Source,
	rollupCfg *rollup.Config,
	verifierConfDepth uint64,
	pvu ProtocolVersionUpdateListener,
) *RuntimeConfig {
	return &RuntimeConfig{
		log:       log,
		l1Client:  l1Client,
		rollupCfg: rollupCfg,
		// TODO: make it configurable
		store:             NewRuntimeConfigStore(100),
		initialized:       false,
		verifierConfDepth: verifierConfDepth,
		pvuListener:       pvu,
	}
}

func (r *RuntimeConfig) Initialize(ctx context.Context, l1Ref eth.L1BlockRef) error {
	if err := r.OnNewL1Block(ctx, l1Ref); err != nil {
		return err
	}
	r.initialized = true
	return nil
}

var _ p2p.GossipRuntimeConfig = (*RuntimeConfig)(nil)
var _ ReadonlyRuntimeConfig = (*RuntimeConfig)(nil)

// P2PSequencerAddress implements p2p.GossipRuntimeConfig.
func (r *RuntimeConfig) P2PSequencerAddress() common.Address {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.store.Latest().sysCfg.UnsafeBlockSigner
}

// P2PSequencerAddressByL1BlockNumber implements p2p.GossipRuntimeConfig.
func (r *RuntimeConfig) P2PSequencerAddressByL1BlockNumber(blkNum uint64) (common.Address, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	val, existed := r.store.Get(blkNum)
	return val.sysCfg.UnsafeBlockSigner, existed
}

// RecommendedProtocolVersion implements ReadonlyRuntimeConfig.
func (r *RuntimeConfig) RecommendedProtocolVersion() params.ProtocolVersion {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.store.Latest().recommended
}

// RequiredProtocolVersion implements ReadonlyRuntimeConfig.
func (r *RuntimeConfig) RequiredProtocolVersion() params.ProtocolVersion {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.store.Latest().required
}

func (r *RuntimeConfig) OnNewL1Block(ctx context.Context, l1Ref eth.L1BlockRef) error {
	// Apply confirmation depth
	blkNum := l1Ref.Number
	if blkNum >= r.verifierConfDepth {
		blkNum -= r.verifierConfDepth
	}

	fetchCtx, fetchCancel := context.WithTimeout(ctx, time.Second*10)
	confirmed, err := r.l1Client.L1BlockRefByNumber(fetchCtx, blkNum)
	fetchCancel()
	if err != nil {
		r.log.Error("failed to fetch confirmed L1 block for runtime config loading", "err", err, "number", blkNum)
		return err
	}

	unsafeBlockSigner, err := r.l1Client.ReadStorageAt(ctx, r.rollupCfg.L1SystemConfigAddress, UnsafeBlockSignerAddressSystemConfigStorageSlot, confirmed.Hash)
	if err != nil {
		return fmt.Errorf("failed to fetch unsafe block signing address from system config: %w", err)
	}
	// The superchain protocol version data is optional; only applicable to rollup configs that specify a ProtocolVersions address.
	var requiredProtVersion, recommendedProtoVersion params.ProtocolVersion
	if r.rollupCfg.ProtocolVersionsAddress != (common.Address{}) {
		requiredVal, err := r.l1Client.ReadStorageAt(ctx, r.rollupCfg.ProtocolVersionsAddress, RequiredProtocolVersionStorageSlot, confirmed.Hash)
		if err != nil {
			return fmt.Errorf("required-protocol-version value failed to load from L1 contract: %w", err)
		}
		requiredProtVersion = params.ProtocolVersion(requiredVal)
		recommendedVal, err := r.l1Client.ReadStorageAt(ctx, r.rollupCfg.ProtocolVersionsAddress, RecommendedProtocolVersionStorageSlot, confirmed.Hash)
		if err != nil {
			return fmt.Errorf("recommended-protocol-version value failed to load from L1 contract: %w", err)
		}
		recommendedProtoVersion = params.ProtocolVersion(recommendedVal)
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	runCfgData := NewRuntimeConfigData(
		l1Ref,
		eth.SystemConfig{
			UnsafeBlockSigner: common.BytesToAddress(unsafeBlockSigner[:]),
			// Read other configs if needed.
		},
		recommendedProtoVersion,
		requiredProtVersion,
	)
	r.store.Add(runCfgData)

	r.log.Info(
		"loaded new runtime config values!",
		"p2p_seq_address", runCfgData.sysCfg.UnsafeBlockSigner,
		"recommended_protocol_version", recommendedProtoVersion,
		"required_protocol_version", requiredProtVersion,
	)
	return r.pvuListener(ctx, requiredProtVersion, recommendedProtoVersion)
}
