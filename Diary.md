# Day1
## 20260304

### 1. 一个RPC调用需要提供服务名和方法名以及相关参数，响应需要包括错误信息和返回值

请求和响应中的参数和返回值->body

方法名，错误信息和请求序列号->header


### 2. 抽象出编解码接口用于实现不同的codec实例，同时抽象出构造函数，用于创建不同的codec实例，通过Type做区分

可使用类似工厂模式，根据Type创建不同的codec构造函数(暂时仅实现gob codec)

### 3. 定义GobCodec结构体，实现Codec接口

GobCodec结构体分为四部分：conn TCP socket链接实例，dec/enc 解码器/编码器，buf 缓冲区（？？？防止阻塞提升性能）

实现Codec接口的方法：ReadHeader/ReadBody/Write/Close

### 4.通信过程协商，包括编解码方式

用结构体Option承载, 包括MagicNumber和CodecType,固定用JSON编码放在报文开头，然后使用CodecType指定的编码方式编码body

Option放在报文开头，Header和body可以有多对

```abpublidot
| Option | Header1 | Body1 | Header2 | Body2 | ...
```

### 5.实现服务端

实现了 Accept 方式，net.Listener 作为参数，for 循环等待 socket 连接建立，并开启子协程处理，处理过程交给了 ServerConn 方法。

同时实现默认设定，Accept 方法默认使用 DefaultServer 实例，方便使用

使用serveConn方法处理连接，读取Option，根据CodecType创建CodecFunc函数，然后调用serveCodec处理连接

serveCodec 的过程非常简单。主要包含三个阶段 :读取请求 readRequest ,处理请求 handleRequest ,回复请求 sendResponse,但需要注意并发执行处理请求时串行发送回复，尽力而为，header解析失败就中止循环

ps:回复响应时，首先是直接通过连接实例发送option，然后将header和body按顺序编码后写入缓冲区再一起通过链接实例发送

### 6. 实现简易客户端

首先开启服务端，监听随机端口并将地址写入通道，然后开启客户端，连接服务端，发送option，随后发送请求接收响应，最后关闭连接








