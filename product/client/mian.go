package main

import (
	"context"
	"io"
	"log"
	"product/service/pb"
	"time"

	"google.golang.org/protobuf/types/known/wrapperspb"

	"google.golang.org/grpc"
)

const address = "localhost:50051"

type client struct {
	pb.ProductInfoClient
	pb.OrderManagerClient
}

func main() {
	conn, err := grpc.Dial(address, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	cli := client{
		ProductInfoClient:  pb.NewProductInfoClient(conn),
		OrderManagerClient: pb.NewOrderManagerClient(conn),
	}

	name := "Apple iPhone 11"
	description := "Meet Apple iPhone 11. All-new dual-camera system system with Ultra Wide and Night mode."
	price := float32(1000.0)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	r, err := cli.AddProduct(ctx, &pb.Product{Name: name, Description: description, Price: price})
	if err != nil {
		log.Fatalf("Could not add product: %v", err)
	}
	log.Printf("Product ID: %s added successfully", r.Value)

	product, err := cli.GetProduct(ctx, &pb.ProductID{Value: r.Value})
	if err != nil {
		log.Fatalf("Could not get product: %v", err)
	}
	log.Printf("Product: %s", product.String())

	order, err := cli.GetOrder(ctx, &wrapperspb.StringValue{Value: "123456789"})
	if err != nil {
		log.Fatalf("Could not get order by id: %s", "123456789")
	}
	log.Printf("Order: %s", order.String())

	searchStream, _ := cli.SearchOrders(ctx, &wrapperspb.StringValue{Value: "Google"})
	for {
		searchOrder, err := searchStream.Recv()
		if err != nil {
			if err == io.EOF {
				// 流结束时，Recv() 方法会返回 io.EOF
				break
			} else {
				// 处理可能出现的错误
			}
		}
		log.Printf("Search Result: %v\n", searchOrder)
	}

	updateStream, err := cli.UpdateOrders(ctx)
	if err != nil {
		log.Fatalf("%v.UpdateOrders(_) = _, %v", cli, err)
	}

	// 更新订单1
	upOrder1 := &pb.Order{}
	if err = updateStream.Send(upOrder1); err != nil {
		log.Fatalf("%v.Send(%v) = %v", updateStream, upOrder1, err)
	}

	// 更新订单2
	upOrder2 := &pb.Order{}
	if err = updateStream.Send(upOrder1); err != nil {
		log.Fatalf("%v.Send(%v) = %v", updateStream, upOrder2, err)
	}

	// 更新订单3
	upOrder3 := &pb.Order{}
	if err = updateStream.Send(upOrder1); err != nil {
		log.Fatalf("%v.Send(%v) = %v", updateStream, upOrder3, err)
	}

	updateRes, err := updateStream.CloseAndRecv()
	if err != nil {
		log.Fatalf("%v.CloseAndRecv() got error %v, want %v", updateStream, err, nil)
	}
	log.Printf("Update Orders Res: %s", updateRes)

	processOrdersStream, _ := cli.ProcessOrders(ctx)
	if err = processOrdersStream.Send(&wrapperspb.StringValue{Value: "102"}); err != nil {
		log.Fatalf("%v.Send(%v) = %v", cli, "102", err)
	}

	if err = processOrdersStream.Send(&wrapperspb.StringValue{Value: "103"}); err != nil {
		log.Fatalf("%v.Send(%v) = %v", cli, "103", err)
	}

	if err = processOrdersStream.Send(&wrapperspb.StringValue{Value: "104"}); err != nil {
		log.Fatalf("%v.Send(%v) = %v", cli, "104", err)
	}

	stop := make(chan struct{})

	go asyncClientBidirectionalRPC(processOrdersStream, stop)

	time.Sleep(1000 * time.Millisecond)

	if err = processOrdersStream.Send(&wrapperspb.StringValue{Value: "101"}); err != nil {
		log.Fatalf("%v.Send(%v) = %v", cli, "101", err)
	}

	if err = processOrdersStream.CloseSend(); err != nil {
		log.Fatal(err)
	}

	<-stop
}

func asyncClientBidirectionalRPC(processOrdersStream pb.OrderManager_ProcessOrdersClient, stop chan struct{}) {
	for {
		combinedShipment, err := processOrdersStream.Recv()
		if err == io.EOF {
			break
		}
		log.Printf("Combined shipment: %s", combinedShipment.OrderList)
	}
	<-stop
}
