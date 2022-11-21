# gRPC On Kubernetes

## Cluster IP

ClusterIP 模式下，Service 会被分配一个集群内的 IP 地址，客户端的请求会发送给它，然后再通过负载均衡转发给后端某个 pod。

![cluster ip](/resources/kubernetes-cluster-ip.png)

- 如果是基于 HTTP/1.1 协议的服务，那么 ClusterIP 完全没有问题；
- 如果是基于 HTTP/2 协议的服务（如，gRPC 服务），那么 ClusterIP 会导致负载失衡，因为 HTTP/2 协议多个请求在一个 TCP 连接上多路复用，一旦 ClusterIP 和某个 Pod 建立了连接后，后续请求都会被转发给此 Pod。

> 虽然 HTTP/1.1 实现了基于 KeepAlive 的连接复用，但是这里的复用是串行的（HTTP队头堵塞问题），当请求到达的时候，如果没有空闲连接那么就新创建一个连接，如果有空闲连接那么就可以复用，同一个时间点，连接里最多只能承载一个请求，结果是 HTTP/1.1 可以连接多个 Pod；而 HTTP/2 的复用是并行的，当请求到达的时候，如果没有连接那么就创建连接，如果有连接，那么不管其是否空闲都可以复用，同一个时间点，连接里可以承载多个请求，结果是 HTTP/2 仅仅连接了一个 Pod。

## 解决方案

### 服务端负载均衡

主要是在 Pod 之前增加一个中间组件 Proxy，一般为 7 层负载均衡。Client 请求中间组件，由中间组件再去请求后端的 Pod。

常见的组件，如采用 Envoy 做代理，和每台后端服务器保持长连接，当客户端请求到达时，代理服务器依照规则转发请求给后端服务器，从而实现负载均衡。

服务端负载均衡方案结构清晰，客户端不需要了解后端服务器，对架构没有侵入性，但是性能会因为存在转发而打折扣。

### 客户端负载均衡

具体方案：**NameResolver** + **Load Balancing Policy** + **Headless Service**。

- 当 gRPC 客户端想要与 gRPC 服务器进行交互时，它首先尝试通过向 NameResolver 发出名称解析请求来解析服务器名称，解析程序返回已解析 IP 地址的列表，然后通过算法来实现负载均衡。

- Kubernetes Headless-Service 在创建的时候会将该服务对应的每个 Pod IP 以 A 记录的形式存储。

- 常见的 gRPC 库都内置了负载均衡算法，如 `pick_first` 和 `round_robin` 两种算法。
  - `pick_first`：尝试连接到第一个地址，如果连接成功，则将其用于所有RPC，如果连接失败，则尝试下一个地址（并继续这样做，直到一个连接成功）。
  - `round_robin`：连接到它看到的所有地址，并依次向每个后端发送一个 RPC。例如，第一个 RPC 将发送到 backend-1，第二个 RPC 将发送到 backend-2，第三个 RPC 将再次发送到 backend-1。

所以建立连接时只需要提供一个服务名即可，gRPC Client 会根据 DNS resolver 返回的 IP 列表分别建立连接，请求时使用 `round_robin` 算法进行负载均衡，选择其中一个连接用来发起请求。

```go
	svc := "mygrpc:50051"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	conn, err := grpc.DialContext(
		ctx,
		fmt.Sprintf("%s:///%s", "dns", svc),
        // 指定轮询负载均衡算法
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`), 
		grpc.WithInsecure(),
		grpc.WithBlock(),
	)
	if err != nil {
		log.Fatal(err)
	}
```

客户端负载均衡方案结构复杂，客户端需要了解后端服务器，对架构有侵入性，但是性能更好。

相比之下更加推荐使用 **客户端负载均衡**。

- 客户端负载均衡更加简单，服务直连性能更高。
- 服务端负载均衡所有请求都需要经过负载均衡组件，相当于是又引入了一个全局热点。
- ServiceMesh 的话对基础设施、技术栈要求比较高，落地比较困难。

#### 存在的问题

**当 Pod 扩缩容时 客户端如何感知并更新连接？**

- Pod 缩容后，由于 gRPC 具有连接探活机制，会自动丢弃无效连接。
- Pod 扩容后，没有感知机制，导致后续扩容的 Pod 无法被请求到。

gRPC 连接默认能永久存活，如果将该值降低能改善这个问题。

在服务端做以下设置

```go
	port := conf.GetPort()
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer(grpc.KeepaliveParams(keepalive.ServerParameters{
		MaxConnectionAge:      time.Minute,
	}))
	pb.RegisterVerifyServer(s, core.Verify)
	log.Println("Serving gRPC on 0.0.0.0" + port)
	if err = s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
```

这样每个连接只会使用一分钟，到期后会重新建立连接，相当于对扩容的感知只会延迟 1 分钟。

#### [kuberesolver](https://github.com/sercand/kuberesolver)

为了解决以上问题，很容易想到直接在 client 端调用 Kubernetes API 监测 Service 对应的 endpoints 变化，然后动态更新连接信息。

具体就是将 DNSresolver 替换成了自定义的 kuberesolver。**同时如果 Kubernetes 集群中使用了 RBAC 授权的话需要给 client 所在Pod赋予 endpoint 资源的 get 和 watch 权限。**

具体授权过程如下：需要分别创建`ServiceAccount`、`Role`、`RoleBinding`3 个实例， k8s 用的也是 RBAC 授权，所以应该比较好理解。

因为 kuberesolver 是直接调用 Kubernetes API 获取 endpoint 所以不需要创建 Headless Service 了，创建普通 Service 也可以。