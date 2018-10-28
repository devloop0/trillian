package main

import (
	"log"
	"context"
	"time"
	"encoding/json"
	"encoding/csv"
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"

	"google.golang.org/grpc"
	"github.com/google/trillian"
	"google.golang.org/grpc/codes"
)

const chunkSize int64 = 1024
const initializeFile string = "data/transaction_data"

const txRead int64 = 1
const txWrite int64 = 2

type tx struct {
	txType int64
	logId int64
	userId string
	publicKey string
	userIdentifier string
}

type UserData struct {
	UserId string
	PublicKey string
	UserIdentifier string
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
		publicKey := record[3]
		userIdentifier := record[4]
		ret = append(ret, tx{txType: txType, logId: logId, userId: userId, publicKey: publicKey, userIdentifier: userIdentifier})
	}
	return ret, nil
}

func writeTransactions(ctx context.Context, client trillian.TrillianLogClient, transactions []tx) error {
	for _, tx_data := range transactions {
		if tx_data.txType != txWrite {
			break
		}

		data := UserData{UserId: tx_data.userId, PublicKey: tx_data.publicKey, UserIdentifier: tx_data.userIdentifier}
		j, err := json.Marshal(data)
		if err != nil {
			return err
		}

		tl := &trillian.LogLeaf{LeafValue: j}
		q := &trillian.QueueLeafRequest{LogId: tx_data.logId, Leaf: tl}
		r, err := client.QueueLeaf(ctx, q)
		if err != nil {
			return err
		}
		c := codes.Code(r.QueuedLeaf.GetStatus().GetCode())
		if c != codes.OK && c != codes.AlreadyExists {
			return err
		}
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

	err = writeTransactions(ctx, client, txData)
	if err != nil {
		log.Fatal("Could not write data")
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
			log.Fatal("No progress made at leaf %d", n)
		}

		for m := 0; m < len(r.Leaves) && n < ts; n++ {
			if r.Leaves[m] == nil {
				log.Fatal("Could not read leaf at index %d", m)
			}

			if r.Leaves[m].LeafIndex != n {
				log.Fatal("Expected index %d got index %d", r.Leaves[m].LeafIndex, n)
			}

			fmt.Printf("%v\n", r.Leaves[m])

			m++
		}
	}
}
