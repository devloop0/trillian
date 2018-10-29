all: setup initialize

initialize: initialize_log_server initialize_log_signer initialize_log_id

delete: delete_log_id

clean: delete
	killall ../trillian_log_server
	killall ../trillian_log_signer
	rm -f data/transaction_data data/logid

setup:
	mkdir -p data

initialize_log_server: 
	../trillian_log_server -rpc_endpoint "localhost:8090" -http_endpoint "localhost:8091" &

initialize_log_signer:
	../trillian_log_signer --force_master -rpc_endpoint "localhost:8092" -http_endpoint "localhost:8093" -batch_size=1 &

initialize_log_id:
	sleep 2
	../createtree --admin_server "localhost:8090" > data/logid

delete_log_id:
	go run ../cmd/deletetree/main.go --admin_server "localhost:8090" --log_id=`cat data/logid`

.PHONY: all initialize delete setup initialize_log_server initialize_log_singer initialize_log_id delete_log_id clean
