all: setup initialize generate_data

initialize: initialize_log_server initialize_log_signer initialize_log_id

delete: delete_log_id

clean: delete
	killall ../trillian_log_signer
	killall ../trillian_log_server
	rm -f data/*
	rm read_transaction_data

setup: rebuild
	mkdir -p data

initialize_log_server: 
	../trillian_log_server -rpc_endpoint "localhost:8090" -http_endpoint "localhost:8091" &

initialize_log_signer:
	../trillian_log_signer --force_master -rpc_endpoint "localhost:8092" -http_endpoint "localhost:8093" -batch_size=3 -sequencer_interval=50ms -extra_checks=true -use_trxns=true &

initialize_log_id:
	sleep 2
	../createtree --admin_server "localhost:8090" > data/logid

delete_log_id:
	go run ../cmd/deletetree/main.go --admin_server "localhost:8090" --log_id=`cat data/logid`

generate_data:
	sleep 2
	../scripts/transactiongen.py `cat data/logid` --output_path=data/

rebuild:
	cd .. && go build -tags='batched_queue' ./server/trillian_log_server && go build -tags='batched_queue' ./server/trillian_log_signer
	yes | ../scripts/resetdb.sh
	go build -tags='batched_queue'

initialize_data: initialize_log_id generate_data

debug_log_server: 
	../trillian_log_server -rpc_endpoint "localhost:8090" -http_endpoint "localhost:8091" -logtostderr

debug_log_signer:
	../trillian_log_signer --force_master -rpc_endpoint "localhost:8092" -http_endpoint "localhost:8093" -batch_size=3 -sequencer_interval=50ms -extra_checks=true -use_trxns=true -logtostderr

.PHONY: all initialize delete setup initialize_log_server initialize_log_singer initialize_log_id delete_log_id clean generate_data rebuild initialize_data debug_log_server debug_log_signer
