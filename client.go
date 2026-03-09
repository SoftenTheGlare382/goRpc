package goRpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"goRpc/codec"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Call
//
//	@Description: 代表一个异步的远程调用
type Call struct {
	Seq           uint64      // 调用的序号，用于匹配响应
	ServiceMethod string      // 服务名.方法名
	Args          interface{} // 调用的参数
	Reply         interface{} // 调用的返回值
	Error         error       // 调用的错误
	Done          chan *Call  // 调用完成的通知通道,支持异步调用
}

// done
//
//	@Description: 调用完成后，将调用对象发送到Done通道
//	@receiver c
func (c *Call) done() {
	c.Done <- c
}

// Client
//
//	@Description: 代表一个RPC客户端
type Client struct {
	cc       codec.Codec
	opt      *Option
	sending  sync.Mutex       //并发保护（保证请求的有序发送）
	header   codec.Header     // 用于存储请求头
	mu       sync.Mutex       // 并发保护(保证客户端相关操作的并发安全)
	seq      uint64           // 全局唯一的序号，用于匹配请求和响应
	pending  map[uint64]*Call // 存储未完成的调用，键为序号
	closing  bool             // 客户端已经通知关闭
	shutdown bool             // 服务端已经告诉我们停止
}

var _ io.Closer = (*Client)(nil)

var ErrShutdown = errors.New("connection is shut down")

// Close
//
//	@Description: 关闭客户端连接
//	@receiver client 客户端实例
//	@return error 关闭连接时可能返回的错误
func (client *Client) Close() error {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.closing {
		return ErrShutdown
	}
	client.closing = true
	return client.cc.Close()
}

// IsAvailable
//
//	@Description: 检查客户端是否可用
//	@receiver client 客户端实例
//	@return bool 是否可用
func (client *Client) IsAvailable() bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	return !client.closing && !client.shutdown
}

// registerCall
//
//	@Description: 注册一个调用，返回调用的序号和可能的错误
//	@receiver client
//	@param call
//	@return uint64 调用的序号
//	@return error 注册调用时可能返回的错误
func (client *Client) registerCall(call *Call) (uint64, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.closing || client.shutdown {
		return 0, ErrShutdown
	}
	call.Seq = client.seq
	client.pending[call.Seq] = call
	client.seq++
	return call.Seq, nil
}

// removeCall
//
//	@Description: 移除一个调用，返回调用对象
//	@receiver client
//	@param seq 调用的序号
//	@return *Call 调用对象
func (client *Client) removeCall(seq uint64) *Call {
	client.mu.Lock()
	defer client.mu.Unlock()
	call := client.pending[seq]
	delete(client.pending, seq)
	return call
}

// terminateCalls
//
//	@Description: 终止所有未完成的调用，将它们的错误设置为err
//	@receiver client 客户端实例
//	@param err 终止调用时要设置的错误
func (client *Client) terminateCalls(err error) {
	client.sending.Lock()
	defer client.sending.Unlock()
	client.mu.Lock()
	defer client.mu.Unlock()
	client.shutdown = true
	for _, call := range client.pending {
		call.Error = err
		call.done()
	}
}

// receive
//
//	@Description: 接收服务端的响应，根据响应类型处理调用
//	@receiver client 客户端实例
func (client *Client) receive() {
	var err error
	for err == nil {
		var h codec.Header
		if err := client.cc.ReadHeader(&h); err != nil {
			break
		}
		call := client.removeCall(h.Seq)
		switch {
		case call == nil:
			err = client.cc.ReadBody(nil) // 调用不存在，读取body但不做处理
		case h.Error != "":
			call.Error = errors.New(h.Error) //  直接创建 error
			err = client.cc.ReadBody(nil)    // 调用存在，但处理出错，读取body但不做处理
			call.done()
		case h.Error == "":
			err = client.cc.ReadBody(call.Reply) // 调用存在，且处理成功，从body中读取返回值
			if err != nil {
				call.Error = fmt.Errorf("read body error: %v", err)
			}
			call.done()
		}
	}
	client.terminateCalls(err)
}

// NewClient
//
//	@Description: 创建一个新的RPC客户端实例
//	@param conn 客户端连接
//	@param opt 客户端选项
//	@return *Client 客户端实例
//	@return error 创建客户端时可能返回的错误
func NewClient(conn net.Conn, opt *Option) (*Client, error) {
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		err := fmt.Errorf("codec type %s not found", opt.CodecType)
		log.Println("rpc client: codec type not found:", err)
		return nil, err
	}
	//协议交换
	if err := json.NewEncoder(conn).Encode(opt); err != nil {
		log.Println("rpc client: encode option error:", err)
		_ = conn.Close()
		return nil, err
	}
	return NewClientCodec(f(conn), opt), nil
}

// NewClientCodec
//
//	@Description: 创建一个新的RPC客户端实例，使用指定的编码方式
//	@param cc 编码方式
//	@param opt 客户端选项
//	@return *Client 客户端实例
func NewClientCodec(cc codec.Codec, opt *Option) *Client {
	client := &Client{
		cc:      cc,
		opt:     opt,
		pending: make(map[uint64]*Call),
	}
	go client.receive()
	return client
}

// parseOptions
//
//	@Description: 解析客户端选项，返回解析后的选项和可能的错误
//	@param opts 客户端选项
//	@return *Option 解析后的选项
//	@return error 解析选项时可能返回的错误
func parseOptions(opts ...*Option) (*Option, error) {
	if len(opts) == 0 || opts[0] == nil {
		return DefaultOption, nil
	}
	if len(opts) != 1 {
		return nil, errors.New("number of options is more than 1")
	}
	opt := opts[0]
	opt.MagicNumber = DefaultOption.MagicNumber
	if opt.CodecType == "" {
		opt.CodecType = DefaultOption.CodecType
	}
	return opt, nil
}

// clientResult
// @Description: 客户端连接结果结构体
type clientResult struct {
	client *Client
	err    error
}

type newClientFunc func(conn net.Conn, opt *Option) (*Client, error)

func dialTimeout(f newClientFunc, network, address string, opts ...*Option) (*Client, error) {
	opt, err := parseOptions(opts...)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout(network, address, opt.ConnectTimeout)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = conn.Close()
		}
	}()
	ch := make(chan clientResult)
	go func() {
		client, err := f(conn, opt)
		ch <- clientResult{client, err}
	}()
	if opt.ConnectTimeout == 0 {
		result := <-ch
		return result.client, result.err
	}
	select {
	case <-time.After(opt.ConnectTimeout):
		return nil, fmt.Errorf("connect timeout: %v", opt.ConnectTimeout)
	case result := <-ch:
		return result.client, result.err
	}
}

// Dial
//
//	@Description: 连接到RPC服务端，返回一个新的客户端实例
//	@param network 网络类型，如"tcp"
//	@param address 服务端地址，如"localhost:8000"
//	@param opts 客户端选项
//	@return client 客户端实例
//	@return err 连接时可能返回的错误
func Dial(network, address string, opts ...*Option) (client *Client, err error) {

	return dialTimeout(NewClient, network, address, opts...)
}

// send
//
//	@Description: 发送一个调用请求到服务端
//	@receiver client 客户端实例
//	@param call 调用对象
func (client *Client) send(call *Call) {
	client.sending.Lock()
	defer client.sending.Unlock()

	seq, err := client.registerCall(call)
	if err != nil {
		call.Error = err
		call.done()
		return
	}

	// 准备请求头
	client.header.ServiceMethod = call.ServiceMethod
	client.header.Seq = seq
	client.header.Error = ""

	//编码并发送请求
	if err := client.cc.Write(&client.header, call.Args); err != nil {
		call := client.removeCall(seq)
		// 调用不存在，或已被处理，忽略错误
		if call != nil {
			call.Error = err
			call.done()
		}
	}
}

// Go
//
//	@Description: 异步调用服务端方法，返回一个调用对象
//	@receiver client
//	@param serviceMethod 服务方法名，格式为"Service.Method"
//	@param args 调用参数
//	@param reply 调用返回值
//	@param done 调用完成通知通道，用于接收调用结果
//	@return *Call 调用对象，包含调用信息和结果
func (client *Client) Go(serviceMethod string, args, reply interface{}, done chan *Call) *Call {
	if done == nil {
		done = make(chan *Call, 10)
	} else if cap(done) == 0 {
		log.Panic("rpc client: done channel is unbuffered")
	}
	call := &Call{
		ServiceMethod: serviceMethod,
		Args:          args,
		Reply:         reply,
		Done:          done,
	}
	client.send(call)
	return call
}

// Call
//
//	@Description: 同步调用服务端方法，返回调用结果或错误
//	@receiver client
//	@param serviceMethod 服务方法名，格式为"Service.Method"
//	@param args 调用参数
//	@param reply 调用返回值
//	@return error 调用时可能返回的错误
func (client *Client) Call(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	call := client.Go(serviceMethod, args, reply, make(chan *Call, 1))
	select {
	case <-ctx.Done():
		client.removeCall(call.Seq)
		return errors.New("rpc client: call failed: " + ctx.Err().Error())
	case call := <-call.Done:
		return call.Error
	}
}

// NewHTTPClient
//
//	@Description: 创建一个HTTP客户端，用于连接到RPC服务端
//	@param conn 网络连接实例
//	@param opt 客户端选项
//	@return *Client 客户端实例
//	@return error 创建客户端时可能返回的错误
func NewHTTPClient(conn net.Conn, opt *Option) (*Client, error) {
	// 发送CONNECT请求
	_, _ = io.WriteString(conn, fmt.Sprintf("CONNECT %s HTTP/1.0\n\n", defaultRPCPath))

	//需要接收到成功的HTTP响应才能转换为RPC协议
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "CONNECT"})

	if err == nil && resp.Status == "200 Connected to RPC Server" {
		return NewClient(conn, opt)
	}
	if err == nil {
		err = errors.New("unexpected HTTP response: " + resp.Status)
	}
	return nil, err
}

// DialHTTP
//
//	@Description: 连接到RPC服务端，返回一个新的HTTP客户端实例
//	@param network 网络类型，如"tcp"
//	@param address 服务端地址，如"localhost:8000"
//	@param opts 客户端选项
//	@return *Client 客户端实例
//	@return error 连接时可能返回的错误
func DialHTTP(network, address string, opts ...*Option) (*Client, error) {
	return dialTimeout(NewHTTPClient, network, address, opts...)
}

// XDial
//
//	@Description: 连接到RPC服务端，根据协议类型选择不同的连接方式
//	@param rpcAddr 服务端地址，格式为 "protocol@addr"
//	@param opts 客户端选项
//	@return *Client 客户端实例
//	@return error 连接时可能返回的错误
func XDial(rpcAddr string, opts ...*Option) (*Client, error) {
	parts := strings.Split(rpcAddr, "@") // rpcAddr 格式为 "protocol@addr"
	if len(parts) != 2 {
		return nil, fmt.Errorf("rpc client: invalid rpcAddr format: %s", rpcAddr)
	}
	protocol, addr := parts[0], parts[1]
	switch protocol {
	case "http":
		// HTTP协议，使用HTTP客户端连接
		return DialHTTP("tcp", addr, opts...)
	default:
		// 其他协议，默认使用TCP
		return Dial(protocol, addr, opts...)
	}
}
