package goRpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"goRpc/codec"
	"io"
	"log"
	"net"
	"reflect"
	"strings"
	"sync"
	"time"
)

const MagicNumber = 0x3bef5c

// Option
//
//	@Description: 通信过程协商，包括编解码方式
type Option struct {
	MagicNumber    int32         // MagicNumber marks this's a geerpc request
	CodecType      codec.Type    // client may choose different Codec to encode body
	ConnectTimeout time.Duration // 连接超时时间，默认0表示不超时
	HandleTimeout  time.Duration // 处理超时时间，默认0表示不超时
}

var DefaultOption = &Option{
	MagicNumber:    MagicNumber,
	CodecType:      codec.GobType,
	ConnectTimeout: time.Second * 10,
	//HandleTimeout:  0,
}

// Server
//
//	@Description:RPC服务端
type Server struct {
	serviceMap sync.Map // 并发安全的服务映射，键为服务名，值为服务实例
}

// NewServer
//
//	@Description: 创建RPC服务端实例
//	@return *Server
func NewServer() *Server {
	return &Server{}
}

var DefaultServer = NewServer()

// Accept
//
//	@Description: 监听并接受连接，为每个连接创建一个goroutine处理
//	@receiver s
//	@param lis 监听的网络地址
func (server *Server) Accept(lis net.Listener) {
	for {
		conn, err := lis.Accept()
		if err != nil {
			log.Println("rpc server: accept error:", err)
			continue
		}
		go server.ServeConn(conn)
	}
}

// Accept
//
//	@Description: 监听并接受连接，为每个连接创建一个goroutine处理
//	@param lis 监听的网络地址
func Accept(lis net.Listener) {
	DefaultServer.Accept(lis)
}

// ServeConn
//
//	@Description: 处理单个连接，读取Option，根据CodecType创建CodecFunc函数，然后调用serveCodec处理连接
//	@receiver server 服务端实例
//	@param conn 连接实例
func (server *Server) ServeConn(conn io.ReadWriteCloser) {
	defer func() {
		_ = conn.Close()
	}()
	var opt Option
	if err := json.NewDecoder(conn).Decode(&opt); err != nil {
		log.Println("rpc server: decode option error:", err)
		return
	}
	if opt.MagicNumber != MagicNumber {
		log.Println("rpc server: invalid magic number:", opt.MagicNumber)
		return
	}
	codecFunc := codec.NewCodecFuncMap[opt.CodecType]
	if codecFunc == nil {
		log.Println("rpc server: invalid codec type:", opt.CodecType)
		return
	}
	server.serveCodec(codecFunc(conn), &opt)
}

var invalidRequest = struct{}{}

// serveCodec
//
//	@Description: 处理单个连接，读取请求，处理请求，回复请求
//	@receiver server
//	@param cc 连接实例
func (server *Server) serveCodec(cc codec.Codec, opt *Option) {
	sending := new(sync.Mutex)
	wg := new(sync.WaitGroup)
	for {
		req, err := server.readRequest(cc)
		if err != nil {
			if req == nil {
				break // it's not possible to recover, so close the connection
			}
			req.h.Error = err.Error()
			server.sendResponse(cc, req.h, invalidRequest, sending)
			continue
		}
		wg.Add(1)
		go server.handleRequest(cc, req, sending, wg, opt.HandleTimeout)
	}
	wg.Wait()
	_ = cc.Close()
}

// request
// @Description: stores all information of a call
type request struct {
	h            *codec.Header // request header
	argv, replyv reflect.Value // argv and replyv of request
	mtype        *methodType
	svc          *service
}

// readRequestHeader
//
//	@Description: read request header from connection
//	@receiver server 服务端实例
//	@param cc 连接实例
//	@return *codec.Header 请求头
//	@return error 错误信息
func (server *Server) readRequestHeader(cc codec.Codec) (*codec.Header, error) {
	var h codec.Header
	if err := cc.ReadHeader(&h); err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Println("rpc server: read header error:", err)
		}
		return nil, err
	}
	return &h, nil
}

// readRequest
//
//	@Description: read request from connection
//	@receiver server 服务端实例
//	@param cc 连接实例
//	@return *request 请求实例
//	@return error 错误信息
func (server *Server) readRequest(cc codec.Codec) (*request, error) {
	h, err := server.readRequestHeader(cc)
	if err != nil {
		return nil, err
	}
	req := &request{h: h}
	req.svc, req.mtype, err = server.findService(h.ServiceMethod)
	if err != nil {
		return nil, err
	}
	req.argv = req.mtype.newArgv()
	req.replyv = req.mtype.newReplyv()

	argvi := req.argv.Interface()
	if req.argv.Type().Kind() != reflect.Ptr {
		argvi = req.argv.Addr().Interface()
	}
	if err = cc.ReadBody(argvi); err != nil {
		log.Println("rpc server: read body err:", err)
		return req, err
	}
	return req, nil
}

// sendResponse
//
//	@Description: 发送响应
//	@receiver server
//	@param cc 连接实例
//	@param h 请求头
//	@param body 响应体
//	@param sending 互斥锁，确保响应发送是原子操作
func (server *Server) sendResponse(cc codec.Codec, h *codec.Header, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()
	if err := cc.Write(h, body); err != nil {
		log.Println("rpc server: write response error:", err)
	}
}

// handleRequest
//
//	@Description: 处理请求，根据请求头调用注册的方法，回复请求
//	@receiver server
//	@param cc 连接实例
//	@param req 请求实例
//	@param sending 互斥锁，确保响应发送是原子操作
//	@param wg 等待组，确保所有请求处理完成后关闭连接
func (server *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup, timeout time.Duration) {
	// day 1, just print argv and send a hello message
	defer wg.Done()
	called := make(chan struct{})
	sent := make(chan struct{})
	go func() {
		err := req.svc.call(req.mtype, req.argv, req.replyv)
		called <- struct{}{}
		if err != nil {
			req.h.Error = err.Error()
			server.sendResponse(cc, req.h, invalidRequest, sending)
			sent <- struct{}{}
			return
		}
		server.sendResponse(cc, req.h, req.replyv.Interface(), sending)
		sent <- struct{}{}
	}()
	if timeout == 0 {
		<-called
		<-sent
		return
	}
	select {
	case <-called:
		<-sent
	case <-time.After(timeout):
		req.h.Error = fmt.Sprintf("rpc server: request handle timeout: expect within %s", timeout)
		server.sendResponse(cc, req.h, invalidRequest, sending)
	}

}

// Register
//
//	@Description: 注册服务，将服务实例存储到服务映射中
//	@receiver server
//	@param rcvr 服务实例
//	@return error 错误信息
func (server *Server) Register(rcvr interface{}) error {
	s := newService(rcvr)
	if _, dup := server.serviceMap.LoadOrStore(s.name, s); dup {
		return errors.New("rpc: service already defined: " + s.name)
	}
	return nil
}

// Register
//
//	@Description: 注册服务，将服务实例存储到默认服务映射中
//	@param rcvr 服务实例
//	@return error 错误信息
func Register(rcvr interface{}) error {
	return DefaultServer.Register(rcvr)
}

// findService
//
//	@Description: 查找服务和方法
//	@receiver server
//	@param serviceMethod 服务方法名，格式为"服务名.方法名"
//	@return svc 服务实例
//	@return mtype 方法类型
//	@return err 错误信息
func (server *Server) findService(serviceMethod string) (svc *service, mtype *methodType, err error) {
	dot := strings.Index(serviceMethod, ".")
	if dot < 0 {
		err = errors.New("rpc: service/method not found: " + serviceMethod)
		return
	}
	serviceName, methodName := serviceMethod[:dot], serviceMethod[dot+1:]
	if svci, ok := server.serviceMap.Load(serviceName); !ok {
		err = errors.New("rpc: service not found: " + serviceName)
		return
	} else {
		svc = svci.(*service)
	}
	if mtype = svc.method[methodName]; mtype == nil {
		err = errors.New("rpc: method not found: " + methodName)
	}
	return
}
