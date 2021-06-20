package main

import (
	"context"
	"io"
	"log"
	"product/service/pb"
	"strings"

	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// server 实现 product info 服务器
type server struct {
	productMap      map[string]*pb.Product          // 内存数据库
	orderMap        map[string]*pb.Order            // 内存数据库
	combinedShipMap map[string]*pb.CombinedShipment // 内存数据库

	orderBatchSize int
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

// GetOrder 根据 ID 获取订单
func (s *server) GetOrder(ctx context.Context, orderId *wrapperspb.StringValue) (*pb.Order, error) {
	if ord, ok := s.orderMap[orderId.Value]; ok {
		return ord, status.New(codes.OK, "").Err()
	}
	return nil, status.Errorf(codes.NotFound, "order ID: %s not exist.", orderId.Value)
}

// SearchOrder 根据 ID 搜索订单
func (s *server) SearchOrder(searchQuery *wrapperspb.StringValue, stream pb.OrderManager_SearchOrdersServer) error {
	for key, order := range s.orderMap {
		log.Println(key, order)
		for _, item := range order.Items {
			log.Println(item)
			if strings.Contains(item, searchQuery.Value) {
				// 在流中发送匹配的订单
				if err := stream.Send(order); err != nil {
					return status.Errorf(codes.Internal, "error sending message to stream: %v", err)
				}
				log.Printf("Matching Order Found: %s\n", key)
				break
			}
		}
	}

	return nil
}

// UpdateOrders 根据批量存储订单
func (s *server) UpdateOrders(stream pb.OrderManager_UpdateOrdersServer) error {
	orderStr := "Updated Order IDs: "

	for {
		order, err := stream.Recv()
		if err == io.EOF {
			// 完成定读取订单流
			return stream.SendAndClose(&wrapperspb.StringValue{Value: "Orders processed" + orderStr})
		}

		// 更新订单
		s.orderMap[order.Id] = order
		log.Printf("Order ID %s: Updated", order.Id)
		orderStr += order.Id + ", "
	}
}

// ProcessOrders 批量处理订单，批量返回处理结果
func (s *server) ProcessOrders(stream pb.OrderManager_ProcessOrdersServer) error {
	batchMarker := 0

	for {
		orderId, err := stream.Recv()
		if err == io.EOF {
			// 处理订单
			_ = orderId
			for _, shipment := range s.combinedShipMap {
				stream.Send(shipment)
			}
			return nil
		}

		if err != nil {
			return err
		}

		//  基于目的地址，将订单组织到发货组合中
		_ = orderId

		if batchMarker == s.orderBatchSize {
			// 将组合后的订单以流的形式分批发送至客户端
			for _, shipment := range s.combinedShipMap {
				// 将发货组合发送到客户端
				stream.Send(shipment)
			}
			batchMarker = 0
			s.combinedShipMap = make(map[string]*pb.CombinedShipment)

		} else {
			batchMarker++
		}
	}
}
