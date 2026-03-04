package gorpc

import (
	"encoding/json"
	"fmt"
	"goRpc/codec/codec"
	"io"
	"log"
	"net"
	"reflect"
	"sync"
)

const MagicNumber = 0x3bef5c

// Option
//
//	@Description: 通信过程协商，包括编解码方式
type Option struct {
	MagicNumber int32      // MagicNumber marks this's a geerpc request
	CodecType   codec.Type // client may choose different Codec to encode body
}

var DefaultOption = &Option{
	MagicNumber: MagicNumber,
	CodecType:   codec.GobType,
}

// Server
//
//	@Description:RPC服务端
type Server struct{}

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
	server.serveCodec(codecFunc(conn))
}

var invalidRequest = struct{}{}

// serveCodec
//
//	@Description: 处理单个连接，读取请求，处理请求，回复请求
//	@receiver server
//	@param cc 连接实例
func (server *Server) serveCodec(cc codec.Codec) {
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
		go server.handleRequest(cc, req, sending, wg)
	}
	wg.Wait()
	_ = cc.Close()
}

// request
// @Description: stores all information of a call
type request struct {
	h            *codec.Header // request header
	argv, replyv reflect.Value // argv and replyv of request
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
	// TODO: now we don't know the type of request argv
	// day 1, just suppose it's string
	req.argv = reflect.New(reflect.TypeOf(""))
	if err = cc.ReadBody(req.argv.Interface()); err != nil {
		log.Println("rpc server: read argv err:", err)
	}
	return req, nil
}

func (server *Server) sendResponse(cc codec.Codec, h *codec.Header, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()
	if err := cc.Write(h, body); err != nil {
		log.Println("rpc server: write response error:", err)
	}
}

func (server *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup) {
	// TODO, should call registered rpc methods to get the right replyv
	// day 1, just print argv and send a hello message
	defer wg.Done()
	log.Println(req.h, req.argv.Elem())
	req.replyv = reflect.ValueOf(fmt.Sprintf("geerpc resp %d", req.h.Seq))
	server.sendResponse(cc, req.h, req.replyv.Interface(), sending)
}
