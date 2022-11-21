# Service Discovery

本质上服务发现的目的是解耦程序对服务具体位置的依赖，对于微服务架构来说，服务发现不是可选的，而是必须的。
要理解服务发现，需要知道服务发现解决了如下三个问题：

- **服务注册**（Service Registration）当服务启动的时候，通过某种形式（比如调用API、产生上线事件消息、在Etcd中记录、存数据库等等）把自己（服务）的信息通知给服务注册中心，这个过程一般是由微服务框架来完成，业务代码无感知。
- **服务维护**（Service Maintaining）尽管在微服务框架中通常都提供下线机制，但并没有办法保证每次服务都能优雅下线（Graceful Shutdown），而不是由于宕机、断网等原因突然失联，所以，在微服务框架中就必须要尽可能的保证维护的服务列表的正确性，以避免访问不可用服务节点的尴尬。
- **服务发现**（Service Discovery）这里所说的发现是狭义的，它特指消费者从微服务框架（服务发现模块）中，把一个服务标识（一般是服务名）转换为服务实际位置（一般是ip地址）的过程。这个过程（可能是调用API，监听Etcd，查询数据库等）业务代码无感知。

服务发现有两种模式，分别是**服务端服务发现**和**客户端服务发现**。

- 对于服务端服务发现来说，服务调用方无需关注服务发现的具体细节，只需要知道服务的DNS域名即可，支持不同语言的接入，对基础设施来说，需要专门支持负载均衡器，对于请求链路来说多了一次网络跳转，可能会有性能损耗。
- 对于客户端服务发现来说，由于客户端和服务端采用了直连的方式，比服务端服务发现少了一次网络跳转，对于服务调用方来说需要内置负载均衡器，不同的语言需要各自实现。

对于微服务架构来说，我们期望的是去中心化依赖，中心化的依赖会让架构变得复杂，当出现问题的时候也会让整个排查链路变得繁琐，所以一般采用的是客户端服务发现的模式。

## 服务发现

gRPC 提供了自定义 Resolver 的能力来实现服务发现，通过 Register 方法来进行注册自定义的 Resolver，自定义的 Resolver 需要实现 `Builder` 接口，定义如下：

```go
// Builder creates a resolver that will be used to watch name resolution updates.
type Builder interface {
    // Build creates a new resolver for the given target.
    //
    // gRPC dial calls Build synchronously, and fails if the returned error is
    // not nil.
    Build(target Target, cc ClientConn, opts BuildOptions) (Resolver, error)
    // Scheme returns the scheme supported by this resolver.
    // Scheme is defined at https://github.com/grpc/grpc/blob/master/doc/naming.md.
    Scheme() string
}
```

`Scheme()`返回一个stirng。注册的 `Resolver` 会被保存在一个全局的变量 m 中，m 是一个 map，这个 map 的 key 即为 `Scheme()` 方法返回的字符串。也就是多个 Resolver 是通过Scheme 来进行区分的，所以我们定义 `Resolver` 的时候 Scheme 不要重复，否则 Resolver 就会被覆盖。
通过下面的示例代码，来看一下自定义的 Builer 是在哪里被执行的。

## 实例代码

```go
func main() {
    flag.Parse()
    // Set up a connection to the server.
    conn, err := grpc.Dial(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
    if err != nil {
        log.Fatalf("did not connect: %v", err)
    }
    defer conn.Close()
    c := pb.NewGreeterClient(conn)

    // Contact the server and print out its response.
    ctx, cancel := context.WithTimeout(context.Background(), time.Second)
    defer cancel()
    r, err := c.SayHello(ctx, &pb.HelloRequest{Name: *name})
    if err != nil {
        log.Fatalf("could not greet: %v", err)
    }
    log.Printf("Greeting: %s", r.GetMessage())
}
```

如上的示例代码，使用 gRPC 进行服务调用前：

1. 先调用`grpc.Dial()` 方法创建一个 `grpc.ClientConn` 对象，底层调用 `DialContext`。

```go
// Dial creates a client connection to the given target.
func Dial(target string, opts ...DialOption) (*ClientConn, error) {
    return DialContext(context.Background(), target, opts...)
}
```

2. 其中 `ClientConn` 是通过调用 `NewGreeterClient` 传入的，`NewGreeterClient` 为 **protoc** 自动生成的代码，并赋值给 `cc` 属性。

```go
func NewGreeterClient(cc grpc.ClientConnInterface) GreeterClient {
    return &greeterClient{cc}
}
```

3. 最终发起调用时，调用了 `ClientConn` 的 `Invoke()` 方法。

```go
func (c *greeterClient) SayHello(ctx context.Context, in *HelloRequest, opts ...grpc.CallOption) (*HelloReply, error) {
    out := new(HelloReply)
    err := c.cc.Invoke(ctx, "/helloworld.Greeter/SayHello", in, out, opts...)
    if err != nil {
        return nil, err
    }
    return out, nil
}
```

在整个客户端的调用流程中，核心就是 `ClientConn` 这个对象，在 `DialContext()` 方法种创建完成。

```go
// ClientConn represents a virtual connection to a conceptual endpoint, to
// perform RPCs.
//
// A ClientConn is free to have zero or more actual connections to the endpoint
// based on configuration, load, etc. It is also free to determine which actual
// endpoints to use and may change it every RPC, permitting client-side load
// balancing.
//
// A ClientConn encapsulates a range of functionality including name
// resolution, TCP connection establishment (with retries and backoff) and TLS
// handshakes. It also handles errors on established connections by
// re-resolving the name and reconnecting.
type ClientConn struct {
    ctx    context.Context    // Initialized using the background context at dial time.
    cancel context.CancelFunc // Cancelled on close.

    // The following are initialized at dial time, and are read-only after that.
    target          string               // User's dial target.
    parsedTarget    resolver.Target      // See parseTargetAndFindResolver().
    authority       string               // See determineAuthority().
    dopts           dialOptions          // Default and user specified dial options.
    channelzID      *channelz.Identifier // Channelz identifier for the channel.
    balancerWrapper *ccBalancerWrapper   // Uses gracefulswitch.balancer underneath.

    // The following provide their own synchronization, and therefore don't
    // require cc.mu to be held to access them.
    csMgr              *connectivityStateManager
    blockingpicker     *pickerWrapper
    safeConfigSelector iresolver.SafeConfigSelector
    czData             *channelzData
    retryThrottler     atomic.Value // Updated from service config.

    // firstResolveEvent is used to track whether the name resolver sent us at
    // least one update. RPCs block on this event.
    firstResolveEvent *grpcsync.Event

    // mu protects the following fields.
    // TODO: split mu so the same mutex isn't used for everything.
    mu              sync.RWMutex
    resolverWrapper *ccResolverWrapper         // Initialized in Dial; cleared in Close.
    sc              *ServiceConfig             // Latest service config received from the resolver.
    conns           map[*addrConn]struct{}     // Set to nil on close.
    mkp             keepalive.ClientParameters // May be updated upon receipt of a GoAway.

    lceMu               sync.Mutex // protects lastConnectionError
    lastConnectionError error
}
```

`target` 字段就是传入的地址，具体的采用的是 URI 的格式，如`dns:[//authority/]host[:port]`，参看详细[说明文档](https://github.com/grpc/grpc/blob/master/doc/naming.md)。通过调用 `ClientConn` 的 `parseTargetAndFindResolver()` 来获取 Resolver，在这个方法中主要就是把 `target` 中的 resolver name 解析出来，然后根据 resolver name 去保存 Resolver 的全局变量 `m` 中去找对应的 Resolver。

```go
func Register(b Builder) {
    m[b.Scheme()] = b
}
```

在获取到 Resolver 后在 `newCCResolverWrapper()` 方法中调用该 Resolver 的 `Build()`方法，也就是上面 Builder 接口中的方法。Build() 方法中的一个参数 target 如下所示。

```go
// Target represents a target for gRPC, as specified in:
// https://github.com/grpc/grpc/blob/master/doc/naming.md.
// It is parsed from the target string that gets passed into Dial or DialContext
// by the user. And gRPC passes it to the resolver and the balancer.
//
// If the target follows the naming spec, and the parsed scheme is registered
// with gRPC, we will parse the target string according to the spec. If the
// target does not contain a scheme or if the parsed scheme is not registered
// (i.e. no corresponding resolver available to resolve the endpoint), we will
// apply the default scheme, and will attempt to reparse it.
//
// Examples:
//
//   - "dns://some_authority/foo.bar"
//     Target{Scheme: "dns", Authority: "some_authority", Endpoint: "foo.bar"}
//   - "foo.bar"
//     Target{Scheme: resolver.GetDefaultScheme(), Endpoint: "foo.bar"}
//   - "unknown_scheme://authority/endpoint"
//     Target{Scheme: resolver.GetDefaultScheme(), Endpoint: "unknown_scheme://authority/endpoint"}
type Target struct {
    // Deprecated: use URL.Scheme instead.
    Scheme string
    // Deprecated: use URL.Host instead.
    Authority string
    // Deprecated: use URL.Path or URL.Opaque instead. The latter is set when
    // the former is empty.
    Endpoint string
    // URL contains the parsed dial target with an optional default scheme added
    // to it if the original dial target contained no scheme or contained an
    // unregistered scheme. Any query params specified in the original dial
    // target can be accessed from here.
    URL url.URL
}
```

`Build()`方法中的第二个参数，是 `resoulver.ClientConn` 接口（与上面的 `grpc.ClientConn`结构体要区分一下），它的实现 `ccResolverWrapper`的第一个参数就是 `grpc.ClientConn`。

```go

// ClientConn contains the callbacks for resolver to notify any updates
// to the gRPC ClientConn.
//
// This interface is to be implemented by gRPC. Users should not need a
// brand new implementation of this interface. For the situations like
// testing, the new implementation should embed this interface. This allows
// gRPC to add new methods to this interface.
type ClientConn interface {
    // UpdateState updates the state of the ClientConn appropriately.
    UpdateState(State) error
    // ReportError notifies the ClientConn that the Resolver encountered an
    // error.  The ClientConn will notify the load balancer and begin calling
    // ResolveNow on the Resolver with exponential backoff.
    ReportError(error)
    // NewAddress is called by resolver to notify ClientConn a new list
    // of resolved addresses.
    // The address list should be the complete list of resolved addresses.
    //
    // Deprecated: Use UpdateState instead.
    NewAddress(addresses []Address)
    // NewServiceConfig is called by resolver to notify ClientConn a new
    // service config. The service config should be provided as a json string.
    //
    // Deprecated: Use UpdateState instead.
    NewServiceConfig(serviceConfig string)
    // ParseServiceConfig parses the provided service config and returns an
    // object that provides the parsed config.
    ParseServiceConfig(serviceConfigJSON string) *serviceconfig.ParseResult
}
// ccResolverWrapper is a wrapper on top of cc for resolvers.
// It implements resolver.ClientConn interface.
type ccResolverWrapper struct {
    cc         *ClientConn
    resolverMu sync.Mutex
    resolver   resolver.Resolver
    done       *grpcsync.Event
    curState   resolver.State

    incomingMu sync.Mutex // Synchronizes all the incoming calls.
}
```

`Build()`方法中的第三个参数，是一些 `BuildOptions`。

```go
// BuildOptions includes additional information for the builder to create
// the resolver.
type BuildOptions struct {
    // DisableServiceConfig indicates whether a resolver implementation should
    // fetch service config data.
    DisableServiceConfig bool
    // DialCreds is the transport credentials used by the ClientConn for
    // communicating with the target gRPC service (set via
    // WithTransportCredentials). In cases where a name resolution service
    // requires the same credentials, the resolver may use this field. In most
    // cases though, it is not appropriate, and this field may be ignored.
    DialCreds credentials.TransportCredentials
    // CredsBundle is the credentials bundle used by the ClientConn for
    // communicating with the target gRPC service (set via
    // WithCredentialsBundle). In cases where a name resolution service
    // requires the same credentials, the resolver may use this field. In most
    // cases though, it is not appropriate, and this field may be ignored.
    CredsBundle credentials.Bundle
    // Dialer is the custom dialer used by the ClientConn for dialling the
    // target gRPC service (set via WithDialer). In cases where a name
    // resolution service requires the same dialer, the resolver may use this
    // field. In most cases though, it is not appropriate, and this field may
    // be ignored.
    Dialer func(context.Context, string) (net.Conn, error)
}
```

到这里我们已经知道了自定 Resolver 的 **Build()** 方法的调用逻辑，以及传入的参数的由来以及含义。
