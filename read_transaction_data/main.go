package main

import (
	"log"
	"context"
	"time"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"

	"google.golang.org/grpc"
	"github.com/google/trillian"
	"github.com/google/trillian/userTypes"
	"github.com/google/trillian/networkSimulator"
	"google.golang.org/grpc/codes"
)

const chunkSize int64 = 1024
const initializeFile string = "data/transaction_data"

const txRead int64 = 1
const txWrite int64 = 2

const useTrillianAPI = false

const useTransactions = false

type tx struct {
	txType int64
	logId int64
	userId string
	oldPublicKey string
	deviceId string
	newPublicKey string
}

func readTransactionData(initializeFile string) ([]tx, error) {
	b, err := ioutil.ReadFile(initializeFile)
	if err != nil {
		return nil, err
	}
	s := string(b)

	r := csv.NewReader(strings.NewReader(s))
	ret := []tx{}
	records, err := r.ReadAll()
	for _, record := range records {
		txType, err := strconv.ParseInt(record[0], 10, 64)
		if err != nil {
			return ret, err
		}
		logId, err := strconv.ParseInt(record[1], 10, 64)
		if err != nil {
			return ret, err
		}
		userId := record[2]
		oldPublicKey := record[3]
		deviceId := record[4]
		newPublicKey := record[5]
		ret = append(ret, tx{txType: txType, logId: logId, userId: userId, oldPublicKey: oldPublicKey, deviceId: deviceId, newPublicKey: newPublicKey})
	}
	return ret, nil
}

func writeTransactions(ctx context.Context, client trillian.TrillianLogClient, transactions []tx) error {
	for _, tx_data := range transactions {
		if tx_data.txType != txWrite {
			break
		}
		if !useTrillianAPI {
			q := &trillian.UserWriteLeafRequest{LogId: tx_data.logId, UserId: tx_data.userId, OldPublicKey: tx_data.oldPublicKey, DeviceId: tx_data.deviceId, NewPublicKey: tx_data.newPublicKey}
			r, err := client.UserWriteLeaves(ctx, q)
			NetworkSimulator.GenerateDelay()
			if err != nil {
				log.Fatal (err)
				return err
			}
			for _, leaf := range (r.Leaves.QueuedLeaves) {
				c := codes.Code(leaf.GetStatus().GetCode())
				if c != codes.OK && c != codes.AlreadyExists {
					return err
				}
			}
		} else {
			data := UserTypes.UserData{UserId: tx_data.userId, OldPublicKey: tx_data.oldPublicKey, DeviceId: tx_data.deviceId, NewPublicKey: tx_data.newPublicKey}
			j, err := json.Marshal(data)
			if err != nil {
				return err
			}

			tl := &trillian.LogLeaf{LeafValue: j}
			q := &trillian.QueueLeafRequest{LogId: tx_data.logId, Leaf: tl}
			r, err := client.QueueLeaf(ctx, q)
			NetworkSimulator.GenerateDelay()
			if err != nil {
				return err
			}
			c := codes.Code(r.QueuedLeaf.GetStatus().GetCode())
			if c != codes.OK && c != codes.AlreadyExists {
				return err;
			}
		}
		time.Sleep (10 * time.Millisecond)
	}
	return nil
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second * 300)
	defer cancel()

	conn, err := grpc.DialContext(ctx, "localhost:8090", grpc.WithInsecure())
	if err != nil {
		log.Fatal("Failed to connect to log server")
	}
	defer conn.Close()

	txData, err := readTransactionData(initializeFile)
	if err != nil {
		log.Fatal("Could not read transaction data")
	}
	client := trillian.NewTrillianLogClient(conn)

	if (useTransactions) {
		err = writeTransactions(ctx, client, txData)
		if err != nil {
			log.Fatal("Could not write data")
		}
	}

	logId := txData[0].logId

	rr := &trillian.GetLatestSignedLogRootRequest{LogId: logId}
	lr, err := client.GetLatestSignedLogRoot(ctx, rr)
	if err != nil {
		log.Fatal("Could not get latest signed root")
	}
	ts := lr.SignedLogRoot.TreeSize
	for n := int64(0); n < ts; {
		g := &trillian.GetLeavesByRangeRequest{LogId: logId, StartIndex: n, Count: chunkSize}
		r, err := client.GetLeavesByRange(ctx, g)
		if err != nil {
			log.Fatal("Could not get leaves")
		}

		if len(r.Leaves) == 0 {
			log.Fatalf("No progress made at leaf %d", n)
		}

		for m := 0; m < len(r.Leaves) && n < ts; n++ {
			if r.Leaves[m] == nil {
				log.Fatalf("Could not read leaf at index %d", m)
			}

			if r.Leaves[m].LeafIndex != n {
				log.Fatalf("Expected index %d got index %d", r.Leaves[m].LeafIndex, n)
			}

			fmt.Printf("%v\n", r.Leaves[m])

			m++
		}
	}
}
