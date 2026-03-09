package xclient

import (
	"context"
	. "goRpc"
	"io"
	"reflect"
	"sync"
)

// XClient
//
//	@Description: 代表一个RPC客户端，支持服务发现和负载均衡
type XClient struct {
	d       Discovery
	mode    SelectMode
	opt     *Option
	mu      sync.Mutex         // 并发保护(保护客户端相关操作的并发安全)
	clients map[string]*Client // 存储已创建的客户端，键为服务地址
}

var _ io.Closer = (*XClient)(nil)

func NewXClient(d Discovery, mode SelectMode, opt *Option) *XClient {
	return &XClient{
		d:       d,
		mode:    mode,
		opt:     opt,
		clients: make(map[string]*Client),
	}
}

// Close
//
//	@Description: 关闭所有已创建的客户端连接
//	@receiver xc XClient实例
//	@return error 关闭客户端连接时可能返回的错误
func (xc *XClient) Close() error {
	xc.mu.Lock()
	defer xc.mu.Unlock()
	for key, client := range xc.clients {
		_ = client.Close()
		delete(xc.clients, key)
	}
	return nil
}

// dial
//
//	@Description: 尝试创建或获取一个客户端连接
//	@receiver xc
//	@param rpcaddr 服务地址
//	@return *Client 客户端实例
//	@return error 创建客户端连接时可能返回的错误
func (xc *XClient) dial(rpcaddr string) (*Client, error) {
	xc.mu.Lock()
	defer xc.mu.Unlock()
	client, ok := xc.clients[rpcaddr]
	if ok && !client.IsAvailable() {
		_ = client.Close()
		delete(xc.clients, rpcaddr)
		client = nil
	}
	if client == nil {
		var err error
		client, err = XDial(rpcaddr, xc.opt)
		if err != nil {
			return nil, err
		}
		xc.clients[rpcaddr] = client
	}
	return client, nil
}

// call
//
//	@Description: 调用指定服务方法
//	@receiver xc
//	@param rpcAddr 服务地址
//	@param ctx 上下文，用于取消和超时控制
//	@param serviceMethod 服务名.方法名
//	@param args 调用的参数
//	@param reply 调用的返回值
//	@return error 调用时可能返回的错误
func (xc *XClient) call(rpcAddr string, ctx context.Context, serviceMethod string, args, reply interface{}) error {
	client, err := xc.dial(rpcAddr)
	if err != nil {
		return err
	}
	return client.Call(ctx, serviceMethod, args, reply)
}

// Call
//
//	@Description: 调用指定服务方法，根据负载均衡策略选择服务地址
//	@receiver xc
//	@param ctx 上下文，用于取消和超时控制
//	@param serviceMethod 服务名.方法名
//	@param args 调用的参数
//	@param reply 调用的返回值
//	@return error 调用时可能返回的错误
func (xc *XClient) Call(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	rpcAddr, err := xc.d.Get(xc.mode)
	if err != nil {
		return err
	}
	return xc.call(rpcAddr, ctx, serviceMethod, args, reply)
}

// Broadcast
//
//	@Description: 广播调用指定服务方法，并发调用所有服务实例
//	@receiver xc XClient实例
//	@param ctx 上下文，用于取消和超时控制
//	@param serviceMethod 服务名.方法名
//	@param args 调用的参数
//	@param reply 调用的返回值
//	@return error 调用时可能返回的错误
func (xc *XClient) Broadcast(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	servers, err := xc.d.GetAll()
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	var mu sync.Mutex // protect e and replyDone
	var e error
	replyDone := reply == nil // if reply is nil, don't need to set value
	ctx, cancel := context.WithCancel(ctx)
	for _, rpcAddr := range servers {
		wg.Add(1)
		go func(rpcAddr string) {
			defer wg.Done()
			var clonedReply interface{}
			if reply != nil {
				clonedReply = reflect.New(reflect.ValueOf(reply).Elem().Type()).Interface()
			}
			err := xc.call(rpcAddr, ctx, serviceMethod, args, clonedReply)
			mu.Lock()
			if err != nil && e == nil {
				e = err
				cancel() // if any call failed, cancel unfinished calls
			}
			if err == nil && !replyDone {
				reflect.ValueOf(reply).Elem().Set(reflect.ValueOf(clonedReply).Elem())
				replyDone = true
			}
			mu.Unlock()
		}(rpcAddr)
	}
	wg.Wait()
	return e
}
