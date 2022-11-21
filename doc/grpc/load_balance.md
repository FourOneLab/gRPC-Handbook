# Load Balance

1. **公平性**，即负载均衡需要关注被调用服务实例组之间的公平性。
2. **正确性**，即对于有状态的服务来说，负载均衡需要关心请求的状态，将请求调度到能处理它的后端实例上，不要出现不能处理和错误处理的情况。

## 无状态负载均衡

指参与负载均衡的后端实例是无状态的，所有的后端实例都是对等的，一个请求不论发向哪一个实例，都会得到相同的并且正确的处理结果，所以无状态的负载均衡策略不需要关心请求的状态。常见的无状态负载均衡算法：

1. 轮询：将请求按顺序分配给多个实例，轮询在路由时，不利用请求的状态信息。在公平性方面，因为轮询策略只是按顺序分配请求，所以适用于请求的工作负载和实例的处理能力差异都较小的情况。
2. 权重轮询：将每一个后端实例分配一个权重，分配请求的数量和实例的权重成正比轮询。权重轮询在路由时，不利用请求的状态信息。在公平性方面，因为权重策略会按实例的权重比例来分配请求数，所以，可以利用它解决实例的处理能力差异的问题，认为它的公平性比轮询策略要好。

## 有状态负载均衡

在负载均衡策略中会保存服务端的一些状态，然后根据这些状态按照一定的算法选择出对应的实例。有状态负载均衡算法：

1. P2C：随机从所有可用节点中选择两个节点，然后计算这两个节点的负载情况，选择负载较低的一个节点来服务本次请求。为了避免某些节点一直得不到选择导致不平衡，会在超过一定的时间后强制选择一次。
2. EWMA：指数移动加权平均算法，表示一段时间内的均值。该算法相对于算数平均来说对于突然的网络抖动没有那么敏感，突然的抖动不会体现在请求的lag中，从而可以让算法更加均衡。

## gRPC

### 负载均衡

在 gRPC 中，Balancer 和 Resolver 一样也可以自定义，同样也是通过 Register 方法进行注册。

```go
func Register(b Builder) {
    m[strings.ToLower(b.Name())] = b
}
```

要想实现自定义的 Balancer ，就必须要实现 `balancer.Builder` 接口。

```go
type Builder interface {
    // Build creates a new balancer with the ClientConn.
    Build(cc ClientConn, opts BuildOptions) Balancer
    // Name returns the name of balancers built by this builder.
    // It will be used to pick balancers (for example in service config).
    Name() string
}
```

`Build()` 方法的参数 `ClientConn` 和返回值 `Balancer` 也都是接口。

```go
type ClientConn interface {
    // NewSubConn is called by balancer to create a new SubConn.
    // It doesn't block and wait for the connections to be established.
    // Behaviors of the SubConn can be controlled by options.
    NewSubConn([]resolver.Address, NewSubConnOptions) (SubConn, error)
    // RemoveSubConn removes the SubConn from ClientConn.
    // The SubConn will be shutdown.
    RemoveSubConn(SubConn)
    // UpdateAddresses updates the addresses used in the passed in SubConn.
    // gRPC checks if the currently connected address is still in the new list.
    // If so, the connection will be kept. Else, the connection will be
    // gracefully closed, and a new connection will be created.
    //
    // This will trigger a state transition for the SubConn.
    UpdateAddresses(SubConn, []resolver.Address)

    // UpdateState notifies gRPC that the balancer's internal state has
    // changed.
    //
    // gRPC will update the connectivity state of the ClientConn, and will call
    // Pick on the new Picker to pick new SubConns.
    UpdateState(State)

    // ResolveNow is called by balancer to notify gRPC to do a name resolving.
    ResolveNow(resolver.ResolveNowOptions)

    // Target returns the dial target for this ClientConn.
    //
    // Deprecated: Use the Target field in the BuildOptions instead.
    Target() string
}
type Balancer interface {
    // UpdateClientConnState is called by gRPC when the state of the ClientConn
    // changes.  If the error returned is ErrBadResolverState, the ClientConn
    // will begin calling ResolveNow on the active name resolver with
    // exponential backoff until a subsequent call to UpdateClientConnState
    // returns a nil error.  Any other errors are currently ignored.
    UpdateClientConnState(ClientConnState) error
    // ResolverError is called by gRPC when the name resolver reports an error.
    ResolverError(error)
    // UpdateSubConnState is called by gRPC when the state of a SubConn
    // changes.
    UpdateSubConnState(SubConn, SubConnState)
    // Close closes the balancer. The balancer is not required to call
    // ClientConn.RemoveSubConn for its existing SubConns.
    Close()
}
```

通过上面的流程步骤，已经知道了如何自定义Balancer，以及如何注册自定义的Blancer。既然注册了肯定就会获取，接下来看一下是在哪里获取已经注册的 Balancer。
Resolver 是通过解析 `DialContext()` 的第二个参数 target，从而得到 Resolver name，然后根据 name 获取到对应的 Resolver 。获取 Balancer 同样也是根据名称，Balancer 的名称是在创建gRPC Client 的时候通过配置项传入的。

```go
roundrobinConn, err := grpc.Dial(
        fmt.Sprintf("%s:///%s", exampleScheme, exampleServiceName),
        grpc.WithDefaultServiceConfig(`{"loadBalancingConfig": [{"round_robin":{}}]}`), // This sets the initial balancing policy.
        grpc.WithTransportCredentials(insecure.NewCredentials()),
    )
```

当创建 gRPC 客户端的时候，会触发调用自定义 Resolver 的 `Build()` 方法，在 `Build()` 方法内部获取到服务地址列表后，通过 `cc.UpdateState()` 方法进行状态更新，后面当监听到服务状态变化的时候同样也会调用 `cc.UpdateState()` 进行状态的更新，而这里的 cc 指的就是 `ccResolverWrapper` 对象。

```go
func (ccr *ccResolverWrapper) UpdateState(s resolver.State) error {
    ccr.incomingMu.Lock()
    defer ccr.incomingMu.Unlock()
    if ccr.done.HasFired() {
        return nil
    }
    ccr.addChannelzTraceEvent(s)
    ccr.curState = s
    if err := ccr.cc.updateResolverState(ccr.curState, nil); err == balancer.ErrBadResolverState {
        return balancer.ErrBadResolverState
    }
    return nil
}
```

当监听到服务状态的变更后（首次启动或者通过 Watch 监听变化）调用 `ccResolverWrapper.UpdateState()` 触发更新状态的流程，各模块间的调用链路如下所示：

1. 在自定义的 Resolver 中监听服务状态的变更
2. 通过 UpdateState 来更新状态
3. 获取自定义的 Balancer
4. 执行自定义 Balancer 的 Build 方法获取 Balancer

```go
    ---> ccResolverWrapper.UpdateState() 
    ---> ClientConn.updateResolverState() 
    ---> ClientConn.applyServiceConfigAndBalancer() 
    ---> ccBalancerWrapper.switchTo() 
    ---> ccBalancerWrapper.handleSwitchTo()
    ---> Balancer.SwitchTo() 
    ---> builder.Build()
```

到这里我们已经知道了获取自定义 Balancer 是在哪里触达的，以及在哪里获取的自定义的 Balancer，和 `balancer.Builder` 的 `Build()` 方法在哪里被调用。这里的 `balancer.Builder` 为 `baseBuilder`，所以调用的 `Build()` 方法为 baseBuilder 的 `Build()` 方法，定义如下：

```go
func (bb *baseBuilder) Build(cc balancer.ClientConn, opt balancer.BuildOptions) balancer.Balancer {
    bal := &baseBalancer{
        cc:            cc,
        pickerBuilder: bb.pickerBuilder,

        subConns: resolver.NewAddressMap(),
        scStates: make(map[balancer.SubConn]connectivity.State),
        csEvltr:  &balancer.ConnectivityStateEvaluator{},
        config:   bb.config,
        state:    connectivity.Connecting,
    }
    // Initialize picker to a picker that always returns
    // ErrNoSubConnAvailable, because when state of a SubConn changes, we
    // may call UpdateState with this picker.
    bal.picker = NewErrPicker(balancer.ErrNoSubConnAvailable)
    return bal
}
```

### 建立新连接

```go
    ---> ClientConn.updateResolverState()
    ---> ccBalancerWrapper.updateClientConnState()
    ---> ccBalancerWrapper.handleClientConnStateChange()
    ---> Balancer.UpdateClientConnState()
    ---> balancerWrapper.UpdateClientConnState()
    ---> Balancer.UpdateSubConnState()
    ---> balancerWrapper.NewSubConn()
    ---> ccBalancerWrapper.NewSubConn()
    ---> ClientConn.newAddrConn()
```

这里的 `balancer.Builder` 为 `baseBuilder`，所以调用的 `Build()` 方法为 baseBuilder 的 `UpdateSubConnState()` 方法。

```go
func (b *baseBalancer) UpdateClientConnState(s balancer.ClientConnState) error {
    // TODO: handle s.ResolverState.ServiceConfig?
    if logger.V(2) {
        logger.Info("base.baseBalancer: got new ClientConn state: ", s)
    }
    // Successful resolution; clear resolver error and ensure we return nil.
    b.resolverErr = nil
    // addrsSet is the set converted from addrs, it's used for quick lookup of an address.
    addrsSet := resolver.NewAddressMap()
    for _, a := range s.ResolverState.Addresses {
        addrsSet.Set(a, nil)
        if _, ok := b.subConns.Get(a); !ok {
            // a is a new address (not existing in b.subConns).
            sc, err := b.cc.NewSubConn([]resolver.Address{a}, balancer.NewSubConnOptions{HealthCheckEnabled: b.config.HealthCheck})
            if err != nil {
                logger.Warningf("base.baseBalancer: failed to create new SubConn: %v", err)
                continue
            }
            b.subConns.Set(a, sc)
            b.scStates[sc] = connectivity.Idle
            b.csEvltr.RecordTransition(connectivity.Shutdown, connectivity.Idle)
            sc.Connect()
        }
    }
    for _, a := range b.subConns.Keys() {
        sci, _ := b.subConns.Get(a)
        sc := sci.(balancer.SubConn)
        // a was removed by resolver.
        if _, ok := addrsSet.Get(a); !ok {
            b.cc.RemoveSubConn(sc)
            b.subConns.Delete(a)
            // Keep the state of this sc in b.scStates until sc's state becomes Shutdown.
            // The entry will be deleted in UpdateSubConnState.
        }
    }
    // If resolver state contains no addresses, return an error so ClientConn
    // will trigger re-resolve. Also records this as an resolver error, so when
    // the overall state turns transient failure, the error message will have
    // the zero address information.
    if len(s.ResolverState.Addresses) == 0 {
        b.ResolverError(errors.New("produced zero addresses"))
        return balancer.ErrBadResolverState
    }

    b.regeneratePicker()
    b.cc.UpdateState(balancer.State{ConnectivityState: b.state, Picker: b.picker})
    return nil
}
```

当第一次触发调用 `UpdateClientConnState()` 方法的时候，第 12 行 `_, ok := b.subConns.Get(a)`为 false，所以会执行第 14 行 `b.cc.NewSubConn([]resolver.Address{a}, balancer.NewSubConnOptions{HealthCheckEnabled: b.config.HealthCheck})` 创建一个新的连接，这里的 `b.cc`就是 `balancerWrapper`，继续往下调用，最终创建的连接是 `addrConn`。

```go
// addrConn is a network connection to a given address.
type addrConn struct {
    ctx    context.Context
    cancel context.CancelFunc

    cc     *ClientConn
    dopts  dialOptions
    acbw   balancer.SubConn
    scopts balancer.NewSubConnOptions

    // transport is set when there's a viable transport (note: ac state may not be READY as LB channel
    // health checking may require server to report healthy to set ac to READY), and is reset
    // to nil when the current transport should no longer be used to create a stream (e.g. after GoAway
    // is received, transport is closed, ac has been torn down).
    transport transport.ClientTransport // The current transport.

    mu      sync.Mutex
    curAddr resolver.Address   // The current address.
    addrs   []resolver.Address // All addresses that the resolver resolved to.

    // Use updateConnectivityState for updating addrConn's connectivity state.
    state connectivity.State

    backoffIdx   int // Needs to be stateful for resetConnectBackoff.
    resetBackoff chan struct{}

    channelzID *channelz.Identifier
    czData     *channelzData
}
```

创建连接的默认状态为 `connectivity.Idle` ，在 gRPC 中连接共定义了 5 种状态：

```go
const (
    // Idle indicates the ClientConn is idle.
    Idle State = iota
    // Connecting indicates the ClientConn is connecting.
    Connecting
    // Ready indicates the ClientConn is ready for work.
    Ready
    // TransientFailure indicates the ClientConn has seen a failure but expects to recover.
    TransientFailure
    // Shutdown indicates the ClientConn has started shutting down.
    Shutdown
)
```

在 baseBalancer 中通过 `scStates` 保存创建的连接，初始状态也为 `connectivity.Idle`，之后通过 `sc.Connect()` 也就是 `acBalancerWrapper.Connect()` 新建 goroutine 异步进行连接，最终调用的是 `addrConn.connect()` 来完成连接。

```go
func (ac *addrConn) connect() error {
    ac.mu.Lock()
    if ac.state == connectivity.Shutdown {
        ac.mu.Unlock()
        return errConnClosing
    }
    if ac.state != connectivity.Idle {
        ac.mu.Unlock()
        return nil
    }
    // Update connectivity state within the lock to prevent subsequent or
    // concurrent calls from resetting the transport more than once.
    ac.updateConnectivityState(connectivity.Connecting, nil)
    ac.mu.Unlock()

    ac.resetTransport()
    return nil
}
```

从 `addrConn.connect()`开始的调用链如下：

```go
    ---> addrConn.connect()
    ---> addrConn.updateConnectivityState()
    ---> ClientConn.handleSubConnStateChange()
    ---> ccBalancerWrapper.updateSubConnState()
    ---> ccBalancerWrapper.handleSubConnStateChange()
    ---> Balancer.UpdateSubConnState()
    ---> balancerWrapper.UpdateSubConnState()
    ---> baseBalancer.UpdateSubConnState()
    ---> balancerWrapper.UpdateState()
    ---> ccBalancerWrapper.UpdateState()
    ---> pickerWrapper.updatePicker()
```

至此，在 baseBalancer 的 UpdateSubConnState 方法的最后，更新了 Picker 为自定义的 Picker，最后 `addrConn.connect` 中调用 `addrConn.resetTransport()` 进行真正的连接建立，调用过程如下：

```go
    ---> addrConn.resetTransport()
    ---> addrConn.tryAllAddrs()
    ---> addrConn.createTransport()
    ---> transport.NewClientTransport()
    ---> transport.newHTTP2Client()
    ---> addrConn.startHealthCheck()
    ---> addrConn.updateConnectivityState()
```

当连接已经创建好，处于 Ready 状态，最后调用 `baseBalancer.UpdateSubConnState()` 方法，此时 `s == connectivity.Ready` 为 true，而 `oldS == connectivity.Ready` 为 false，所以会调用 `b.regeneratePicker()` 方法

```go
if (s == connectivity.Ready) != (oldS == connectivity.Ready) ||
        b.state == connectivity.TransientFailure {
        b.regeneratePicker()
    }
func (b *baseBalancer) regeneratePicker() {
    if b.state == connectivity.TransientFailure {
        b.picker = NewErrPicker(b.mergeErrors())
        return
    }
    readySCs := make(map[balancer.SubConn]SubConnInfo)

    // Filter out all ready SCs from full subConn map.
    for _, addr := range b.subConns.Keys() {
        sci, _ := b.subConns.Get(addr)
        sc := sci.(balancer.SubConn)
        if st, ok := b.scStates[sc]; ok && st == connectivity.Ready {
            readySCs[sc] = SubConnInfo{Address: addr}
        }
    }
    b.picker = b.pickerBuilder.Build(PickerBuildInfo{ReadySCs: readySCs})
}
```

在 regeneratePicker 中获取了处于 `connectivity.Ready` 状态可用的连接，同时更新了 picker。

### 选择已创建连接

现在已经知道了如何创建连接，以及连接其实是在 `baseBalancer.scStates` 中管理，当连接的状态发生变化，则会更新 `baseBalancer.scStates`。那么接下来看一下 gRPC 是如何选择一个连接进行请求的发送的。当 gRPC 客户端发起调用的时候，会调用 ClientConn 的 `Invoke()` 方法，一般不会主动使用该方法进行调用，该方法的调用一般是自动生成：

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

如下是发起请求的调用链路，最终会调用 `Picker.Pick()` 方法获取连接，我们自定义的负载均衡算法一般都在 `Pick()` 方法中实现，获取到连接之后，通过 `sendMsg()` 发送请求。

```go
    ---> ClientConn.Invoke()
    ---> grpc.invoke()
    ---> grpc.newClientStream()
    ---> csAttempt.getTransport()
    ---> ClientConn.getTransport()
    ---> pickerWrapper.pick()
    ---> Picker.Pick()
    ---> clientStream.SendMsg()
    ---> csAttempt.sendMsg()
func (a *csAttempt) sendMsg(m interface{}, hdr, payld, data []byte) error {
    cs := a.cs
    if a.trInfo != nil {
        a.mu.Lock()
        if a.trInfo.tr != nil {
            a.trInfo.tr.LazyLog(&payload{sent: true, msg: m}, true)
        }
        a.mu.Unlock()
    }
    if err := a.t.Write(a.s, hdr, payld, &transport.Options{Last: !cs.desc.ClientStreams}); err != nil {
        if !cs.desc.ClientStreams {
            // For non-client-streaming RPCs, we return nil instead of EOF on error
            // because the generated code requires it.  finish is not called; RecvMsg()
            // will call it with the stream's status independently.
            return nil
        }
        return io.EOF
    }
    for _, sh := range a.statsHandlers {
        sh.HandleRPC(a.ctx, outPayload(true, m, data, payld, time.Now()))
    }
    if channelz.IsOn() {
        a.t.IncrMsgSent()
    }
    return nil
}
```
