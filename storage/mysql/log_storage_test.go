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

package mysql

import (
	"bytes"
	"context"
	"crypto"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/google/trillian"
	"github.com/google/trillian/storage"
	"github.com/google/trillian/storage/testonly"
	"github.com/google/trillian/types"
	"github.com/kylelemons/godebug/pretty"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	tcrypto "github.com/google/trillian/crypto"
	ttestonly "github.com/google/trillian/testonly"

	_ "github.com/go-sql-driver/mysql"
)

var allTables = []string{"Unsequenced", "TreeHead", "SequencedLeafData", "LeafData", "Subtree", "TreeControl", "Trees", "MapLeaf", "MapHead", "PublicKeyMaps"}

// Must be 32 bytes to match sha256 length if it was a real hash
var dummyHash = []byte("hashxxxxhashxxxxhashxxxxhashxxxx")
var dummyRawHash = []byte("xxxxhashxxxxhashxxxxhashxxxxhash")
var dummyRawHash2 = []byte("yyyyhashyyyyhashyyyyhashyyyyhash")
var dummyHash2 = []byte("HASHxxxxhashxxxxhashxxxxhashxxxx")
var dummyHash3 = []byte("hashxxxxhashxxxxhashxxxxHASHxxxx")

// Time we will queue all leaves at
var fakeQueueTime = time.Date(2016, 11, 10, 15, 16, 27, 0, time.UTC)

// Time we will integrate all leaves at
var fakeIntegrateTime = time.Date(2016, 11, 10, 15, 16, 30, 0, time.UTC)

// Time we'll request for guard cutoff in tests that don't test this (should include all above)
var fakeDequeueCutoffTime = time.Date(2016, 11, 10, 15, 16, 30, 0, time.UTC)

// Used for tests involving extra data
var someExtraData = []byte("Some extra data")
var someExtraData2 = []byte("Some even more extra data")

const leavesToInsert = 5
const sequenceNumber int64 = 237

//Nick's added tests
func createFakeAddMap(ctx context.Context, db *sql.DB, TreeId int64, UserId string, PublicKey string, Identifiers string, Identity string, t *testing.T) {
	_, err := db.ExecContext(ctx, insertUserMap, TreeId, UserId, PublicKey, Identifiers, Identity)
	if err != nil {
		t.Fatalf("Failed to create test element: %v", err)
	}
}

// Tests that access the db should each use a distinct log ID to prevent lock contention when
// run in parallel or race conditions / unexpected interactions. Tests that pass should hold
// no locks afterwards.

func createFakeLeaf(ctx context.Context, db *sql.DB, logID int64, rawHash, hash, data, extraData []byte, seq int64, t *testing.T) *trillian.LogLeaf {
	t.Helper()
	queuedAtNanos := fakeQueueTime.UnixNano()
	integratedAtNanos := fakeIntegrateTime.UnixNano()
	_, err := db.ExecContext(ctx, "INSERT INTO LeafData(TreeId, LeafIdentityHash, LeafValue, ExtraData, QueueTimestampNanos) VALUES(?,?,?,?,?)", logID, rawHash, data, extraData, queuedAtNanos)
	_, err2 := db.ExecContext(ctx, "INSERT INTO SequencedLeafData(TreeId, SequenceNumber, LeafIdentityHash, MerkleLeafHash, IntegrateTimestampNanos) VALUES(?,?,?,?,?)", logID, seq, rawHash, hash, integratedAtNanos)

	if err != nil || err2 != nil {
		t.Fatalf("Failed to create test leaves: %v %v", err, err2)
	}
	queueTimestamp, err := ptypes.TimestampProto(fakeQueueTime)
	if err != nil {
		panic(err)
	}
	integrateTimestamp, err := ptypes.TimestampProto(fakeIntegrateTime)
	if err != nil {
		panic(err)
	}
	return &trillian.LogLeaf{
		MerkleLeafHash:     hash,
		LeafValue:          data,
		ExtraData:          extraData,
		LeafIndex:          seq,
		LeafIdentityHash:   rawHash,
		QueueTimestamp:     queueTimestamp,
		IntegrateTimestamp: integrateTimestamp,
	}
}

func checkLeafContents(leaf *trillian.LogLeaf, seq int64, rawHash, hash, data, extraData []byte, t *testing.T) {
	t.Helper()
	if got, want := leaf.MerkleLeafHash, hash; !bytes.Equal(got, want) {
		t.Fatalf("Wrong leaf hash in returned leaf got\n%v\nwant:\n%v", got, want)
	}

	if got, want := leaf.LeafIdentityHash, rawHash; !bytes.Equal(got, want) {
		t.Fatalf("Wrong raw leaf hash in returned leaf got\n%v\nwant:\n%v", got, want)
	}

	if got, want := seq, leaf.LeafIndex; got != want {
		t.Fatalf("Bad sequence number in returned leaf got: %d, want:%d", got, want)
	}

	if got, want := leaf.LeafValue, data; !bytes.Equal(got, want) {
		t.Fatalf("Unxpected data in returned leaf. got:\n%v\nwant:\n%v", got, want)
	}

	if got, want := leaf.ExtraData, extraData; !bytes.Equal(got, want) {
		t.Fatalf("Unxpected data in returned leaf. got:\n%v\nwant:\n%v", got, want)
	}

	iTime, err := ptypes.Timestamp(leaf.IntegrateTimestamp)
	if err != nil {
		t.Fatalf("Got invalid integrate timestamp: %v", err)
	}
	if got, want := iTime.UnixNano(), fakeIntegrateTime.UnixNano(); got != want {
		t.Errorf("Wrong IntegrateTimestamp: got %v, want %v", got, want)
	}
}

func TestMySQLLogStorage_CheckDatabaseAccessible(t *testing.T) {
	cleanTestDB(DB)
	s := NewLogStorage(DB, nil)
	if err := s.CheckDatabaseAccessible(context.Background()); err != nil {
		t.Errorf("CheckDatabaseAccessible() = %v, want = nil", err)
	}
}

func TestSnapshot(t *testing.T) {
	cleanTestDB(DB)

	frozenLog := createTreeOrPanic(DB, testonly.LogTree)
	createFakeSignedLogRoot(DB, frozenLog, 0)
	if _, err := updateTree(DB, frozenLog.TreeId, func(tree *trillian.Tree) {
		tree.TreeState = trillian.TreeState_FROZEN
	}); err != nil {
		t.Fatalf("Error updating frozen tree: %v", err)
	}

	activeLog := createTreeOrPanic(DB, testonly.LogTree)
	createFakeSignedLogRoot(DB, activeLog, 0)
	mapTreeID := createTreeOrPanic(DB, testonly.MapTree).TreeId

	tests := []struct {
		desc    string
		tree    *trillian.Tree
		wantErr bool
	}{
		{
			desc:    "unknownSnapshot",
			tree:    logTree(-1),
			wantErr: true,
		},
		{
			desc: "activeLogSnapshot",
			tree: activeLog,
		},
		{
			desc: "frozenSnapshot",
			tree: frozenLog,
		},
		{
			desc:    "mapSnapshot",
			tree:    logTree(mapTreeID),
			wantErr: true,
		},
	}

	ctx := context.Background()
	s := NewLogStorage(DB, nil)
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			tx, err := s.SnapshotForTree(ctx, test.tree)

			if err == storage.ErrTreeNeedsInit {
				defer tx.Close()
			}

			if hasErr := err != nil; hasErr != test.wantErr {
				t.Fatalf("err = %q, wantErr = %v", err, test.wantErr)
			} else if hasErr {
				return
			}
			defer tx.Close()

			_, err = tx.LatestSignedLogRoot(ctx)
			if err != nil {
				t.Errorf("LatestSignedLogRoot() returned err = %v", err)
			}
			if err := tx.Commit(); err != nil {
				t.Errorf("Commit() returned err = %v", err)
			}
		})
	}
}

func TestReadWriteTransaction(t *testing.T) {
	cleanTestDB(DB)
	activeLog := createTreeOrPanic(DB, testonly.LogTree)
	createFakeSignedLogRoot(DB, activeLog, 0)

	tests := []struct {
		desc        string
		tree        *trillian.Tree
		wantErr     bool
		wantLogRoot []byte
		wantTXRev   int64
	}{
		{
			// Unknown logs IDs are now handled outside storage.
			desc:        "unknownBegin",
			tree:        logTree(-1),
			wantLogRoot: nil,
			wantTXRev:   -1,
		},
		{
			desc: "activeLogBegin",
			tree: activeLog,
			wantLogRoot: func() []byte {
				b, err := (&types.LogRootV1{RootHash: []byte{0}}).MarshalBinary()
				if err != nil {
					panic(err)
				}
				return b
			}(),
			wantTXRev: 1,
		},
	}

	ctx := context.Background()
	s := NewLogStorage(DB, nil)
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			err := s.ReadWriteTransaction(ctx, test.tree, func(ctx context.Context, tx storage.LogTreeTX) error {
				root, err := tx.LatestSignedLogRoot(ctx)
				if err != nil {
					t.Fatalf("%v: LatestSignedLogRoot() returned err = %v", test.desc, err)
				}
				gotRev, _ := tx.WriteRevision(ctx)
				if gotRev != test.wantTXRev {
					t.Errorf("%v: WriteRevision() = %v, want = %v", test.desc, gotRev, test.wantTXRev)
				}
				if got, want := root.LogRoot, test.wantLogRoot; !bytes.Equal(got, want) {
					t.Errorf("%v: LogRoot: \n%x, want \n%x", test.desc, got, want)
				}
				return nil
			})
			if hasErr := err != nil; hasErr != test.wantErr {
				t.Fatalf("%v: err = %q, wantErr = %v", test.desc, err, test.wantErr)
			} else if hasErr {
				return
			}
		})
	}
}

func TestQueueDuplicateLeaf(t *testing.T) {
	cleanTestDB(DB)
	tree := createTreeOrPanic(DB, testonly.LogTree)
	s := NewLogStorage(DB, nil)
	count := 15
	leaves := createTestLeaves(int64(count), 10)
	leaves2 := createTestLeaves(int64(count), 12)
	leaves3 := createTestLeaves(3, 100)

	// Note that tests accumulate queued leaves on top of each other.
	var tests = []struct {
		desc   string
		leaves []*trillian.LogLeaf
		want   []*trillian.LogLeaf
	}{
		{
			desc:   "[10, 11, 12, ...]",
			leaves: leaves,
			want:   make([]*trillian.LogLeaf, count),
		},
		{
			desc:   "[12, 13, 14, ...] so first (count-2) are duplicates",
			leaves: leaves2,
			want:   append(leaves[2:], nil, nil),
		},
		{
			desc:   "[10, 100, 11, 101, 102] so [dup, new, dup, new, dup]",
			leaves: []*trillian.LogLeaf{leaves[0], leaves3[0], leaves[1], leaves3[1], leaves[2]},
			want:   []*trillian.LogLeaf{leaves[0], nil, leaves[1], nil, leaves[2]},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			runLogTX(s, tree, t, func(ctx context.Context, tx storage.LogTreeTX) error {
				existing, err := tx.QueueLeaves(ctx, test.leaves, fakeQueueTime)
				if err != nil {
					t.Errorf("Failed to queue leaves: %v", err)
					return err
				}

				if len(existing) != len(test.want) {
					t.Fatalf("|QueueLeaves()|=%d; want %d", len(existing), len(test.want))
				}
				for i, want := range test.want {
					got := existing[i]
					if want == nil {
						if got != nil {
							t.Fatalf("QueueLeaves()[%d]=%v; want nil", i, got)
						}
						return nil
					}
					if got == nil {
						t.Fatalf("QueueLeaves()[%d]=nil; want non-nil", i)
					} else if !bytes.Equal(got.LeafIdentityHash, want.LeafIdentityHash) {
						t.Fatalf("QueueLeaves()[%d].LeafIdentityHash=%x; want %x", i, got.LeafIdentityHash, want.LeafIdentityHash)
					}
				}
				return nil
			})
		})
	}
}

func TestQueueLeaves(t *testing.T) {
	ctx := context.Background()

	cleanTestDB(DB)
	tree := createTreeOrPanic(DB, testonly.LogTree)
	s := NewLogStorage(DB, nil)

	runLogTX(s, tree, t, func(ctx context.Context, tx storage.LogTreeTX) error {
		leaves := createTestLeaves(leavesToInsert, 20)
		if _, err := tx.QueueLeaves(ctx, leaves, fakeQueueTime); err != nil {
			t.Fatalf("Failed to queue leaves: %v", err)
		}
		return nil
	})

	// Should see the leaves in the database. There is no API to read from the unsequenced data.
	var count int
	if err := DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM Unsequenced WHERE TreeID=?", tree.TreeId).Scan(&count); err != nil {
		t.Fatalf("Could not query row count: %v", err)
	}
	if leavesToInsert != count {
		t.Fatalf("Expected %d unsequenced rows but got: %d", leavesToInsert, count)
	}

	// Additional check on timestamp being set correctly in the database
	var queueTimestamp int64
	if err := DB.QueryRowContext(ctx, "SELECT DISTINCT QueueTimestampNanos FROM Unsequenced WHERE TreeID=?", tree.TreeId).Scan(&queueTimestamp); err != nil {
		t.Fatalf("Could not query timestamp: %v", err)
	}
	if got, want := queueTimestamp, fakeQueueTime.UnixNano(); got != want {
		t.Fatalf("Incorrect queue timestamp got: %d want: %d", got, want)
	}
}

// AddSequencedLeaves tests. ---------------------------------------------------

type addSequencedLeavesTest struct {
	t    *testing.T
	s    storage.LogStorage
	tree *trillian.Tree
}

func initAddSequencedLeavesTest(t *testing.T) addSequencedLeavesTest {
	cleanTestDB(DB)
	s := NewLogStorage(DB, nil)
	tree := createTreeOrPanic(DB, testonly.PreorderedLogTree)
	return addSequencedLeavesTest{t, s, tree}
}

func (t *addSequencedLeavesTest) addSequencedLeaves(leaves []*trillian.LogLeaf) {
	runLogTX(t.s, t.tree, t.t, func(ctx context.Context, tx storage.LogTreeTX) error {
		if _, err := tx.AddSequencedLeaves(ctx, leaves, fakeQueueTime); err != nil {
			t.t.Fatalf("Failed to add sequenced leaves: %v", err)
		}
		// TODO(pavelkalinnikov): Verify returned status for each leaf.
		return nil
	})
}

func (t *addSequencedLeavesTest) verifySequencedLeaves(start, count int64, exp []*trillian.LogLeaf) {
	var stored []*trillian.LogLeaf
	runLogTX(t.s, t.tree, t.t, func(ctx context.Context, tx storage.LogTreeTX) error {
		var err error
		stored, err = tx.GetLeavesByRange(ctx, start, count)
		if err != nil {
			t.t.Fatalf("Failed to read sequenced leaves: %v", err)
		}
		return nil
	})
	if got, want := len(stored), len(exp); got != want {
		t.t.Fatalf("Unexpected number of leaves: got %d, want %d", got, want)
	}

	for i, leaf := range stored {
		if got, want := leaf.LeafIndex, exp[i].LeafIndex; got != want {
			t.t.Fatalf("Leaf #%d: LeafIndex=%v, want %v", i, got, want)
		}
		if got, want := leaf.LeafIdentityHash, exp[i].LeafIdentityHash; !bytes.Equal(got, want) {
			t.t.Fatalf("Leaf #%d: LeafIdentityHash=%v, want %v", i, got, want)
		}
	}
}

func TestAddSequencedLeavesUnordered(t *testing.T) {
	const chunk = leavesToInsert
	const count = chunk * 5
	const extraCount = 16
	leaves := createTestLeaves(count, 0)

	aslt := initAddSequencedLeavesTest(t)
	for _, idx := range []int{1, 0, 4, 2} {
		aslt.addSequencedLeaves(leaves[chunk*idx : chunk*(idx+1)])
	}
	aslt.verifySequencedLeaves(0, count+extraCount, leaves[:chunk*3])
	aslt.verifySequencedLeaves(chunk*4, chunk+extraCount, leaves[chunk*4:count])
	aslt.addSequencedLeaves(leaves[chunk*3 : chunk*4])
	aslt.verifySequencedLeaves(0, count+extraCount, leaves)
}

func TestAddSequencedLeavesWithDuplicates(t *testing.T) {
	leaves := createTestLeaves(6, 0)

	aslt := initAddSequencedLeavesTest(t)
	aslt.addSequencedLeaves(leaves[:3])
	aslt.verifySequencedLeaves(0, 3, leaves[:3])
	aslt.addSequencedLeaves(leaves[2:]) // Full dup.
	aslt.verifySequencedLeaves(0, 6, leaves)

	dupLeaves := createTestLeaves(4, 6)
	dupLeaves[0].LeafIdentityHash = leaves[0].LeafIdentityHash // Hash dup.
	dupLeaves[2].LeafIndex = 2                                 // Index dup.
	aslt.addSequencedLeaves(dupLeaves)
	aslt.verifySequencedLeaves(6, 4, nil)
	aslt.verifySequencedLeaves(7, 4, dupLeaves[1:2])
	aslt.verifySequencedLeaves(8, 4, nil)
	aslt.verifySequencedLeaves(9, 4, dupLeaves[3:4])

	dupLeaves = createTestLeaves(4, 6)
	aslt.addSequencedLeaves(dupLeaves)
	aslt.verifySequencedLeaves(6, 4, dupLeaves)
}

// -----------------------------------------------------------------------------

func TestDequeueLeavesNoneQueued(t *testing.T) {
	cleanTestDB(DB)
	tree := createTreeOrPanic(DB, testonly.LogTree)
	s := NewLogStorage(DB, nil)

	runLogTX(s, tree, t, func(ctx context.Context, tx storage.LogTreeTX) error {
		leaves, err := tx.DequeueLeaves(ctx, 999, fakeDequeueCutoffTime)
		if err != nil {
			t.Fatalf("Didn't expect an error on dequeue with no work to be done: %v", err)
		}
		if len(leaves) > 0 {
			t.Fatalf("Expected nothing to be dequeued but we got %d leaves", len(leaves))
		}
		return nil
	})
}

func TestDequeueLeaves(t *testing.T) {
	cleanTestDB(DB)
	tree := createTreeOrPanic(DB, testonly.LogTree)
	s := NewLogStorage(DB, nil)

	{
		runLogTX(s, tree, t, func(ctx context.Context, tx storage.LogTreeTX) error {
			leaves := createTestLeaves(leavesToInsert, 20)
			if _, err := tx.QueueLeaves(ctx, leaves, fakeDequeueCutoffTime); err != nil {
				t.Fatalf("Failed to queue leaves: %v", err)
			}
			return nil
		})
	}

	{
		// Now try to dequeue them
		runLogTX(s, tree, t, func(ctx context.Context, tx2 storage.LogTreeTX) error {
			leaves2, err := tx2.DequeueLeaves(ctx, 99, fakeDequeueCutoffTime)
			if err != nil {
				t.Fatalf("Failed to dequeue leaves: %v", err)
			}
			if len(leaves2) != leavesToInsert {
				t.Fatalf("Dequeued %d leaves but expected to get %d", len(leaves2), leavesToInsert)
			}
			ensureAllLeavesDistinct(leaves2, t)
			return nil
		})
	}

	{
		// If we dequeue again then we should now get nothing
		runLogTX(s, tree, t, func(ctx context.Context, tx3 storage.LogTreeTX) error {
			leaves3, err := tx3.DequeueLeaves(ctx, 99, fakeDequeueCutoffTime)
			if err != nil {
				t.Fatalf("Failed to dequeue leaves (second time): %v", err)
			}
			if len(leaves3) != 0 {
				t.Fatalf("Dequeued %d leaves but expected to get none", len(leaves3))
			}
			return nil
		})
	}
}

func TestDequeueLeavesHaveQueueTimestamp(t *testing.T) {
	cleanTestDB(DB)
	tree := createTreeOrPanic(DB, testonly.LogTree)
	s := NewLogStorage(DB, nil)

	{
		runLogTX(s, tree, t, func(ctx context.Context, tx storage.LogTreeTX) error {
			leaves := createTestLeaves(leavesToInsert, 20)
			if _, err := tx.QueueLeaves(ctx, leaves, fakeDequeueCutoffTime); err != nil {
				t.Fatalf("Failed to queue leaves: %v", err)
			}
			return nil
		})
	}

	{
		// Now try to dequeue them
		runLogTX(s, tree, t, func(ctx context.Context, tx2 storage.LogTreeTX) error {
			leaves2, err := tx2.DequeueLeaves(ctx, 99, fakeDequeueCutoffTime)
			if err != nil {
				t.Fatalf("Failed to dequeue leaves: %v", err)
			}
			if len(leaves2) != leavesToInsert {
				t.Fatalf("Dequeued %d leaves but expected to get %d", len(leaves2), leavesToInsert)
			}
			ensureLeavesHaveQueueTimestamp(t, leaves2, fakeDequeueCutoffTime)
			return nil
		})
	}
}

func TestDequeueLeavesTwoBatches(t *testing.T) {
	cleanTestDB(DB)
	tree := createTreeOrPanic(DB, testonly.LogTree)
	s := NewLogStorage(DB, nil)

	leavesToDequeue1 := 3
	leavesToDequeue2 := 2

	{
		runLogTX(s, tree, t, func(ctx context.Context, tx storage.LogTreeTX) error {
			leaves := createTestLeaves(leavesToInsert, 20)
			if _, err := tx.QueueLeaves(ctx, leaves, fakeDequeueCutoffTime); err != nil {
				t.Fatalf("Failed to queue leaves: %v", err)
			}
			return nil
		})
	}

	var err error
	var leaves2, leaves3, leaves4 []*trillian.LogLeaf
	{
		// Now try to dequeue some of them
		runLogTX(s, tree, t, func(ctx context.Context, tx2 storage.LogTreeTX) error {
			leaves2, err = tx2.DequeueLeaves(ctx, leavesToDequeue1, fakeDequeueCutoffTime)
			if err != nil {
				t.Fatalf("Failed to dequeue leaves: %v", err)
			}
			if len(leaves2) != leavesToDequeue1 {
				t.Fatalf("Dequeued %d leaves but expected to get %d", len(leaves2), leavesToInsert)
			}
			ensureAllLeavesDistinct(leaves2, t)
			ensureLeavesHaveQueueTimestamp(t, leaves2, fakeDequeueCutoffTime)
			return nil
		})

		// Now try to dequeue the rest of them
		runLogTX(s, tree, t, func(ctx context.Context, tx3 storage.LogTreeTX) error {
			leaves3, err = tx3.DequeueLeaves(ctx, leavesToDequeue2, fakeDequeueCutoffTime)
			if err != nil {
				t.Fatalf("Failed to dequeue leaves: %v", err)
			}
			if len(leaves3) != leavesToDequeue2 {
				t.Fatalf("Dequeued %d leaves but expected to get %d", len(leaves3), leavesToDequeue2)
			}
			ensureAllLeavesDistinct(leaves3, t)
			ensureLeavesHaveQueueTimestamp(t, leaves3, fakeDequeueCutoffTime)

			// Plus the union of the leaf batches should all have distinct hashes
			leaves4 = append(leaves2, leaves3...)
			ensureAllLeavesDistinct(leaves4, t)
			return nil
		})
	}

	{
		// If we dequeue again then we should now get nothing
		runLogTX(s, tree, t, func(ctx context.Context, tx4 storage.LogTreeTX) error {
			leaves5, err := tx4.DequeueLeaves(ctx, 99, fakeDequeueCutoffTime)
			if err != nil {
				t.Fatalf("Failed to dequeue leaves (second time): %v", err)
			}
			if len(leaves5) != 0 {
				t.Fatalf("Dequeued %d leaves but expected to get none", len(leaves5))
			}
			return nil
		})
	}
}

// Queues leaves and attempts to dequeue before the guard cutoff allows it. This should
// return nothing. Then retry with an inclusive guard cutoff and ensure the leaves
// are returned.
func TestDequeueLeavesGuardInterval(t *testing.T) {
	cleanTestDB(DB)
	tree := createTreeOrPanic(DB, testonly.LogTree)
	s := NewLogStorage(DB, nil)

	{
		runLogTX(s, tree, t, func(ctx context.Context, tx storage.LogTreeTX) error {
			leaves := createTestLeaves(leavesToInsert, 20)
			if _, err := tx.QueueLeaves(ctx, leaves, fakeQueueTime); err != nil {
				t.Fatalf("Failed to queue leaves: %v", err)
			}
			return nil
		})
	}

	{
		// Now try to dequeue them using a cutoff that means we should get none
		runLogTX(s, tree, t, func(ctx context.Context, tx2 storage.LogTreeTX) error {
			leaves2, err := tx2.DequeueLeaves(ctx, 99, fakeQueueTime.Add(-time.Second))
			if err != nil {
				t.Fatalf("Failed to dequeue leaves: %v", err)
			}
			if len(leaves2) != 0 {
				t.Fatalf("Dequeued %d leaves when they all should be in guard interval", len(leaves2))
			}

			// Try to dequeue again using a cutoff that should include them
			leaves2, err = tx2.DequeueLeaves(ctx, 99, fakeQueueTime.Add(time.Second))
			if err != nil {
				t.Fatalf("Failed to dequeue leaves: %v", err)
			}
			if len(leaves2) != leavesToInsert {
				t.Fatalf("Dequeued %d leaves but expected to get %d", len(leaves2), leavesToInsert)
			}
			ensureAllLeavesDistinct(leaves2, t)
			return nil
		})
	}
}

func TestDequeueLeavesTimeOrdering(t *testing.T) {
	// Queue two small batches of leaves at different timestamps. Do two separate dequeue
	// transactions and make sure the returned leaves are respecting the time ordering of the
	// queue.
	cleanTestDB(DB)
	tree := createTreeOrPanic(DB, testonly.LogTree)
	s := NewLogStorage(DB, nil)

	batchSize := 2
	leaves := createTestLeaves(int64(batchSize), 0)
	leaves2 := createTestLeaves(int64(batchSize), int64(batchSize))

	{
		runLogTX(s, tree, t, func(ctx context.Context, tx storage.LogTreeTX) error {
			if _, err := tx.QueueLeaves(ctx, leaves, fakeQueueTime); err != nil {
				t.Fatalf("QueueLeaves(1st batch) = %v", err)
			}
			// These are one second earlier so should be dequeued first
			if _, err := tx.QueueLeaves(ctx, leaves2, fakeQueueTime.Add(-time.Second)); err != nil {
				t.Fatalf("QueueLeaves(2nd batch) = %v", err)
			}
			return nil
		})
	}

	{
		// Now try to dequeue two leaves and we should get the second batch
		runLogTX(s, tree, t, func(ctx context.Context, tx2 storage.LogTreeTX) error {
			dequeue1, err := tx2.DequeueLeaves(ctx, batchSize, fakeQueueTime)
			if err != nil {
				t.Fatalf("DequeueLeaves(1st) = %v", err)
			}
			if got, want := len(dequeue1), batchSize; got != want {
				t.Fatalf("Dequeue count mismatch (1st) got: %d, want: %d", got, want)
			}
			ensureAllLeavesDistinct(dequeue1, t)

			// Ensure this is the second batch queued by comparing leaf hashes (must be distinct as
			// the leaf data was).
			if !leafInBatch(dequeue1[0], leaves2) || !leafInBatch(dequeue1[1], leaves2) {
				t.Fatalf("Got leaf from wrong batch (1st dequeue): %v", dequeue1)
			}
			return nil
		})

		// Try to dequeue again and we should get the batch that was queued first, though at a later time
		runLogTX(s, tree, t, func(ctx context.Context, tx3 storage.LogTreeTX) error {
			dequeue2, err := tx3.DequeueLeaves(ctx, batchSize, fakeQueueTime)
			if err != nil {
				t.Fatalf("DequeueLeaves(2nd) = %v", err)
			}
			if got, want := len(dequeue2), batchSize; got != want {
				t.Fatalf("Dequeue count mismatch (2nd) got: %d, want: %d", got, want)
			}
			ensureAllLeavesDistinct(dequeue2, t)

			// Ensure this is the first batch by comparing leaf hashes.
			if !leafInBatch(dequeue2[0], leaves) || !leafInBatch(dequeue2[1], leaves) {
				t.Fatalf("Got leaf from wrong batch (2nd dequeue): %v", dequeue2)
			}
			return nil
		})
	}
}

func TestGetLeavesByHashNotPresent(t *testing.T) {
	cleanTestDB(DB)
	tree := createTreeOrPanic(DB, testonly.LogTree)
	s := NewLogStorage(DB, nil)

	runLogTX(s, tree, t, func(ctx context.Context, tx storage.LogTreeTX) error {
		hashes := [][]byte{[]byte("thisdoesn'texist")}
		leaves, err := tx.GetLeavesByHash(ctx, hashes, false)
		if err != nil {
			t.Fatalf("Error getting leaves by hash: %v", err)
		}
		if len(leaves) != 0 {
			t.Fatalf("Expected no leaves returned but got %d", len(leaves))
		}
		return nil
	})
}

func TestGetLeavesByHash(t *testing.T) {
	ctx := context.Background()

	// Create fake leaf as if it had been sequenced
	cleanTestDB(DB)
	tree := createTreeOrPanic(DB, testonly.LogTree)
	s := NewLogStorage(DB, nil)

	data := []byte("some data")
	createFakeLeaf(ctx, DB, tree.TreeId, dummyRawHash, dummyHash, data, someExtraData, sequenceNumber, t)

	runLogTX(s, tree, t, func(ctx context.Context, tx storage.LogTreeTX) error {
		hashes := [][]byte{dummyHash}
		leaves, err := tx.GetLeavesByHash(ctx, hashes, false)
		if err != nil {
			t.Fatalf("Unexpected error getting leaf by hash: %v", err)
		}
		if len(leaves) != 1 {
			t.Fatalf("Got %d leaves but expected one", len(leaves))
		}
		checkLeafContents(leaves[0], sequenceNumber, dummyRawHash, dummyHash, data, someExtraData, t)
		return nil
	})
}

func TestGetLeafDataByIdentityHash(t *testing.T) {
	ctx := context.Background()

	// Create fake leaf as if it had been sequenced
	cleanTestDB(DB)
	tree := createTreeOrPanic(DB, testonly.LogTree)
	s := NewLogStorage(DB, nil)
	data := []byte("some data")
	leaf := createFakeLeaf(ctx, DB, tree.TreeId, dummyRawHash, dummyHash, data, someExtraData, sequenceNumber, t)
	leaf.LeafIndex = -1
	leaf.MerkleLeafHash = []byte(dummyMerkleLeafHash)
	leaf2 := createFakeLeaf(ctx, DB, tree.TreeId, dummyHash2, dummyHash2, data, someExtraData, sequenceNumber+1, t)
	leaf2.LeafIndex = -1
	leaf2.MerkleLeafHash = []byte(dummyMerkleLeafHash)

	var tests = []struct {
		hashes [][]byte
		want   []*trillian.LogLeaf
	}{
		{
			hashes: [][]byte{dummyRawHash},
			want:   []*trillian.LogLeaf{leaf},
		},
		{
			hashes: [][]byte{{0x01, 0x02}},
		},
		{
			hashes: [][]byte{
				dummyRawHash,
				{0x01, 0x02},
				dummyHash2,
				{0x01, 0x02},
			},
			// Note: leaves not necessarily returned in order requested.
			want: []*trillian.LogLeaf{leaf2, leaf},
		},
	}
	for i, test := range tests {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			runLogTX(s, tree, t, func(ctx context.Context, tx storage.LogTreeTX) error {
				leaves, err := tx.(*logTreeTX).getLeafDataByIdentityHash(ctx, test.hashes)
				if err != nil {
					t.Fatalf("getLeavesByIdentityHash(_) = (_,%v); want (_,nil)", err)
				}

				if len(leaves) != len(test.want) {
					t.Fatalf("getLeavesByIdentityHash(_) = (|%d|,nil); want (|%d|,nil)", len(leaves), len(test.want))
				}
				leavesEquivalent(t, leaves, test.want)
				return nil
			})
		})
	}
}

func leavesEquivalent(t *testing.T, gotLeaves, wantLeaves []*trillian.LogLeaf) {
	t.Helper()
	want := make(map[string]*trillian.LogLeaf)
	for _, w := range wantLeaves {
		k := sha256.Sum256([]byte(w.String()))
		want[string(k[:])] = w
	}
	got := make(map[string]*trillian.LogLeaf)
	for _, g := range gotLeaves {
		k := sha256.Sum256([]byte(g.String()))
		got[string(k[:])] = g
	}
	if diff := pretty.Compare(want, got); diff != "" {
		t.Errorf("leaves not equivalent: diff -want,+got:\n%v", diff)
	}
}

func TestGetLeavesByIndex(t *testing.T) {
	ctx := context.Background()

	// Create fake leaf as if it had been sequenced, read it back and check contents
	cleanTestDB(DB)
	tree := createTreeOrPanic(DB, testonly.LogTree)
	s := NewLogStorage(DB, nil)

	// The leaf indices are checked against the tree size so we need a root.
	createFakeSignedLogRoot(DB, tree, uint64(sequenceNumber+1))

	data := []byte("some data")
	data2 := []byte("some other data")
	createFakeLeaf(ctx, DB, tree.TreeId, dummyRawHash, dummyHash, data, someExtraData, sequenceNumber, t)
	createFakeLeaf(ctx, DB, tree.TreeId, dummyRawHash2, dummyHash2, data2, someExtraData2, sequenceNumber-1, t)

	var tests = []struct {
		desc     string
		indices  []int64
		wantErr  bool
		wantCode codes.Code
		checkFn  func([]*trillian.LogLeaf, *testing.T)
	}{
		{
			desc:    "InTree",
			indices: []int64{sequenceNumber},
			checkFn: func(leaves []*trillian.LogLeaf, t *testing.T) {
				checkLeafContents(leaves[0], sequenceNumber, dummyRawHash, dummyHash, data, someExtraData, t)
			},
		},
		{
			desc:    "InTree2",
			indices: []int64{sequenceNumber - 1},
			wantErr: false,
			checkFn: func(leaves []*trillian.LogLeaf, t *testing.T) {
				checkLeafContents(leaves[0], sequenceNumber, dummyRawHash2, dummyHash2, data2, someExtraData2, t)
			},
		},
		{
			desc:    "InTreeMultiple",
			indices: []int64{sequenceNumber - 1, sequenceNumber},
			checkFn: func(leaves []*trillian.LogLeaf, t *testing.T) {
				checkLeafContents(leaves[1], sequenceNumber, dummyRawHash, dummyHash, data, someExtraData, t)
				checkLeafContents(leaves[0], sequenceNumber, dummyRawHash2, dummyHash2, data2, someExtraData2, t)
			},
		},
		{
			desc:    "InTreeMultipleReverse",
			indices: []int64{sequenceNumber, sequenceNumber - 1},
			checkFn: func(leaves []*trillian.LogLeaf, t *testing.T) {
				checkLeafContents(leaves[0], sequenceNumber, dummyRawHash, dummyHash, data, someExtraData, t)
				checkLeafContents(leaves[1], sequenceNumber, dummyRawHash2, dummyHash2, data2, someExtraData2, t)
			},
		}, {
			desc:     "OutsideTree",
			indices:  []int64{sequenceNumber + 1},
			wantErr:  true,
			wantCode: codes.OutOfRange,
		},
		{
			desc:     "LongWayOutsideTree",
			indices:  []int64{9999},
			wantErr:  true,
			wantCode: codes.OutOfRange,
		},
		{
			desc:     "MixedInOutTree",
			indices:  []int64{sequenceNumber, sequenceNumber + 1},
			wantErr:  true,
			wantCode: codes.OutOfRange,
		},
		{
			desc:     "MixedInOutTree2",
			indices:  []int64{sequenceNumber - 1, sequenceNumber + 1},
			wantErr:  true,
			wantCode: codes.OutOfRange,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			runLogTX(s, tree, t, func(ctx context.Context, tx storage.LogTreeTX) error {
				got, err := tx.GetLeavesByIndex(ctx, test.indices)
				if test.wantErr {
					if err == nil || status.Code(err) != test.wantCode {
						t.Errorf("GetLeavesByIndex(%v)=%v,%v; want: nil, err with code %v", test.indices, got, err, test.wantCode)
					}
				} else {
					if err != nil {
						t.Errorf("GetLeavesByIndex(%v)=%v,%v; want: got, nil", test.indices, got, err)
					}
				}
				return nil
			})
		})
	}
}

// GetLeavesByRange tests. -----------------------------------------------------

type getLeavesByRangeTest struct {
	start, count int64
	want         []int64
	wantErr      bool
}

func testGetLeavesByRangeImpl(t *testing.T, create *trillian.Tree, tests []getLeavesByRangeTest) {
	cleanTestDB(DB)

	ctx := context.Background()
	tree, err := createTree(DB, create)
	if err != nil {
		t.Fatalf("Error creating log: %v", err)
	}
	// Note: GetLeavesByRange loads the root internally to get the tree size.
	createFakeSignedLogRoot(DB, tree, 14)
	s := NewLogStorage(DB, nil)

	// Create leaves [0]..[19] but drop leaf [5] and set the tree size to 14.
	for i := int64(0); i < 20; i++ {
		if i == 5 {
			continue
		}
		data := []byte{byte(i)}
		identityHash := sha256.Sum256(data)
		createFakeLeaf(ctx, DB, tree.TreeId, identityHash[:], identityHash[:], data, someExtraData, i, t)
	}

	for _, test := range tests {
		runLogTX(s, tree, t, func(ctx context.Context, tx storage.LogTreeTX) error {
			leaves, err := tx.GetLeavesByRange(ctx, test.start, test.count)
			if err != nil {
				if !test.wantErr {
					t.Errorf("GetLeavesByRange(%d, +%d)=_,%v; want _,nil", test.start, test.count, err)
				}
				return nil
			}
			if test.wantErr {
				t.Errorf("GetLeavesByRange(%d, +%d)=_,nil; want _,non-nil", test.start, test.count)
			}
			got := make([]int64, len(leaves))
			for i, leaf := range leaves {
				got[i] = leaf.LeafIndex
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("GetLeavesByRange(%d, +%d)=%+v; want %+v", test.start, test.count, got, test.want)
			}
			return nil
		})
	}
}

func TestGetLeavesByRangeFromLog(t *testing.T) {
	var tests = []getLeavesByRangeTest{
		{start: 0, count: 1, want: []int64{0}},
		{start: 0, count: 2, want: []int64{0, 1}},
		{start: 1, count: 3, want: []int64{1, 2, 3}},
		{start: 10, count: 7, want: []int64{10, 11, 12, 13}},
		{start: 13, count: 1, want: []int64{13}},
		{start: 14, count: 4, wantErr: true},   // Starts right after tree size.
		{start: 19, count: 2, wantErr: true},   // Starts further away.
		{start: 3, count: 5, wantErr: true},    // Hits non-contiguous leaves.
		{start: 5, count: 5, wantErr: true},    // Starts from a missing leaf.
		{start: 1, count: 0, wantErr: true},    // Empty range.
		{start: -1, count: 1, wantErr: true},   // Negative start.
		{start: 1, count: -1, wantErr: true},   // Negative count.
		{start: 100, count: 30, wantErr: true}, // Starts after all stored leaves.
	}
	testGetLeavesByRangeImpl(t, testonly.LogTree, tests)
}

func TestGetLeavesByRangeFromPreorderedLog(t *testing.T) {
	var tests = []getLeavesByRangeTest{
		{start: 0, count: 1, want: []int64{0}},
		{start: 0, count: 2, want: []int64{0, 1}},
		{start: 1, count: 3, want: []int64{1, 2, 3}},
		{start: 10, count: 7, want: []int64{10, 11, 12, 13, 14, 15, 16}},
		{start: 13, count: 1, want: []int64{13}},
		// Starts right after tree size.
		{start: 14, count: 4, want: []int64{14, 15, 16, 17}},
		{start: 19, count: 2, want: []int64{19}}, // Starts further away.
		{start: 3, count: 5, wantErr: true},      // Hits non-contiguous leaves.
		{start: 5, count: 5, wantErr: true},      // Starts from a missing leaf.
		{start: 1, count: 0, wantErr: true},      // Empty range.
		{start: -1, count: 1, wantErr: true},     // Negative start.
		{start: 1, count: -1, wantErr: true},     // Negative count.
		{start: 100, count: 30, want: []int64{}}, // Starts after all stored leaves.
	}
	testGetLeavesByRangeImpl(t, testonly.PreorderedLogTree, tests)
}

// -----------------------------------------------------------------------------

func TestLatestSignedRootNoneWritten(t *testing.T) {
	ctx := context.Background()

	cleanTestDB(DB)
	tree, err := createTree(DB, testonly.LogTree)
	if err != nil {
		t.Fatalf("createTree: %v", err)
	}
	s := NewLogStorage(DB, nil)

	tx, err := s.SnapshotForTree(ctx, tree)
	if err != storage.ErrTreeNeedsInit {
		t.Fatalf("SnapshotForTree gave %v, want %v", err, storage.ErrTreeNeedsInit)
	}
	commit(tx, t)
}

func TestLatestSignedLogRoot(t *testing.T) {
	cleanTestDB(DB)
	tree := createTreeOrPanic(DB, testonly.LogTree)
	s := NewLogStorage(DB, nil)

	signer := tcrypto.NewSigner(tree.TreeId, ttestonly.NewSignerWithFixedSig(nil, []byte("notempty")), crypto.SHA256)
	root, err := signer.SignLogRoot(&types.LogRootV1{
		TimestampNanos: 98765,
		TreeSize:       16,
		Revision:       5,
		RootHash:       []byte(dummyHash),
	})
	if err != nil {
		t.Fatalf("SignLogRoot(): %v", err)
	}

	runLogTX(s, tree, t, func(ctx context.Context, tx storage.LogTreeTX) error {
		if err := tx.StoreSignedLogRoot(ctx, *root); err != nil {
			t.Fatalf("Failed to store signed root: %v", err)
		}
		return nil
	})

	{
		runLogTX(s, tree, t, func(ctx context.Context, tx2 storage.LogTreeTX) error {
			root2, err := tx2.LatestSignedLogRoot(ctx)
			if err != nil {
				t.Fatalf("Failed to read back new log root: %v", err)
			}
			if !proto.Equal(root, &root2) {
				t.Fatalf("Root round trip failed: <%v> and: <%v>", root, root2)
			}
			return nil
		})
	}
}

func TestDuplicateSignedLogRoot(t *testing.T) {
	cleanTestDB(DB)
	tree := createTreeOrPanic(DB, testonly.LogTree)
	s := NewLogStorage(DB, nil)

	signer := tcrypto.NewSigner(tree.TreeId, ttestonly.NewSignerWithFixedSig(nil, []byte("notempty")), crypto.SHA256)
	root, err := signer.SignLogRoot(&types.LogRootV1{
		TimestampNanos: 98765,
		TreeSize:       16,
		Revision:       5,
		RootHash:       []byte(dummyHash),
	})
	if err != nil {
		t.Fatalf("SignLogRoot(): %v", err)
	}

	runLogTX(s, tree, t, func(ctx context.Context, tx storage.LogTreeTX) error {
		if err := tx.StoreSignedLogRoot(ctx, *root); err != nil {
			t.Fatalf("Failed to store signed root: %v", err)
		}
		// Shouldn't be able to do it again
		if err := tx.StoreSignedLogRoot(ctx, *root); err == nil {
			t.Fatal("Allowed duplicate signed root")
		}
		return nil
	})
}

func TestLogRootUpdate(t *testing.T) {
	// Write two roots for a log and make sure the one with the newest timestamp supersedes
	cleanTestDB(DB)
	tree := createTreeOrPanic(DB, testonly.LogTree)
	s := NewLogStorage(DB, nil)

	signer := tcrypto.NewSigner(tree.TreeId, ttestonly.NewSignerWithFixedSig(nil, []byte("notempty")), crypto.SHA256)
	root, err := signer.SignLogRoot(&types.LogRootV1{
		TimestampNanos: 98765,
		TreeSize:       16,
		Revision:       5,
		RootHash:       []byte(dummyHash),
	})
	if err != nil {
		t.Fatalf("SignLogRoot(): %v", err)
	}
	root2, err := signer.SignLogRoot(&types.LogRootV1{
		TimestampNanos: 98766,
		TreeSize:       16,
		Revision:       6,
		RootHash:       []byte(dummyHash),
	})
	if err != nil {
		t.Fatalf("SignLogRoot(): %v", err)
	}

	runLogTX(s, tree, t, func(ctx context.Context, tx storage.LogTreeTX) error {
		if err := tx.StoreSignedLogRoot(ctx, *root); err != nil {
			t.Fatalf("Failed to store signed root: %v", err)
		}
		if err := tx.StoreSignedLogRoot(ctx, *root2); err != nil {
			t.Fatalf("Failed to store signed root: %v", err)
		}
		return nil
	})

	runLogTX(s, tree, t, func(ctx context.Context, tx2 storage.LogTreeTX) error {
		root3, err := tx2.LatestSignedLogRoot(ctx)
		if err != nil {
			t.Fatalf("Failed to read back new log root: %v", err)
		}
		if !proto.Equal(root2, &root3) {
			t.Fatalf("Root round trip failed: <%v> and: <%v>", root, root2)
		}
		return nil
	})
}

func TestGetActiveLogIDs(t *testing.T) {
	ctx := context.Background()

	cleanTestDB(DB)
	admin := NewAdminStorage(DB)

	// Create a few test trees
	log1 := proto.Clone(testonly.LogTree).(*trillian.Tree)
	log2 := proto.Clone(testonly.LogTree).(*trillian.Tree)
	log3 := proto.Clone(testonly.PreorderedLogTree).(*trillian.Tree)
	drainingLog := proto.Clone(testonly.LogTree).(*trillian.Tree)
	frozenLog := proto.Clone(testonly.LogTree).(*trillian.Tree)
	deletedLog := proto.Clone(testonly.LogTree).(*trillian.Tree)
	map1 := proto.Clone(testonly.MapTree).(*trillian.Tree)
	map2 := proto.Clone(testonly.MapTree).(*trillian.Tree)
	deletedMap := proto.Clone(testonly.MapTree).(*trillian.Tree)
	for _, tree := range []*trillian.Tree{log1, log2, log3, drainingLog, frozenLog, deletedLog, map1, map2, deletedMap} {
		newTree, err := storage.CreateTree(ctx, admin, tree)
		if err != nil {
			t.Fatalf("CreateTree(%+v) returned err = %v", tree, err)
		}
		*tree = *newTree
	}

	// FROZEN is not a valid initial state, so we have to update it separately.
	if _, err := storage.UpdateTree(ctx, admin, frozenLog.TreeId, func(t *trillian.Tree) {
		t.TreeState = trillian.TreeState_FROZEN
	}); err != nil {
		t.Fatalf("UpdateTree() returned err = %v", err)
	}
	// DRAINING is not a valid initial state, so we have to update it separately.
	if _, err := storage.UpdateTree(ctx, admin, drainingLog.TreeId, func(t *trillian.Tree) {
		t.TreeState = trillian.TreeState_DRAINING
	}); err != nil {
		t.Fatalf("UpdateTree() returned err = %v", err)
	}

	// Update deleted trees accordingly
	updateDeletedStmt, err := DB.PrepareContext(ctx, "UPDATE Trees SET Deleted = ? WHERE TreeId = ?")
	if err != nil {
		t.Fatalf("PrepareContext() returned err = %v", err)
	}
	defer updateDeletedStmt.Close()
	for _, treeID := range []int64{deletedLog.TreeId, deletedMap.TreeId} {
		if _, err := updateDeletedStmt.ExecContext(ctx, true, treeID); err != nil {
			t.Fatalf("ExecContext(%v) returned err = %v", treeID, err)
		}
	}

	s := NewLogStorage(DB, nil)
	tx, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot() returns err = %v", err)
	}
	defer tx.Close()
	got, err := tx.GetActiveLogIDs(ctx)
	if err != nil {
		t.Fatalf("GetActiveLogIDs() returns err = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("Commit() returned err = %v", err)
	}

	want := []int64{log1.TreeId, log2.TreeId, log3.TreeId, drainingLog.TreeId}
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
	if diff := pretty.Compare(got, want); diff != "" {
		t.Errorf("post-GetActiveLogIDs diff (-got +want):\n%v", diff)
	}
}

func TestGetActiveLogIDsEmpty(t *testing.T) {
	ctx := context.Background()

	cleanTestDB(DB)
	s := NewLogStorage(DB, nil)

	tx, err := s.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() = (_, %v), want = (_, nil)", err)
	}
	defer tx.Close()
	ids, err := tx.GetActiveLogIDs(ctx)
	if err != nil {
		t.Fatalf("GetActiveLogIDs() = (_, %v), want = (_, nil)", err)
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("Commit() = %v, want = nil", err)
	}

	if got, want := len(ids), 0; got != want {
		t.Errorf("GetActiveLogIDs(): got %v IDs, want = %v", got, want)
	}
}

func TestGetUnsequencedCounts(t *testing.T) {
	numLogs := 4
	cleanTestDB(DB)
	trees := make([]*trillian.Tree, 0, numLogs)
	for i := 0; i < numLogs; i++ {
		trees = append(trees, createTreeOrPanic(DB, testonly.LogTree))
	}
	s := NewLogStorage(DB, nil)

	ctx := context.Background()
	expectedCount := make(map[int64]int64)

	for i := int64(1); i < 10; i++ {
		// Put some leaves in the queue of each of the logs
		for j, tree := range trees {
			numToAdd := i + int64(j)
			runLogTX(s, tree, t, func(ctx context.Context, tx storage.LogTreeTX) error {
				leaves := createTestLeaves(numToAdd, expectedCount[tree.TreeId])
				if _, err := tx.QueueLeaves(ctx, leaves, fakeDequeueCutoffTime); err != nil {
					t.Fatalf("Failed to queue leaves: %v", err)
				}
				return nil
			})

			expectedCount[tree.TreeId] += numToAdd
		}

		// Now check what we get back from GetUnsequencedCounts matches
		tx, err := s.Snapshot(ctx)
		if err != nil {
			t.Fatalf("Snapshot() = (_, %v), want no error", err)
		}
		// tx explicitly closed in all branches

		got, err := tx.GetUnsequencedCounts(ctx)
		if err != nil {
			tx.Close()
			t.Errorf("GetUnsequencedCounts() = %v, want no error", err)
		}
		if err := tx.Commit(); err != nil {
			t.Errorf("Commit() = %v, want no error", err)
			return
		}
		if diff := pretty.Compare(expectedCount, got); diff != "" {
			t.Errorf("GetUnsequencedCounts() = diff -want +got:\n%s", diff)
		}
	}
}

func TestReadOnlyLogTX_Rollback(t *testing.T) {
	ctx := context.Background()
	cleanTestDB(DB)
	s := NewLogStorage(DB, nil)
	tx, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot() = (_, %v), want = (_, nil)", err)
	}
	defer tx.Close()
	if _, err := tx.GetActiveLogIDs(ctx); err != nil {
		t.Fatalf("GetActiveLogIDs() = (_, %v), want = (_, nil)", err)
	}
	// It's a bit hard to have a more meaningful test. This should suffice.
	if err := tx.Rollback(); err != nil {
		t.Errorf("Rollback() = (_, %v), want = (_, nil)", err)
	}
}

func TestGetSequencedLeafCount(t *testing.T) {
	ctx := context.Background()

	// We'll create leaves for two different trees
	cleanTestDB(DB)
	log1 := createTreeOrPanic(DB, testonly.LogTree)
	log2 := createTreeOrPanic(DB, testonly.LogTree)
	s := NewLogStorage(DB, nil)

	{
		// Create fake leaf as if it had been sequenced
		data := []byte("some data")
		createFakeLeaf(ctx, DB, log1.TreeId, dummyHash, dummyRawHash, data, someExtraData, sequenceNumber, t)

		// Create fake leaves for second tree as if they had been sequenced
		data2 := []byte("some data 2")
		data3 := []byte("some data 3")
		createFakeLeaf(ctx, DB, log2.TreeId, dummyHash2, dummyRawHash, data2, someExtraData, sequenceNumber, t)
		createFakeLeaf(ctx, DB, log2.TreeId, dummyHash3, dummyRawHash, data3, someExtraData, sequenceNumber+1, t)
	}

	// Read back the leaf counts from both trees
	runLogTX(s, log1, t, func(ctx context.Context, tx storage.LogTreeTX) error {
		count1, err := tx.GetSequencedLeafCount(ctx)
		if err != nil {
			t.Fatalf("unexpected error getting leaf count: %v", err)
		}
		if want, got := int64(1), count1; want != got {
			t.Fatalf("expected %d sequenced for logId but got %d", want, got)
		}
		return nil
	})

	runLogTX(s, log2, t, func(ctx context.Context, tx storage.LogTreeTX) error {
		count2, err := tx.GetSequencedLeafCount(ctx)
		if err != nil {
			t.Fatalf("unexpected error getting leaf count2: %v", err)
		}
		if want, got := int64(2), count2; want != got {
			t.Fatalf("expected %d sequenced for logId2 but got %d", want, got)
		}
		return nil
	})
}

func TestSortByLeafIdentityHash(t *testing.T) {
	l := make([]*trillian.LogLeaf, 30)
	for i := range l {
		hash := sha256.Sum256([]byte{byte(i)})
		leaf := trillian.LogLeaf{
			LeafIdentityHash: hash[:],
			LeafValue:        []byte(fmt.Sprintf("Value %d", i)),
			ExtraData:        []byte(fmt.Sprintf("Extra %d", i)),
			LeafIndex:        int64(i),
		}
		l[i] = &leaf
	}
	sort.Sort(byLeafIdentityHash(l))
	for i := range l {
		if i == 0 {
			continue
		}
		if bytes.Compare(l[i-1].LeafIdentityHash, l[i].LeafIdentityHash) != -1 {
			t.Errorf("sorted leaves not in order, [%d] = %x, [%d] = %x", i-1, l[i-1].LeafIdentityHash, i, l[i].LeafIdentityHash)
		}
	}

}


//Nick's test
func TestMapFunctionality(t *testing.T) {
	ctx := context.Background()
	cleanTestDB(DB)
	var TreeId int64 = 12345
	var UserId string = "67890"
	PublicKey := "cs61c"
	Identifiers := "-tap"
	Identity := "sdlkfjnsdf"
	createFakeAddMap (ctx, DB, TreeId, UserId, PublicKey, Identifiers, Identity, t)
}

func ensureAllLeavesDistinct(leaves []*trillian.LogLeaf, t *testing.T) {
	t.Helper()
	// All the leaf value hashes should be distinct because the leaves were created with distinct
	// leaf data. If only we had maps with slices as keys or sets or pretty much any kind of usable
	// data structures we could do this properly.
	for i := range leaves {
		for j := range leaves {
			if i != j && bytes.Equal(leaves[i].LeafIdentityHash, leaves[j].LeafIdentityHash) {
				t.Fatalf("Unexpectedly got a duplicate leaf hash: %v %v",
					leaves[i].LeafIdentityHash, leaves[j].LeafIdentityHash)
			}
		}
	}
}

func ensureLeavesHaveQueueTimestamp(t *testing.T, leaves []*trillian.LogLeaf, want time.Time) {
	t.Helper()
	for _, leaf := range leaves {
		gotQTimestamp, err := ptypes.Timestamp(leaf.QueueTimestamp)
		if err != nil {
			t.Fatalf("Got invalid queue timestamp: %v", err)
		}
		if got, want := gotQTimestamp.UnixNano(), want.UnixNano(); got != want {
			t.Errorf("Got leaf with QueueTimestampNanos = %v, want %v: %v", got, want, leaf)
		}
	}
}

// Creates some test leaves with predictable data
func createTestLeaves(n, startSeq int64) []*trillian.LogLeaf {
	var leaves []*trillian.LogLeaf
	for l := int64(0); l < n; l++ {
		lv := fmt.Sprintf("Leaf %d", l+startSeq)
		h := sha256.New()
		h.Write([]byte(lv))
		leafHash := h.Sum(nil)
		leaf := &trillian.LogLeaf{
			LeafIdentityHash: leafHash,
			MerkleLeafHash:   leafHash,
			LeafValue:        []byte(lv),
			ExtraData:        []byte(fmt.Sprintf("Extra %d", l)),
			LeafIndex:        int64(startSeq + l),
		}
		leaves = append(leaves, leaf)
	}

	return leaves
}

// Convenience methods to avoid copying out "if err != nil { blah }" all over the place
func runLogTX(s storage.LogStorage, tree *trillian.Tree, t *testing.T, f storage.LogTXFunc) {
	t.Helper()
	if err := s.ReadWriteTransaction(context.Background(), tree, f); err != nil {
		t.Fatalf("Failed to run log tx: %v", err)
	}
}

type committableTX interface {
	Commit() error
}

func commit(tx committableTX, t *testing.T) {
	t.Helper()
	if err := tx.Commit(); err != nil {
		t.Errorf("Failed to commit tx: %v", err)
	}
}

func leafInBatch(leaf *trillian.LogLeaf, batch []*trillian.LogLeaf) bool {
	for _, bl := range batch {
		if bytes.Equal(bl.LeafIdentityHash, leaf.LeafIdentityHash) {
			return true
		}
	}

	return false
}

// byLeafIdentityHash allows sorting of leaves by their identity hash, so DB
// operations always happen in a consistent order.
type byLeafIdentityHash []*trillian.LogLeaf

func (l byLeafIdentityHash) Len() int      { return len(l) }
func (l byLeafIdentityHash) Swap(i, j int) { l[i], l[j] = l[j], l[i] }
func (l byLeafIdentityHash) Less(i, j int) bool {
	return bytes.Compare(l[i].LeafIdentityHash, l[j].LeafIdentityHash) == -1
}

func logTree(logID int64) *trillian.Tree {
	return &trillian.Tree{
		TreeId:       logID,
		TreeType:     trillian.TreeType_LOG,
		HashStrategy: trillian.HashStrategy_RFC6962_SHA256,
	}
}
