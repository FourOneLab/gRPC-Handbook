dep:
	go get -u google.golang.org/grpc
	go get -u github.com/golang/protobuf/protoc-gen-go

gen:
	protoc -I product/service/pb product_info.proto order_manager.proto --go_out=plugins=grpc:product/service

build:
	go build -i -v -o bin/server product/service/main.go
	go build -i -v -o bin/client product/client/main.gocd