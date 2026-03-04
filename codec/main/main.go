package main

import (
	"encoding/json"
	"fmt"
	gorpc "goRpc/codec"
	"goRpc/codec/codec"
	"log"
	"net"
	"time"
)

// startServer
//
//	@Description: 启动RPC服务端，监听随机端口并将地址写入通道
//	@param addr 用于返回服务端地址的通道
func startServer(addr chan string) {
	// 服务端监听随机端口
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal("rpc server: listen error:", err)
	}
	log.Println("rpc server: start on", l.Addr())
	addr <- l.Addr().String()
	gorpc.Accept(l)
}
func main() {
	addr := make(chan string)
	go startServer(addr)

	// 客户端连接服务端
	conn, _ := net.Dial("tcp", <-addr)
	defer func() { _ = conn.Close() }()

	time.Sleep(time.Second)

	// 客户端发送option
	_ = json.NewEncoder(conn).Encode(gorpc.DefaultOption)
	cc := codec.NewGobCodec(conn)

	// 客户端发送请求，接收响应
	for i := 0; i < 5; i++ {
		h := &codec.Header{
			ServiceMethod: "Foo.Sum",
			Seq:           uint64(i),
		}
		_ = cc.Write(h, fmt.Sprintf("geerpc req %d", i))
		_ = cc.ReadHeader(h)
		var reply string
		_ = cc.ReadBody(&reply)
		log.Println("reply:", reply)
	}
}
