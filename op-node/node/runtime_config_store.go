package node

import (
	rbt "github.com/emirpasic/gods/trees/redblacktree"
	"github.com/ethereum-optimism/optimism/op-service/eth"
	"github.com/ethereum/go-ethereum/params"
)

type RuntimeConfigData struct {
	l1Ref eth.L1BlockRef

	// System Config stored in L1
	sysCfg eth.SystemConfig

	// superchain protocol version signals
	recommended params.ProtocolVersion
	required    params.ProtocolVersion
}

func NewRuntimeConfigData(l1Ref eth.L1BlockRef, sysCfg eth.SystemConfig, recommended params.ProtocolVersion, required params.ProtocolVersion) RuntimeConfigData {
	return RuntimeConfigData{
		l1Ref:       l1Ref,
		sysCfg:      sysCfg,
		recommended: recommended,
		required:    required,
	}
}

type runtimeConfigStore interface {
	Add(v RuntimeConfigData)
	Get(k uint64) (RuntimeConfigData, bool)
	Latest() RuntimeConfigData
}

func NewRuntimeConfigStore(capacity int) *cachedRuntimeConfigStore {
	return &cachedRuntimeConfigStore{
		rbTree:   rbt.NewWith(UInt64Comparator),
		capacity: capacity,
	}
}

type cachedRuntimeConfigStore struct {
	rbTree   *rbt.Tree
	capacity int
}

// UInt64Comparator provides a basic comparison on uint64
func UInt64Comparator(a, b interface{}) int {
	aAsserted := a.(uint64)
	bAsserted := b.(uint64)
	switch {
	case aAsserted > bAsserted:
		return 1
	case aAsserted < bAsserted:
		return -1
	default:
		return 0
	}
}

func (store *cachedRuntimeConfigStore) getMinFromNode(node *rbt.Node) (foundNode *rbt.Node, found bool) {
	if node == nil {
		return nil, false
	}
	if node.Left == nil {
		return node, true
	}
	return store.getMinFromNode(node.Left)
}

func (store *cachedRuntimeConfigStore) getMaxFromNode(node *rbt.Node) (foundNode *rbt.Node, found bool) {
	if node == nil {
		return nil, false
	}
	if node.Right == nil {
		return node, true
	}
	return store.getMaxFromNode(node.Right)
}

func (store *cachedRuntimeConfigStore) removeMin() (value any, deleted bool) {
	node, found := store.getMinFromNode(store.rbTree.Root)
	if found {
		store.rbTree.Remove(node.Key)
		return node.Value, found
	}
	return nil, false
}

func (store *cachedRuntimeConfigStore) getMax() (value interface{}, found bool) {
	node, found := store.getMaxFromNode(store.rbTree.Root)
	if node != nil {
		return node.Value, found
	}
	return nil, false
}

var _ runtimeConfigStore = (*cachedRuntimeConfigStore)(nil)

func (store *cachedRuntimeConfigStore) Add(v RuntimeConfigData) {
	store.rbTree.Put(v.l1Ref.Number, v)
	if store.rbTree.Size() > store.capacity {
		store.removeMin()
	}
}

func (store *cachedRuntimeConfigStore) Get(k uint64) (RuntimeConfigData, bool) {
	val, found := store.rbTree.Get(k)
	if found {
		return val.(RuntimeConfigData), true
	}
	return RuntimeConfigData{}, false
}

func (store *cachedRuntimeConfigStore) Latest() RuntimeConfigData {
	val, _ := store.getMax()
	return val.(RuntimeConfigData)
}
