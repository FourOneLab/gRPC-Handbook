dep:
	go get -u google.golang.org/grpc
	go get -u github.com/golang/protobuf/protoc-gen-go

gen:
	protoc -I productinfo/service/pb product_info.proto --go_out=plugins=grpc:productinfo/service

build:
	go build -i -v -o bin/server productinfo/service/main.go
	go build -i -v -o bin/client productinfo/client/main.go