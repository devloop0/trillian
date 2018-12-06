// Copyright 2016 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package log includes code that is specific to Trillian's log mode, particularly code
// for running sequencing operations.
package log

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"
	"sort"
	"errors"
	"github.com/golang/glog"
	"github.com/golang/protobuf/ptypes"
	"github.com/google/trillian"
	"github.com/google/trillian/merkle"
	"github.com/google/trillian/merkle/hashers"
	"github.com/google/trillian/monitoring"
	"github.com/google/trillian/quota"
	"github.com/google/trillian/storage"
	"github.com/google/trillian/types"
	"github.com/google/trillian/util"
	"github.com/google/trillian/networkSimulator"

	tcrypto "github.com/google/trillian/crypto"
)

const logIDLabel = "logid"

var (
	once                   sync.Once
	seqBatches             monitoring.Counter
	seqTreeSize            monitoring.Gauge
	seqLatency             monitoring.Histogram
	seqDequeueLatency      monitoring.Histogram
	seqGetRootLatency      monitoring.Histogram
	seqInitTreeLatency     monitoring.Histogram
	seqWriteTreeLatency    monitoring.Histogram
	seqUpdateLeavesLatency monitoring.Histogram
	seqSetNodesLatency     monitoring.Histogram
	seqStoreRootLatency    monitoring.Histogram
	seqCounter             monitoring.Counter
	seqMergeDelay          monitoring.Histogram

	// QuotaIncreaseFactor is the multiplier used for the number of tokens added back to
	// sequencing-based quotas. The resulting PutTokens call is equivalent to
	// "PutTokens(_, numLeaves * QuotaIncreaseFactor, _)".
	// A factor >1 adds resilience to token leakage, on the risk of a system that's overly
	// optimistic in face of true token shortages. The higher the factor, the higher the quota
	// "optimism" is. A factor that's too high (say, >1.5) is likely a sign that the quota
	// configuration should be changed instead.
	// A factor <1 WILL lead to token shortages, therefore it'll be normalized to 1.
	QuotaIncreaseFactor = 1.1
)

// Nick's Structs

// Structure for holding the in memory data necessary for updating a transaction
type TransactionDetails struct {

        // Array which holds all of the actual leaves for a transaction that have
        // been fetched so far.
        Leaves []*trillian.LogLeaf

        // Array which holds the information necessary to dequeue all of the leaves
        // that have been fetched so far.
        DequeueInfo []interface{}

	// Holds the ID for the current transaction. Same data should be in the
	// leaves as well.
	TrxnID int64

        // Holds the size of the Transaction. If the capacity is 0 then its size.
        // is not currently known.
        Capacity uint
}


// Structure for storing in progress transaction data in memory.
type TransactionMemory struct {

        // Array which holds the details for any pending transactions
        Transactions []*TransactionDetails

        // Integer which stores how many of the most recent leaves are held
        // in memory but not deleted from the queue on disk
        Offset uint
}

// End of Nick's Structs


func quotaIncreaseFactor() float64 {
	if QuotaIncreaseFactor < 1 {
		QuotaIncreaseFactor = 1
		return 1
	}
	return QuotaIncreaseFactor
}

func createMetrics(mf monitoring.MetricFactory) {
	if mf == nil {
		mf = monitoring.InertMetricFactory{}
	}
	quota.InitMetrics(mf)
	seqBatches = mf.NewCounter("sequencer_batches", "Number of sequencer batch operations", logIDLabel)
	seqTreeSize = mf.NewGauge("sequencer_tree_size", "Size of Merkle tree", logIDLabel)
	seqLatency = mf.NewHistogram("sequencer_latency", "Latency of sequencer batch operation in seconds", logIDLabel)
	seqDequeueLatency = mf.NewHistogram("sequencer_latency_dequeue", "Latency of dequeue-leaves part of sequencer batch operation in seconds", logIDLabel)
	seqGetRootLatency = mf.NewHistogram("sequencer_latency_get_root", "Latency of get-root part of sequencer batch operation in seconds", logIDLabel)
	seqInitTreeLatency = mf.NewHistogram("sequencer_latency_init_tree", "Latency of init-tree part of sequencer batch operation in seconds", logIDLabel)
	seqWriteTreeLatency = mf.NewHistogram("sequencer_latency_write_tree", "Latency of write-tree part of sequencer batch operation in seconds", logIDLabel)
	seqUpdateLeavesLatency = mf.NewHistogram("sequencer_latency_update_leaves", "Latency of update-leaves part of sequencer batch operation in seconds", logIDLabel)
	seqSetNodesLatency = mf.NewHistogram("sequencer_latency_set_nodes", "Latency of set-nodes part of sequencer batch operation in seconds", logIDLabel)
	seqStoreRootLatency = mf.NewHistogram("sequencer_latency_store_root", "Latency of store-root part of sequencer batch operation in seconds", logIDLabel)
	seqCounter = mf.NewCounter("sequencer_sequenced", "Number of leaves sequenced", logIDLabel)
	seqMergeDelay = mf.NewHistogram("sequencer_merge_delay", "Delay between queuing and integration of leaves", logIDLabel)
}

// Sequencer instances are responsible for integrating new leaves into a single log.
// Leaves will be assigned unique sequence numbers when they are processed.
// There is no strong ordering guarantee but in general entries will be processed
// in order of submission to the log.
type Sequencer struct {
	hasher     hashers.LogHasher
	timeSource util.TimeSource
	logStorage storage.LogStorage
	signer     *tcrypto.Signer
	qm         quota.Manager
}

// maxTreeDepth sets an upper limit on the size of Log trees.
// Note: We actually can't go beyond 2^63 entries because we use int64s,
// but we need to calculate tree depths from a multiple of 8 due to the
// subtree assumptions.
const maxTreeDepth = 64

// NewSequencer creates a new Sequencer instance for the specified inputs.
func NewSequencer(
	hasher hashers.LogHasher,
	timeSource util.TimeSource,
	logStorage storage.LogStorage,
	signer *tcrypto.Signer,
	mf monitoring.MetricFactory,
	qm quota.Manager) *Sequencer {
	once.Do(func() {
		createMetrics(mf)
	})
	return &Sequencer{
		hasher:     hasher,
		timeSource: timeSource,
		logStorage: logStorage,
		signer:     signer,
		qm:         qm,
	}
}

// TODO: This currently doesn't use the batch api for fetching the required nodes. This
// would be more efficient but requires refactoring.
func (s Sequencer) buildMerkleTreeFromStorageAtRoot(ctx context.Context, root *types.LogRootV1, tx storage.TreeTX) (*merkle.CompactMerkleTree, error) {
	mt, err := merkle.NewCompactMerkleTreeWithState(s.hasher, int64(root.TreeSize), func(depth int, index int64) ([]byte, error) {
		nodeID, err := storage.NewNodeIDForTreeCoords(int64(depth), index, maxTreeDepth)
		if err != nil {
			glog.Warningf("%x: Failed to create nodeID: %v", s.signer.KeyHint, err)
			return nil, err
		}
		nodes, err := tx.GetMerkleNodes(ctx, int64(root.Revision), []storage.NodeID{nodeID})

		if err != nil {
			glog.Warningf("%x: Failed to get Merkle nodes: %v", s.signer.KeyHint, err)
			return nil, err
		}

		// We expect to get exactly one node here
		if nodes == nil || len(nodes) != 1 {
			return nil, fmt.Errorf("%x: Did not retrieve one node while loading CompactMerkleTree, got %#v for ID %v@%v", s.signer.KeyHint, nodes, nodeID.String(), root.Revision)
		}

		return nodes[0].Hash, nil
	}, root.RootHash)

	return mt, err
}

func (s Sequencer) buildNodesFromNodeMap(nodeMap map[string]storage.Node, newVersion int64) ([]storage.Node, error) {
	targetNodes := make([]storage.Node, len(nodeMap))
	i := 0
	for _, node := range nodeMap {
		node.NodeRevision = newVersion
		targetNodes[i] = node
		i++
	}
	return targetNodes, nil
}

// Nick's Function(s)
func (s Sequencer) updateTransactionCompactTree(mt *merkle.CompactMerkleTree, leaves []*trillian.LogLeaf, label string, nodeMap map[string]storage.Node) (map[string]storage.Node, error) {
	// Update the tree state by integrating the leaves one by one.
	for _, leaf := range leaves {
		seq, err := mt.AddTransactionLeafHash(leaf.MerkleLeafHash, func(depth int, index int64, hash []byte) error {
			nodeID, err := storage.NewNodeIDForTreeCoords(int64(depth), index, maxTreeDepth)
			if err != nil {
				return err
			}
			nodeMap[nodeID.String()] = storage.Node{
				NodeID: nodeID,
				Hash:   hash,
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		// The leaf should already have the correct index before it's integrated.
		if leaf.LeafIndex != seq {
			return nil, fmt.Errorf("got invalid leaf index: %v, want: %v", leaf.LeafIndex, seq)
		}
		integrateTS := s.timeSource.Now()
		leaf.IntegrateTimestamp, err = ptypes.TimestampProto(integrateTS)
		if err != nil {
			return nil, fmt.Errorf("got invalid integrate timestamp: %v", err)
		}

		// Old leaves might not have a QueueTimestamp, only calculate the merge delay if this one does.
		if leaf.QueueTimestamp != nil && leaf.QueueTimestamp.Seconds != 0 {
			queueTS, err := ptypes.Timestamp(leaf.QueueTimestamp)
			if err != nil {
				return nil, fmt.Errorf("got invalid queue timestamp: %v", queueTS)
			}
			mergeDelay := integrateTS.Sub(queueTS)
			seqMergeDelay.Observe(mergeDelay.Seconds(), label)
		}

		/*
		// Store leaf hash in the Merkle tree too:
		leafNodeID, err := storage.NewNodeIDForTreeCoords(0, seq, maxTreeDepth)
		if err != nil {
			return nil, err
		}
		nodeMap[leafNodeID.String()] = storage.Node{
			NodeID: leafNodeID,
			Hash:   leaf.MerkleLeafHash,
		}
		*/
	}

	return nodeMap, nil
}

// End of Nick's Function(s)

func (s Sequencer) updateCompactTree(mt *merkle.CompactMerkleTree, leaves []*trillian.LogLeaf, label string) (map[string]storage.Node, error) {
	nodeMap := make(map[string]storage.Node)
	// Update the tree state by integrating the leaves one by one.
	for _, leaf := range leaves {
		seq, err := mt.AddLeafHash(leaf.MerkleLeafHash, func(depth int, index int64, hash []byte) error {
			nodeID, err := storage.NewNodeIDForTreeCoords(int64(depth), index, maxTreeDepth)
			if err != nil {
				return err
			}
			nodeMap[nodeID.String()] = storage.Node{
				NodeID: nodeID,
				Hash:   hash,
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		// The leaf should already have the correct index before it's integrated.
		if leaf.LeafIndex != seq {
			return nil, fmt.Errorf("got invalid leaf index: %v, want: %v", leaf.LeafIndex, seq)
		}
		integrateTS := s.timeSource.Now()
		leaf.IntegrateTimestamp, err = ptypes.TimestampProto(integrateTS)
		if err != nil {
			return nil, fmt.Errorf("got invalid integrate timestamp: %v", err)
		}

		// Old leaves might not have a QueueTimestamp, only calculate the merge delay if this one does.
		if leaf.QueueTimestamp != nil && leaf.QueueTimestamp.Seconds != 0 {
			queueTS, err := ptypes.Timestamp(leaf.QueueTimestamp)
			if err != nil {
				return nil, fmt.Errorf("got invalid queue timestamp: %v", queueTS)
			}
			mergeDelay := integrateTS.Sub(queueTS)
			seqMergeDelay.Observe(mergeDelay.Seconds(), label)
		}

		/*
		// Store leaf hash in the Merkle tree too:
		leafNodeID, err := storage.NewNodeIDForTreeCoords(0, seq, maxTreeDepth)
		if err != nil {
			return nil, err
		}
		nodeMap[leafNodeID.String()] = storage.Node{
			NodeID: leafNodeID,
			Hash:   leaf.MerkleLeafHash,
		}
		*/
	}

	return nodeMap, nil
}

func (s Sequencer) initMerkleTreeFromStorage(ctx context.Context, currentRoot *types.LogRootV1, tx storage.LogTreeTX) (*merkle.CompactMerkleTree, error) {
	if currentRoot.TreeSize == 0 {
		return merkle.NewCompactMerkleTree(s.hasher), nil
	}

	// Initialize the compact tree state to match the latest root in the database
	return s.buildMerkleTreeFromStorageAtRoot(ctx, currentRoot, tx)
}

// sequencingTask provides sequenced LogLeaf entries, and updates storage
// according to their ordering if needed.
type sequencingTask interface {
	// fetch returns a batch of sequenced entries obtained from storage, sized up
	// to the specified limit. The returned leaves have consecutive LeafIndex
	// values starting from the current tree size.
	fetch(ctx context.Context, limit int, cutoff time.Time) ([]*trillian.LogLeaf, error)

	// Function used for fetching sequenced entries for those using trnasactions
	fetchTransaction(ctx context.Context, tree *trillian.Tree, limit int, cutoff time.Time, transactionCache *TransactionMemory) ([]*trillian.LogLeaf, int, error)


	// update makes sequencing persisted in storage, if not yet.
	update(ctx context.Context, leaves []*trillian.LogLeaf) error
}

type sequencingTaskData struct {
	label      string
	treeSize   int64
	timeSource util.TimeSource
	tx         storage.LogTreeTX
}

// logSequencingTask is a sequencingTask implementation for "normal" Log mode,
// which assigns consecutive sequence numbers to leaves as they are read from
// the pending unsequenced entries.
type logSequencingTask sequencingTaskData

func (s *logSequencingTask) fetch(ctx context.Context, limit int, cutoff time.Time) ([]*trillian.LogLeaf, error) {
	start := s.timeSource.Now()
	// Recent leaves inside the guard window will not be available for sequencing.
	leaves, err := s.tx.DequeueLeaves(ctx, limit, cutoff)
	if err != nil {
		glog.Warningf("%v: Sequencer failed to dequeue leaves: %v", s.label, err)
		return nil, err
	}
	seqDequeueLatency.Observe(util.SecondsSince(s.timeSource, start), s.label)

	// Assign leaf sequence numbers.
	for i, leaf := range leaves {
		leaf.LeafIndex = s.treeSize + int64(i)
	}
	return leaves, nil
}

// Nick's Functions

func extractCompletedTransactions(ctx context.Context, tree *trillian.Tree, transactionCache *TransactionMemory, limit int, s *logSequencingTask) ([]*trillian.LogLeaf, []interface{}, []int64, int, error) {
	var leaves []*trillian.LogLeaf
	var queueIDs []interface{}
	var trxnIDs []int64
	for i := 0; i < len (transactionCache.Transactions) && limit > 0; {
		transaction := transactionCache.Transactions[i]
		//glog.Warningf("extractCompletedTransactions: Transaction %v", transaction)
		if transaction.Capacity == 0 {
			data, err := s.tx.GetInProgressTransaction (ctx, tree.TreeId, transaction.TrxnID)
			if err != nil {
				return nil, nil, nil, 0, err
			}
			transaction.Capacity = uint (data.NodeCount)
		}
		if uint(len (transaction.Leaves)) == transaction.Capacity {
			leaves = append (leaves, transaction.Leaves...)
			queueIDs = append (queueIDs, transaction.DequeueInfo...)
			trxnIDs = append (trxnIDs, transaction.TrxnID)
			transactionCache.Offset -= transaction.Capacity
			limit -= 1
			endPoint := len (transactionCache.Transactions) - 1
			if endPoint == i {
				i++
			} else {
				transactionCache.Transactions[i], transactionCache.Transactions[endPoint] = transactionCache.Transactions[endPoint], transactionCache.Transactions[i]
			}
			transactionCache.Transactions = transactionCache.Transactions[:endPoint]
		} else {
			i++
		}
	}
	return leaves, queueIDs, trxnIDs, limit, nil
}

func updateTransactionMemory (transactionCache *TransactionMemory, leaves []*trillian.LogLeaf, queueIDs []interface{}) {
	for j, leaf := range leaves {
		unplaced := true
		id := leaf.TransactionId
		for i := 0; i < len (transactionCache.Transactions) && unplaced; i++ {
			if (id == transactionCache.Transactions[i].TrxnID) {
				trxn := transactionCache.Transactions[i]
				unplaced = false
				trxn.Leaves = append (trxn.Leaves, leaf)
				trxn.DequeueInfo = append (trxn.DequeueInfo, queueIDs[j])
			}
		}
		if unplaced {
			trxn := &TransactionDetails{}
			trxn.TrxnID = id
			trxn.Leaves = append (trxn.Leaves, leaf)
			trxn.DequeueInfo = append (trxn.DequeueInfo, queueIDs[j])
			trxn.Capacity = 0
			transactionCache.Transactions = append (transactionCache.Transactions, trxn)
		}
	}
	transactionCache.Offset += uint(len(leaves))
}

func determineLeavesToFetch (transactionCache *TransactionMemory, limit int) int64 {
	_lambda := int64(2) //heuristic size for unknown transactions
	size := int64(len(transactionCache.Transactions))
	var total int64
	if int64(limit) < size {
		leavesLeft := make ([]int64, len(transactionCache.Transactions))
		for i, trxn := range transactionCache.Transactions {
			leavesLeft[i] = int64(trxn.Capacity) - int64(len(trxn.Leaves))
		}
		sort.Slice (leavesLeft, func(i, j int) bool {return leavesLeft[i] < leavesLeft[j]})
		leavesLeft = leavesLeft[:limit]
		for _, amount := range leavesLeft {
			total += amount
		}
		return total
	} else {
		for _, trxn := range transactionCache.Transactions {
			total += int64(trxn.Capacity) - int64(len(trxn.Leaves))
		}
		return total + ((int64 (limit) - size) * _lambda)
	}
}

func (s *logSequencingTask) fetchTransaction(ctx context.Context, tree *trillian.Tree, limit int, cutoff time.Time, transactionCache *TransactionMemory) ([]*trillian.LogLeaf, int, error) {
	keepFetching := false

	// Add a check to see if any transactions are already done.
	leaves, queueIDs, trxnIDs, limit, err := extractCompletedTransactions (ctx, tree, transactionCache, limit, s)
	if err != nil {
		glog.Warningf("Failed to check if any transactions are already done.")
		return nil, 0, err
	}

	// Check how many additional transactions need to be fetched.
	if (limit != 0) {
		keepFetching = true
	}

	for ; keepFetching; {
		leavesRemaining := determineLeavesToFetch (transactionCache, limit)

		start := s.timeSource.Now()
		// Recent leaves inside the guard window will not be available for sequencing.
		leafNodes, leafIDs, err := s.tx.GetQueuedLeavesRange(ctx, int(transactionCache.Offset), int(leavesRemaining), cutoff)
		if err != nil {
			glog.Warningf("%v: Sequencer failed to dequeue leaves: %v", s.label, err)
			return nil, 0, err
		}
		seqDequeueLatency.Observe(util.SecondsSince(s.timeSource, start), s.label)
		dequeueIDs := leafIDs
		// Place the updated leaves in the correct location
		updateTransactionMemory (transactionCache, leafNodes, dequeueIDs)

		// Check if we are done
		tempLeaves, tempQueueIDs, tempTrxnIDs, limit, err := extractCompletedTransactions (ctx, tree, transactionCache, limit, s)
		if err != nil {
			glog.Warningf("Failed to check if we are already done after completing some transactions.")
			return nil, 0, err
		}

		leaves = append (leaves, tempLeaves...)
		queueIDs = append (queueIDs, tempQueueIDs...)
		trxnIDs = append (trxnIDs, tempTrxnIDs...)
		if limit == 0 || len(leafNodes) < int(leavesRemaining) {
			glog.Warningf("fetchTransaction (limit: %d) (len(leafNodes): %d), (int(leaveRemaining): %d)", limit, len(leafNodes), int(leavesRemaining))
			keepFetching = false
		}
	}
	// Remove any pending transactions and leaves
	err = s.tx.RemoveQueuedLeaves (ctx, queueIDs)
	if err != nil {
		glog.Warningf("Failed to remove any pending transactions and leaves.")
		glog.Warningf("%v", err)
		return nil, 0, err
	}
	for _, trxnID := range trxnIDs {
		err = s.tx.DeleteInProgressTransaction (ctx, tree.TreeId, trxnID)
		if err != nil {
			glog.Warningf("Failed to remove transactions from the InProgress table.")
			return nil, 0, err
		}
	}

	// Assign leaf sequence numbers.
	for i, leaf := range leaves {
		leaf.LeafIndex = s.treeSize + int64(i)
	}
	return leaves, len (trxnIDs), nil
}

// End of Nick's functions

func (s *logSequencingTask) update(ctx context.Context, leaves []*trillian.LogLeaf) error {
	start := s.timeSource.Now()
	// Write the new sequence numbers to the leaves in the DB.
	if err := s.tx.UpdateSequencedLeaves(ctx, leaves); err != nil {
		glog.Warningf("%v: Sequencer failed to update sequenced leaves: %v", s.label, err)
		return err
	}
	seqUpdateLeavesLatency.Observe(util.SecondsSince(s.timeSource, start), s.label)
	return nil
}

// preorderedLogSequencingTask is a sequencingTask implementation for
// Pre-ordered Log mode. It reads sequenced entries past the tree size which
// are already in the storage.
type preorderedLogSequencingTask sequencingTaskData

func (s *preorderedLogSequencingTask) fetch(ctx context.Context, limit int, cutoff time.Time) ([]*trillian.LogLeaf, error) {
	start := s.timeSource.Now()
	leaves, err := s.tx.DequeueLeaves(ctx, limit, cutoff)
	if err != nil {
		glog.Warningf("%v: Sequencer failed to load sequenced leaves: %v", s.label, err)
		return nil, err
	}
	seqDequeueLatency.Observe(util.SecondsSince(s.timeSource, start), s.label)
	return leaves, nil
}


func (s *preorderedLogSequencingTask) fetchTransaction(ctx context.Context, tree *trillian.Tree, limit int, cutoff time.Time, transactionCache *TransactionMemory) ([]*trillian.LogLeaf, int, error) {
	return nil, 0, errors.New("Preordered Logs are not supported for transactions.")
}

func (s *preorderedLogSequencingTask) update(ctx context.Context, leaves []*trillian.LogLeaf) error {
	// TODO(pavelkalinnikov): Update integration timestamps.
	return nil
}

// Nick's Function(s)
func (s Sequencer) IntegrateTransactionBatch(ctx context.Context, tree *trillian.Tree, limit int, guardWindow, maxRootDurationInterval time.Duration, ignoreBatchSize, useTrxns bool, TransactionCache *TransactionMemory) (int, error) {
	start := s.timeSource.Now()
	label := strconv.FormatInt(tree.TreeId, 10)

	numLeaves := 0
	var newLogRoot *types.LogRootV1
	var newSLR *trillian.SignedLogRoot
	err := s.logStorage.ReadWriteTransaction(ctx, tree, func(ctx context.Context, tx storage.LogTreeTX) error {
		stageStart := s.timeSource.Now()
		defer seqBatches.Inc(label)
		defer func() { seqLatency.Observe(util.SecondsSince(s.timeSource, start), label) }()

		// Get the latest known root from storage
		sth, err := tx.LatestSignedLogRoot(ctx)
		if err != nil {
			glog.Warningf("%v: Sequencer failed to get latest root: %v", tree.TreeId, err)
			return err
		}
		// There is no trust boundary between the signer and the
		// database, so we skip signature verification.
		// TODO(gbelvin): Add signature checking as a santity check.
		var currentRoot types.LogRootV1
		if err := currentRoot.UnmarshalBinary(sth.LogRoot); err != nil {
			glog.Warningf("%v: Sequencer failed to unmarshal latest root: %v", tree.TreeId, err)
			return err
		}
		seqGetRootLatency.Observe(util.SecondsSince(s.timeSource, stageStart), label)

		if currentRoot.RootHash == nil {
			glog.Warningf("%v: Fresh log - no previous TreeHeads exist.", tree.TreeId)
			return storage.ErrTreeNeedsInit
		}

		taskData := &sequencingTaskData{
			label:      label,
			treeSize:   int64(currentRoot.TreeSize),
			timeSource: s.timeSource,
			tx:         tx,
		}
		var st sequencingTask
		var merkleTree *merkle.CompactMerkleTree
		var newVersion int64
		switch tree.TreeType {
		case trillian.TreeType_LOG:
			st = (*logSequencingTask)(taskData)
		case trillian.TreeType_PREORDERED_LOG:
			st = (*preorderedLogSequencingTask)(taskData)
		default:
			return fmt.Errorf("IntegrateBatch not supported for TreeType %v", tree.TreeType)
		}
		// Change the bounds later
		keepFetching := true
		hasUpdate := false
		nodeMap := make(map[string]storage.Node)
		counter := 10
		for  ; keepFetching ; {
			var sequencedLeaves []*trillian.LogLeaf
			var total int
			if useTrxns {
				sequencedLeaves, total, err = st.fetchTransaction (ctx, tree, limit, start.Add(-guardWindow), TransactionCache)
				if err != nil {
					glog.Warningf("%v: Sequencer failed to load sequenced batch: %v", tree.TreeId, err)
					return err
				}
			} else {
				sequencedLeaves, err = st.fetch(ctx, limit, start.Add(-guardWindow))
				if err != nil {
					glog.Warningf("%v: Sequencer failed to load sequenced batch: %v", tree.TreeId, err)
					return err
				}
				total = len(sequencedLeaves)
			}
			tempLeaves := len(sequencedLeaves)
			if ignoreBatchSize && counter > 0 {
				counter -= 1
			} else {
				limit -= total
			}
			numLeaves += tempLeaves
			keepFetching = limit > 0
			// Break out of the fetching loop when we run out of leaves
			if tempLeaves == 0 {
				break
			}
			if (!hasUpdate) {
				stageStart = s.timeSource.Now()
				merkleTree, err = s.initMerkleTreeFromStorage(ctx, &currentRoot, tx)
				if err != nil {
					glog.Warningf("%v: Sequencer unable to load a tree: %v", tree.TreeId, err)
					return err
				}
				seqInitTreeLatency.Observe(util.SecondsSince(s.timeSource, stageStart), label)
				stageStart = s.timeSource.Now()
				hasUpdate = true
				newVersion, err = tx.WriteRevision(ctx)
				if err != nil {
					glog.Warningf("%v: Sequencer unable to get a new write version: %v", tree.TreeId, err)
					return err
				}
				// We've done all the reads, can now do the updates in the same transaction.
				// The schema should prevent multiple STHs being inserted with the same
				// revision number so it should not be possible for colliding updates to
				// commit.
				if got, want := newVersion, int64(currentRoot.Revision)+1; got != want {
					return fmt.Errorf("%v: got writeRevision of %v, but expected %v", tree.TreeId, got, want)
				}
			}

			// FIX ME. Need to do partial updates based upon nodes read so far. Also
			// want a way of tracking how much has been completed. Most likely this
			// can be inferred from tree size but will need another method to complete
			// the update

			// Collate node updates.
			nodeMap, err = s.updateTransactionCompactTree(merkleTree, sequencedLeaves, label, nodeMap)
			if err != nil {
				glog.Warningf("%v: Sequencer unable to update tree: %v", tree.TreeId, err)
				return err
			}
			seqWriteTreeLatency.Observe(util.SecondsSince(s.timeSource, stageStart), label)
			// Store the sequenced batch.
			// Should require no changes as only updates index number.
			if err := st.update(ctx, sequencedLeaves); err != nil {
				glog.Warningf("%v: Sequencer unable to update indices: %v", tree.TreeId, err)
				return err
			}
		}
		if (!hasUpdate) {
			glog.V(1).Infof("%v: No leaves sequenced in this signing operation", tree.TreeId)
			return nil
		}

		err = merkleTree.UpdateTransactionRoot(func(depth int, index int64, hash []byte) error {
			nodeID, err := storage.NewNodeIDForTreeCoords(int64(depth), index, maxTreeDepth)
			if err != nil {
				return err
			}
			nodeMap[nodeID.String()] = storage.Node{
				NodeID: nodeID,
				Hash:   hash,
			}
			return nil
		})
		if err != nil {
			glog.Warningf("%v: Sequencer unable to update root: %v", tree.TreeId, err)
			return err
		}

		stageStart = s.timeSource.Now()
		// Build objects for the nodes to be updated. Because we deduped via the map
		// each node can only be created / updated once in each tree revision and
		// they cannot conflict when we do the storage update.
		// Should require no change
		targetNodes, err := s.buildNodesFromNodeMap(nodeMap, newVersion)
		if err != nil {
			// Probably an internal error with map building, unexpected.
			glog.Warningf("%v: Failed to build target nodes in sequencer: %v", tree.TreeId, err)
			return err
		}

		// Now insert or update the nodes affected by the above, at the new tree
		// version.
		if err := tx.SetMerkleNodes(ctx, targetNodes); err != nil {
			glog.Warningf("%v: Sequencer failed to set Merkle nodes: %v", tree.TreeId, err)
			return err
		}
		seqSetNodesLatency.Observe(util.SecondsSince(s.timeSource, stageStart), label)
		stageStart = s.timeSource.Now()

		// Create the log root ready for signing
		seqTreeSize.Set(float64(merkleTree.Size()), label)
		newLogRoot = &types.LogRootV1{
			RootHash:       merkleTree.CurrentRoot(),
			TimestampNanos: uint64(s.timeSource.Now().UnixNano()),
			TreeSize:       uint64(merkleTree.Size()),
			Revision:       uint64(newVersion),
		}

		if newLogRoot.TimestampNanos <= currentRoot.TimestampNanos {
			err := fmt.Errorf("refusing to sign root with timestamp earlier than previous root (%d <= %d)", newLogRoot.TimestampNanos, currentRoot.TimestampNanos)
			glog.Warningf("%v: %s", tree.TreeId, err)
			return err
		}

		newSLR, err = s.signer.SignLogRoot(newLogRoot)
		if err != nil {
			glog.Warningf("%v: signer failed to sign root: %v", tree.TreeId, err)
			return err
		}

		if err := tx.StoreSignedLogRoot(ctx, *newSLR); err != nil {
			glog.Warningf("%v: failed to write updated tree root: %v", tree.TreeId, err)
			return err
		}
		seqStoreRootLatency.Observe(util.SecondsSince(s.timeSource, stageStart), label)

		return nil
	})
	if err != nil {
		return 0, err
	}
	NetworkSimulator.GenerateWriteSQLDelay()

	// Let quota.Manager know about newly-sequenced entries.
	// All possibly influenced quotas are replenished: {Tree/Global, Read/Write}.
	// Implementations are tasked with filtering quotas that shouldn't be replenished.
	// TODO(codingllama): Consider adding a source-aware replenish method
	// (eg, qm.Replenish(ctx, tokens, specs, quota.SequencerSource)), so there's no ambiguity as to
	// where the tokens come from.
	if numLeaves > 0 {
		tokens := int(float64(numLeaves) * quotaIncreaseFactor())
		specs := []quota.Spec{
			{Group: quota.Tree, Kind: quota.Read, TreeID: tree.TreeId},
			{Group: quota.Tree, Kind: quota.Write, TreeID: tree.TreeId},
			{Group: quota.Global, Kind: quota.Read},
			{Group: quota.Global, Kind: quota.Write},
		}
		glog.V(2).Infof("%v: Replenishing %v tokens (numLeaves = %v)", tree.TreeId, tokens, numLeaves)
		err := s.qm.PutTokens(ctx, tokens, specs)
		if err != nil {
			glog.Warningf("%v: Failed to replenish %v tokens: %v", tree.TreeId, tokens, err)
		}
		quota.Metrics.IncReplenished(tokens, specs, err == nil)
	}

	seqCounter.Add(float64(numLeaves), label)
	if newSLR != nil {
		glog.Infof("%v: sequenced %v leaves, size %v, tree-revision %v", tree.TreeId, numLeaves, newLogRoot.TreeSize, newLogRoot.Revision)
	}
	return numLeaves, nil
}
// End of Nick's Function(s)

// IntegrateBatch wraps up all the operations needed to take a batch of queued
// or sequenced leaves and integrate them into the tree.
func (s Sequencer) IntegrateBatch(ctx context.Context, tree *trillian.Tree, limit int, guardWindow, maxRootDurationInterval time.Duration) (int, error) {
	start := s.timeSource.Now()
	label := strconv.FormatInt(tree.TreeId, 10)

	numLeaves := 0
	var newLogRoot *types.LogRootV1
	var newSLR *trillian.SignedLogRoot
	err := s.logStorage.ReadWriteTransaction(ctx, tree, func(ctx context.Context, tx storage.LogTreeTX) error {
		stageStart := s.timeSource.Now()
		defer seqBatches.Inc(label)
		defer func() { seqLatency.Observe(util.SecondsSince(s.timeSource, start), label) }()

		// Get the latest known root from storage
		sth, err := tx.LatestSignedLogRoot(ctx)
		if err != nil {
			glog.Warningf("%v: Sequencer failed to get latest root: %v", tree.TreeId, err)
			return err
		}
		// There is no trust boundary between the signer and the
		// database, so we skip signature verification.
		// TODO(gbelvin): Add signature checking as a santity check.
		var currentRoot types.LogRootV1
		if err := currentRoot.UnmarshalBinary(sth.LogRoot); err != nil {
			glog.Warningf("%v: Sequencer failed to unmarshal latest root: %v", tree.TreeId, err)
			return err
		}
		seqGetRootLatency.Observe(util.SecondsSince(s.timeSource, stageStart), label)

		if currentRoot.RootHash == nil {
			glog.Warningf("%v: Fresh log - no previous TreeHeads exist.", tree.TreeId)
			return storage.ErrTreeNeedsInit
		}

		taskData := &sequencingTaskData{
			label:      label,
			treeSize:   int64(currentRoot.TreeSize),
			timeSource: s.timeSource,
			tx:         tx,
		}
		var st sequencingTask
		switch tree.TreeType {
		case trillian.TreeType_LOG:
			st = (*logSequencingTask)(taskData)
		case trillian.TreeType_PREORDERED_LOG:
			st = (*preorderedLogSequencingTask)(taskData)
		default:
			return fmt.Errorf("IntegrateBatch not supported for TreeType %v", tree.TreeType)
		}

		sequencedLeaves, err := st.fetch(ctx, limit, start.Add(-guardWindow))
		if err != nil {
			glog.Warningf("%v: Sequencer failed to load sequenced batch: %v", tree.TreeId, err)
			return err
		}
		numLeaves = len(sequencedLeaves)

		// We need to create a signed root if entries were added or the latest root
		// is too old.
		if numLeaves == 0 {
			nowNanos := s.timeSource.Now().UnixNano()
			interval := time.Duration(nowNanos - int64(currentRoot.TimestampNanos))
			if maxRootDurationInterval == 0 || interval < maxRootDurationInterval {
				// We have nothing to integrate into the tree.
				glog.V(1).Infof("%v: No leaves sequenced in this signing operation", tree.TreeId)
				return nil
			}
			glog.Infof("%v: Force new root generation as %v since last root", tree.TreeId, interval)
		}

		stageStart = s.timeSource.Now()
		merkleTree, err := s.initMerkleTreeFromStorage(ctx, &currentRoot, tx)
		if err != nil {
			return err
		}
		seqInitTreeLatency.Observe(util.SecondsSince(s.timeSource, stageStart), label)
		stageStart = s.timeSource.Now()

		// We've done all the reads, can now do the updates in the same transaction.
		// The schema should prevent multiple STHs being inserted with the same
		// revision number so it should not be possible for colliding updates to
		// commit.
		newVersion, err := tx.WriteRevision(ctx)
		if err != nil {
			return err
		}
		if got, want := newVersion, int64(currentRoot.Revision)+1; got != want {
			return fmt.Errorf("%v: got writeRevision of %v, but expected %v", tree.TreeId, got, want)
		}

		// Collate node updates.
		nodeMap, err := s.updateCompactTree(merkleTree, sequencedLeaves, label)
		if err != nil {
			return err
		}
		seqWriteTreeLatency.Observe(util.SecondsSince(s.timeSource, stageStart), label)

		// Store the sequenced batch.
		if err := st.update(ctx, sequencedLeaves); err != nil {
			return err
		}
		stageStart = s.timeSource.Now()

		// Build objects for the nodes to be updated. Because we deduped via the map
		// each node can only be created / updated once in each tree revision and
		// they cannot conflict when we do the storage update.
		targetNodes, err := s.buildNodesFromNodeMap(nodeMap, newVersion)
		if err != nil {
			// Probably an internal error with map building, unexpected.
			glog.Warningf("%v: Failed to build target nodes in sequencer: %v", tree.TreeId, err)
			return err
		}

		// Now insert or update the nodes affected by the above, at the new tree
		// version.
		if err := tx.SetMerkleNodes(ctx, targetNodes); err != nil {
			glog.Warningf("%v: Sequencer failed to set Merkle nodes: %v", tree.TreeId, err)
			return err
		}
		seqSetNodesLatency.Observe(util.SecondsSince(s.timeSource, stageStart), label)
		stageStart = s.timeSource.Now()

		// Create the log root ready for signing
		seqTreeSize.Set(float64(merkleTree.Size()), label)
		newLogRoot = &types.LogRootV1{
			RootHash:       merkleTree.CurrentRoot(),
			TimestampNanos: uint64(s.timeSource.Now().UnixNano()),
			TreeSize:       uint64(merkleTree.Size()),
			Revision:       uint64(newVersion),
		}

		if newLogRoot.TimestampNanos <= currentRoot.TimestampNanos {
			err := fmt.Errorf("refusing to sign root with timestamp earlier than previous root (%d <= %d)", newLogRoot.TimestampNanos, currentRoot.TimestampNanos)
			glog.Warningf("%v: %s", tree.TreeId, err)
			return err
		}

		newSLR, err = s.signer.SignLogRoot(newLogRoot)
		if err != nil {
			glog.Warningf("%v: signer failed to sign root: %v", tree.TreeId, err)
			return err
		}

		if err := tx.StoreSignedLogRoot(ctx, *newSLR); err != nil {
			glog.Warningf("%v: failed to write updated tree root: %v", tree.TreeId, err)
			return err
		}
		seqStoreRootLatency.Observe(util.SecondsSince(s.timeSource, stageStart), label)

		return nil
	})
	if err != nil {
		return 0, err
	}
	NetworkSimulator.GenerateWriteSQLDelay()

	// Let quota.Manager know about newly-sequenced entries.
	// All possibly influenced quotas are replenished: {Tree/Global, Read/Write}.
	// Implementations are tasked with filtering quotas that shouldn't be replenished.
	// TODO(codingllama): Consider adding a source-aware replenish method
	// (eg, qm.Replenish(ctx, tokens, specs, quota.SequencerSource)), so there's no ambiguity as to
	// where the tokens come from.
	if numLeaves > 0 {
		tokens := int(float64(numLeaves) * quotaIncreaseFactor())
		specs := []quota.Spec{
			{Group: quota.Tree, Kind: quota.Read, TreeID: tree.TreeId},
			{Group: quota.Tree, Kind: quota.Write, TreeID: tree.TreeId},
			{Group: quota.Global, Kind: quota.Read},
			{Group: quota.Global, Kind: quota.Write},
		}
		glog.V(2).Infof("%v: Replenishing %v tokens (numLeaves = %v)", tree.TreeId, tokens, numLeaves)
		err := s.qm.PutTokens(ctx, tokens, specs)
		if err != nil {
			glog.Warningf("%v: Failed to replenish %v tokens: %v", tree.TreeId, tokens, err)
		}
		quota.Metrics.IncReplenished(tokens, specs, err == nil)
	}

	seqCounter.Add(float64(numLeaves), label)
	if newSLR != nil {
		glog.Infof("%v: sequenced %v leaves, size %v, tree-revision %v", tree.TreeId, numLeaves, newLogRoot.TreeSize, newLogRoot.Revision)
	}
	return numLeaves, nil
}

// SignRoot wraps up all the operations for creating a new log signed root.
func (s Sequencer) SignRoot(ctx context.Context, tree *trillian.Tree) error {
	return s.logStorage.ReadWriteTransaction(ctx, tree, func(ctx context.Context, tx storage.LogTreeTX) error {
		// Get the latest known root from storage
		sth, err := tx.LatestSignedLogRoot(ctx)
		if err != nil {
			glog.Warningf("%v: signer failed to get latest root: %v", tree.TreeId, err)
			return err
		}
		var currentRoot types.LogRootV1
		if err := currentRoot.UnmarshalBinary(sth.LogRoot); err != nil {
			glog.Warningf("%v: Sequencer failed to unmarshal latest root: %v", tree.TreeId, err)
			return err
		}

		// Initialize a Merkle Tree from the state in storage. This should fail if the tree is
		// in a corrupt state.
		merkleTree, err := s.initMerkleTreeFromStorage(ctx, &currentRoot, tx)
		if err != nil {
			return err
		}
		newLogRoot := &types.LogRootV1{
			RootHash:       merkleTree.CurrentRoot(),
			TimestampNanos: uint64(s.timeSource.Now().UnixNano()),
			TreeSize:       uint64(merkleTree.Size()),
			Revision:       currentRoot.Revision + 1,
		}
		newSLR, err := s.signer.SignLogRoot(newLogRoot)
		if err != nil {
			glog.Warningf("%v: signer failed to sign root: %v", tree.TreeId, err)
			return err
		}

		// Store the new root and we're done
		if err := tx.StoreSignedLogRoot(ctx, *newSLR); err != nil {
			glog.Warningf("%v: signer failed to write updated root: %v", tree.TreeId, err)
			return err
		}
		glog.V(2).Infof("%v: new signed root, size %v, tree-revision %v", tree.TreeId, newLogRoot.TreeSize, newLogRoot.Revision)

		return nil
	})
}
