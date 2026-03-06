package main

import (
	"fmt"
	gorpc "goRpc"
	"log"
	"net"
	"sync"
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
	log.SetFlags(0)
	addr := make(chan string)
	go startServer(addr)
	client, _ := gorpc.Dial("tcp", <-addr)
	defer func() { _ = client.Close() }()

	time.Sleep(time.Second)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := fmt.Sprintf("gorpc req %d", i)
			var reply string
			if err := client.Call("Foo.Sum", args, &reply); err != nil {
				log.Fatal("rpc client: call Foo.Sum error:", err)
			}
			log.Println("rpc client: call Foo.Sum reply:", reply)
		}(i)
	}
	wg.Wait()
}
