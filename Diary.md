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


# Day2
## 20260306

### 1. 调用方法设计
一个方法要能被远程调用，需要满足以下条件：
1）方法必须是导出的（首字母大写）
2）方法必须有两个导出类型的参数，第一个参数是接收者，第二个参数是指针类型
3）方法必须有一个返回值，类型是 error
```abpublidot
func (t *T) MethodName(argType T1, replyType *T2) error
```
以此为蓝本封装结构体Call，其中Done通道用于异步调用，调用完成后将调用对象发送到Done通道，无需等待调用完成即可继续执行后续代码

### 2.客户端设计
一个客户端可能有多个Call，且被多个goroutine并发调用（并发安全）

### 3.在客户端注册调用，移除调用，中止调用
1）注册调用时，客户端需要生成一个唯一的序号，用于匹配请求和响应，将调用对象存储在pending map中，键为序号
2）移除调用时，客户端需要根据序号从pending map中删除调用对象
3）中止调用时，客户端需要将所有未完成的调用对象的错误设置为指定的错误，然后从pending map中删除调用对象

### 4.接收响应
有三个可能的情况：
1）call不存在，但服务端仍然处理了请求，需要读取body但不做处理
2）call存在，但处理出错，即头部错误信息不为空，需要读取body但不做处理
3）call存在，且处理成功，即头部错误信息为空，需要从body中读取返回值

### 5.创建客户端实例
创建时需要完成一开始的option协议交换，协商好编码方式后，创建一个子协程调用receive（）接收响应

receive的存在使得客户端可以异步接收响应，而不会阻塞在发送请求的过程中，从而提高了并发性能，解耦了发送和接收的过程，使得客户端可以同时处理多个请求和响应

### 6.实现客户端调用，Dial函数
实现dial函数，用于传入服务端地址，创建client实例，完成option协议交换，option实现为可选参数

dial函数主要是为了获取一个连接实例，完成option协议交换，返回一个client实例

### 7.实现发送函数，用于发送请求
主要通过Call，Go方法实现

Go是一个异步调用方法，返回一个Call对象，调用者可以通过Call对象的Done通道获取调用结果
call方法用于同步调用，阻塞直到收到响应或发生错误

## 通过反射实现服务注册

### 8.通过反射实现service与结构体的映射关系
1）定义结构体methodType
根据数据类型分别创建对应的反射值，用于调用方法时传递参数和接收返回值

2）定义结构体service
name 映射的结构体名称
typ 映射的结构体反射类型
rcvr 映射的结构体实例
method 映射的方法映射，键为方法名，值为存储映射的结构体的所有符合条件的方法。

向服务中注册方法时，需要判断方法是否符合条件，即是否是导出的，是否有两个导出类型的参数，是否有一个返回值，类型是 error
如果符合条件，就将方法存储在service的method映射中，键为方法名，值为存储映射的结构体的所有符合条件的方法。

3）实现Call方式，使得能用反射值调用方法
```abpublidot
客户端请求："Calculator.Add" with args {A:10, B:20}
                ↓
服务端收到请求
                ↓
解析 header 得到 "Calculator.Add"
                ↓
查找 service.method["Add"]
                ↓
创建参数实例：
  argv = &Args{A:10, B:20}
  replyv = new(int)
                ↓
从请求体解码数据到 argv
                ↓
【调用 call 函数】⬅️ 当前函数
  │
  ├─> atomic.AddUint64(&m.numCalls, 1)  // 计数 +1
  ├─> f = m.method.Func                 // 获取方法
  ├─> returnValues = f.Call([...])      // 反射调用
  │        │
  │        └─> 实际执行：(*Calculator).Add(rcvr, argv, replyv)
  │                  │
  │                  └─> *reply = 30
  │                      return nil
  │
  ├─> returnValues[0] = nil (error)
  └─> return nil
                ↓
将 replyv 的值（30）编码并返回给客户端
```









