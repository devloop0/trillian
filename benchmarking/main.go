package main

import (
	"log"
	"context"
	"time"
	"flag"
	"encoding/csv"
	"encoding/json"
	"io/ioutil"
	"strconv"
	"strings"
	"errors"
	"google.golang.org/grpc"
	"github.com/google/trillian"
	_ "github.com/google/trillian/merkle/rfc6962"
	"github.com/google/trillian/merkle/hashers"
	"github.com/google/trillian/userTypes"
	"github.com/google/trillian/networkSimulator"
	"google.golang.org/grpc/codes"
//	"crypto"
)

const chunkSize int64 = 1024
const txRead int64 = 1
const txWrite int64 = 2

var(
	useTrillianAPI = flag.Bool("trillian", false, "If true the transactions are run on vanilla trillian")
	initializeFile = flag.String ("input", "data/init_data", "File to read transactions from")
)

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

func processTransactions(ctx context.Context, client trillian.TrillianLogClient, transactions []tx) error {
	for _, tx_data := range transactions {
		if !*useTrillianAPI {
			if tx_data.txType == txWrite {
				q := &trillian.UserWriteLeafRequest{
					LogId: tx_data.logId,
					UserId: tx_data.userId,
					OldPublicKey: tx_data.oldPublicKey,
					DeviceId: tx_data.deviceId,
					NewPublicKey: tx_data.newPublicKey,
				}
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
			} else if tx_data.txType == txRead {
				q := &trillian.UserReadLeafRequest{
					LogId: tx_data.logId,
					UserId: tx_data.userId,
					DeviceId: tx_data.deviceId,
				}
				err := errors.New("inital_error")
				iters := 0
				for ; err != nil && iters < 100; {
					NetworkSimulator.GenerateDelay()
					_, err = client.UserReadLeaves(ctx, q)
					iters++
				}
			}
		} else {
			if tx_data.txType == txWrite {
				data := UserTypes.LeafData{PublicKey: tx_data.newPublicKey, DeviceId: tx_data.deviceId}
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
			} else if tx_data.txType == txRead {
				data := UserTypes.LeafData{PublicKey: tx_data.oldPublicKey, DeviceId: tx_data.deviceId}
				j, err := json.Marshal(data)
				if err != nil {
					return err
				}
				hasher, err := hashers.NewLogHasher (trillian.HashStrategy_RFC6962_SHA256)
				if err != nil {
					return err
				}
				hash, err := hasher.HashLeaf (j)
				if err != nil {
					continue
				}
				tl := &trillian.GetLeavesByHashRequest{LogId: tx_data.logId, LeafHash: [][]byte{hash}}
				err = errors.New("inital_error")
				iters := 0
				var resp *trillian.GetLeavesByHashResponse
				for ; err != nil && iters < 100; {
					NetworkSimulator.GenerateDelay()
					resp, err = client.GetLeavesByHash(ctx, tl)
					iters++
				}
				if err != nil {
					continue
				}
				err = errors.New("inital_error")
				tj := &trillian.GetInclusionProofByHashRequest{LogId: tx_data.logId, LeafHash: hash, TreeSize: resp.SignedLogRoot.GetTreeSize()}
				iters = 0
				for ; err != nil && iters < 100; {
					NetworkSimulator.GenerateDelay()
					_, err = client.GetInclusionProofByHash(ctx, tj)
					iters++
				}
				NetworkSimulator.GenerateDelay()
			}
		//time.Sleep (10 * time.Millisecond)
		}
	}
	return nil
}

func main() {
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second * 300)
	defer cancel()

	conn, err := grpc.DialContext(ctx, "localhost:8090", grpc.WithInsecure())
	if err != nil {
		log.Fatal("Failed to connect to log server")
	}
	defer conn.Close()

	//hashers.RegisterLogHasher(trillian.HashStrategy_RFC6962_SHA256, &rfc6962.Hasher{Hash: crypto.SHA256})
	txData, err := readTransactionData(*initializeFile)
	if err != nil {
		log.Fatal("Could not read transaction data")
	}
	client := trillian.NewTrillianLogClient(conn)

	err = processTransactions(ctx, client, txData)
	if err != nil {
		log.Fatal(err)
	}
}
