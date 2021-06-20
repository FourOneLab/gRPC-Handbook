module productinfo/client

go 1.16

require (
	google.golang.org/grpc v1.38.0
	product/service v0.0.0
)

replace product/service => ../service
