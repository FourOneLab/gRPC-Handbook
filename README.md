# gRPC Handbook

gRPC: Up and Running

## RPC

只要涉及到网络通信，必然涉及到网络协议，应用层也是一样。在应用层最标准和常用的就是 HTTP 协议。但在很多性能要求较高的场景会使用自定义的 RPC 协议。举个例子，就是相当于各个省不但用官方普通话，还都有自己的方言，RPC就相当于是一个方言。

说到 RPC，可能第一反应就是“微服务”。在微服务架构中，每个服务实例负责某一单一领域的业务实现，不同服务实例之间需要进行频繁的交互来共同实现业务逻辑。服务实例之间通过轻量级的远程调用方式进行通信。

**RPC** 的全称是 **Remote Procedure Call**，就是远程过程调用。但这个名字过分强调了和 `LPC`（本地过程调用）的对比。没有突出出来 RPC 本身涉及到的一些技术特点。

### RPC是什么

RPC 可以分为两部分：**用户调用接口** + **具体网络协议**。前者为开发者需要关心的，后者由框架来实现。

1. 用户调用接口：我们会和服务约定好远程调用的函数名。因此，用户接口就是：输入、输出、远程函数名。
1. 具体网络协议：这是框架来实现的，把开发者要发出和接收的内容以某种应用层协议打包进行网络收发。

因此想要实现用户接口，最重要需要支持以下三个功能：

- 定位要调用的服务；
- 把完整的消息切下来；
- 让我们的消息向前/向后兼容；

这样既可以让消息内保证一定的灵活性，又可以方便拿下一块数据，去调用用户想要的服务。

HTTP 和 RPC 协议的实现方式：

|      | 定位要调用的服务          | 消息长度                 | 消息前后兼容    |
| :--- | :------------------------ | :----------------------- | --------------- |
| HTTP | URL                       | header 里 Content-Length | body 里自己解决 |
| RPC  | 指定 Service 和 Method 名 | 协议 header 里自行约定   | 交给具体 IDL    |

因此，都会需要类似的结构去组装一条完整的用户请求，而第三部分的 body 只要框架支持，RPC 协议和 HTTP 是可以互通的！因此完全可以根据自己的业务需求进行选型。

### RPC有什么

RPC 框架从用户到系统都有哪些层次：

1. 与用户相关的接口描述文件层，根据用户定义的请求/回复结构进行代码生成，用户接口和前后兼容问题，都是 IDL 层来解决的。

   - **用户代码**（client 的发送函数/ server 的函数实现）

   - **IDL序列化**（protobuf/thrift serialization）

   - **数据组织** （protobuf/thrift/json）

2. 与框架相关的 RPC 协议层：

   - **压缩**（none/gzip/zlib/snappy/lz4）

   - **协议** （Thrift-framed/TRPC/gRPC）

3. 与框架相关的网络通信层：

   - **通信** （TCP/HTTP）

### RPC生命周期

![生命周期](/resources/srpc-life-cycle.png)

根据上图，可以更清楚地看到各个层级中压缩层、序列化层、协议层是互相解耦打通的，横向增加任何一种压缩算法或 IDL 或协议都不需要也不应该改动现有的代码，才是一个精美的架构。

在这里的生成代码是衔接用户调用接口和框架代码的桥梁。比如 server 的接口作为一个服务端，要做的就是`收到请求`->`处理逻辑`->`返回回复`，而这个时候，框架已经把网络收发、解压缩、反序列化等都给做好了，然后通过生成代码调用到用户实现的派生 service 类的函数逻辑中。

其实 RPC 还可以做的事情还有很多，包括内部各层次的解耦合设计、框架层的功能埋点、外部服务集群的对接等等：

![RPC 插件](/resources/srpc-plugin.png)
