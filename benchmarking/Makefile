SHELL := /bin/bash
PARTITION_COUNT=50
PARTITION_END=2
WRITE_FREQUENCY="vl"
LAMBDA=5
LAMBDA_PK=2
NUM_USERS=1000
BURSTY=false
OUTPUT_PATH="data/"

all: setup initialize generate_data

trillian: setup initialize_trillian generate_data run_trillian

transaction: setup initialize_transaction generate_data run_transaction


initialize_trillian: initialize_log_server initialize_trillian_log_signer initialize_log_id

initialize_transaction: initialize_log_server initialize_transaction_log_signer initialize_log_id

delete: delete_log_id

clean: delete
	killall ../trillian_log_signer
	killall ../trillian_log_server
	rm -f data/*
	rm benchmarking

clean_logs:
	rm -f logs/*

setup: rebuild
	mkdir -p data
	mkdir -p logs

initialize_log_server: 
	../trillian_log_server -rpc_endpoint "localhost:8090" -http_endpoint "localhost:8091" &

initialize_trillian_log_signer:
	../trillian_log_signer --force_master -rpc_endpoint "localhost:8092" -http_endpoint "localhost:8093" -batch_size=15 -sequencer_interval=50ms -log_dir=logs &


initialize_transaction_log_signer:
	../trillian_log_signer --force_master -rpc_endpoint "localhost:8092" -http_endpoint "localhost:8093" -batch_size=3 -sequencer_interval=50ms -log_dir=logs -extra_checks=true -use_trxns=true &

initialize_log_id:
	sleep 2
	../createtree --admin_server "localhost:8090" > data/logid

delete_log_id:
	go run ../cmd/deletetree/main.go --admin_server "localhost:8090" --log_id=`cat data/logid`

generate_data:
	sleep 2
	../scripts/transactiongen.py `cat data/logid` --bursty=$(BURSTY) --output_path=$(OUTPUT_PATH) --num_partitions=$(PARTITION_COUNT) --num_users=$(NUM_USERS) --lambda=$(LAMBDA) --lambda_pk=$(LAMBDA_PK) --write_frequency=$(WRITE_FREQUENCY)

run_trillian: init_tree_trillian run_trillian_clients

run_transaction: init_tree_transaction run_transaction_clients

init_tree_trillian:
	./benchmarking --input="data/init_tree" --trillian=true
	sleep 15

init_tree_transaction:
	./benchmarking --input="data/init_tree"
	sleep 15

run_trillian_clients:
	for i in `seq 0 $(PARTITION_END)`; do \
		./benchmarking --input=data/trillian_client`echo $$i` --trillian=true & \
	done


run_transaction_clients:
	for i in `seq 0 $(PARTITION_END)`; do \
		./benchmarking --input=data/transaction_client`echo $$i` & \
	done


rebuild:
	cd .. && go build -tags='batched_queue' ./server/trillian_log_server && go build -tags='batched_queue' ./server/trillian_log_signer
	yes | ../scripts/resetdb.sh
	go build -tags='batched_queue'

initialize_data: initialize_log_id generate_data

.PHONY: all trillian transaction initialize_trillian initialize_transaction delete setup initialize_log_server initialize_log_singer initialize_log_id delete_log_id clean generate_data rebuild initialize_data debug_log_server debug_log_signer
