package main

import (
	"context"
	"log"
	"productinfo/service/pb"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// server 实现 product info 服务器
type server struct {
	productMap map[string]*pb.Product // 内存数据库
}

// AddProduct 添加商品到服务器中
func (s *server) AddProduct(ctx context.Context, in *pb.Product) (*pb.ProductID, error) {
	out, err := uuid.NewUUID()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Error while generating Product ID", err)
	}

	in.Id = out.String()

	if s.productMap == nil {
		s.productMap = make(map[string]*pb.Product)
	}

	s.productMap[in.Id] = in

	log.Printf("Product %s added successfully", in.Id)
	return &pb.ProductID{Value: in.Id}, status.New(codes.OK, "").Err()
}

// GetProduct 根据商品编号获取指定商品
func (s server) GetProduct(ctx context.Context, in *pb.ProductID) (*pb.Product, error) {
	log.Printf("query %s", in.GetValue())

	if value, ok := s.productMap[in.Value]; ok {
		return value, status.New(codes.OK, "").Err()
	}

	return nil, status.Errorf(codes.NotFound, "Product does not exist.", in.Value)
}
