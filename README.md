
### 核心模块说明

- **`server.go`**: 实现了RPC服务端。负责监听连接、读取请求、调用本地注册的服务、并将结果返回给客户端。
- **`client.go`**: 实现了RPC客户端。负责与服务端建立连接、发送请求、并接收服务端的响应。
- **`codec/`**: 定义了编解码器的接口和具体实现。`goRpc`通过接口将网络连接中的数据流解码为Go对象，或将Go对象编码后发送。默认支持`gob`和`json`。
- **`service.go`**: 通过反射机制，将Go对象及其方法注册为RPC服务，使得这些方法可以被远程调用。
- **`registry/`**: 实现了一个简单的服务注册中心。服务提供者启动后，会定期向注册中心发送心跳来报告自己的存活状态。
- **`xclient/`**: 实现了一个更高级的客户端，它集成了服务发现和负载均衡功能。`XClient`会从注册中心获取可用的服务列表，并根据指定的负载均衡策略（如随机或轮询）来分发请求。

## 🚀 如何使用

以下是如何运行`goRpc`示例程序的步骤。

### 1. 启动服务注册中心

注册中心是一个独立的HTTP服务，用于记录哪些RPC服务是可用的。

```go
// main/main.go

func startRegistry(wg *sync.WaitGroup) {
	l, _ := net.Listen("tcp", ":9999")
	registry.HandleHTTP()
	wg.Done()
	_ = http.Serve(l, nil)
}

// 在main函数中启动
go startRegistry(&wg)
```

### 2. 启动RPC服务

RPC服务（提供者）在启动时会向注册中心注册自己的地址。

```go
// main/main.go

// 定义一个服务
type Foo int
type Args struct{ Num1, Num2 int }

func (f Foo) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

// 启动服务并注册
func startServer(registryAddr string, wg *sync.WaitGroup) {
	var foo Foo
	l, _ := net.Listen("tcp", ":0") // 监听一个随机端口
	server := gorpc.NewServer()
	_ = server.Register(&foo) // 注册服务
	// 发送心跳到注册中心
	registry.Heartbeat(registryAddr, "tcp@"+l.Addr().String(), 0)
	wg.Done()
	server.Accept(l)
}

// 在main函数中启动两个服务实例
go startServer(registryAddr, &wg)
go startServer(registryAddr, &wg)
```

### 3. 启动RPC客户端并发起调用

客户端从注册中心获取服务列表，并使用负载均衡策略来调用远程服务。

#### 普通调用 (Call)

`Call`会根据负载均衡策略选择一个服务实例进行调用。

```go
// main/main.go

func call(registry string) {
    // 创建一个支持服务发现的客户端
	d := xclient.NewGoRegistryDiscovery(registry, 0)
	xc := xclient.NewXClient(d, xclient.RandomSelect, nil) // 使用随机选择策略
	defer func() { _ = xc.Close() }()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var reply int
            // 发起同步调用
			err := xc.Call(context.Background(), "Foo.Sum", &Args{Num1: i, Num2: i * i}, &reply)
			if err != nil {
				log.Printf("call Foo.Sum error: %v", err)
			} else {
				log.Printf("call Foo.Sum success: %d + %d = %d", i, i*i, reply)
			}
		}(i)
	}
	wg.Wait()
}
```

#### 广播调用 (Broadcast)

`Broadcast`会向注册中心的所有服务实例都发起一次调用，并返回其中一个成功的结果。

```go
// main/main.go

func broadcast(registry string) {
	d := xclient.NewGoRegistryDiscovery(registry, 0)
	xc := xclient.NewXClient(d, xclient.RandomSelect, nil)
	defer func() { _ = xc.Close() }()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var reply int
            // 发起广播调用
			err := xc.Broadcast(context.Background(), "Foo.Sum", &Args{Num1: i, Num2: i * i}, &reply)
			if err != nil {
				log.Printf("broadcast Foo.Sum error: %v", err)
			} else {
				log.Printf("broadcast Foo.Sum success: %d + %d = %d", i, i*i, reply)
			}
		}(i)
	}
	wg.Wait()
}
```

### 4. 运行示例

你可以直接运行 `main/main.go` 来体验整个流程：

```bash
go run main/main.go
```

你将会看到注册中心、两个RPC服务依次启动，然后客户端会发起同步调用和广播调用，并打印出结果。

## 💡 核心设计

- **协议协商**: 客户端和服务端在建立连接后，会先交换一个包含`MagicNumber`和编码方式（`CodecType`）的`Option`信息，确保双方协议一致。
- **服务抽象**: `service.go`中的`service`结构体是对一个RPC服务的抽象。它在启动时通过反射解析出符合RPC规则的方法（可导出、两个参数、一个error返回值），并存储起来。
- **心跳机制**: `registry.go`中的`Heartbeat`函数会启动一个goroutine，定期向注册中心发送POST请求，以证明服务实例仍然存活。注册中心会移除长时间没有心跳的服务。
- **上下文控制**: 客户端的`Call`方法接受一个`context.Context`参数，可以方便地实现超时控制和请求取消。

