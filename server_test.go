package goRPC_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/wjh791072385/gorpc/codec"

	goRPC "github.com/wjh791072385/gorpc"
)

// 测试主流程 server端
func TestServer(t *testing.T) {
	//启动服务端
	lis, err := net.Listen("tcp", "localhost:10010")
	if err != nil {
		log.Println("server start failed")
	}
	goRPC.Accept(lis)
}

// 模拟客户端进行测试
func TestClientSimulate(t *testing.T) {
	conn, _ := net.Dial("tcp", "localhost:10010")
	defer conn.Close()

	//等一秒在发送请求
	time.Sleep(time.Second)
	//发送option,采用json编码，将消息op编码进conn
	json.NewEncoder(conn).Encode(goRPC.DefaultOption)
	cc := codec.NewGobCodec(conn)

	//send request & receive reply
	for i := 0; i < 5; i++ {
		h := &codec.Header{
			ServiceMethod: "algorithm.sum", //执行algorithm服务下的sum函数
			Seq:           uint64(i),       //请求序号，用于区分不同的请求
		}

		//发送消息头和消息体,这里是发送到buf中, buf->gob->conn
		_ = cc.Write(h, fmt.Sprintf("gorpc req %d", h.Seq))

		//接收消息头和消息体, 如果conn中没数据，会阻塞在此处，通过注销上面cc.write验证
		var head = new(codec.Header)
		_ = cc.ReadHeader(head)
		log.Println(head)

		var reply string
		_ = cc.ReadBody(&reply)
		log.Println("reply:", reply)
	}
	select {}
}

//随机端口测试
func startServer(addr chan string) {
	// pick a free port
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal("network error:", err)
	}
	log.Println("start rpc server on", l.Addr())
	addr <- l.Addr().String()
	goRPC.Accept(l)
}

// 测试客户端
func TestStandard(t *testing.T) {
	//启动服务端
	addr := make(chan string)
	go startServer(addr)

	//启动客户端,采用默认option
	client, err := goRPC.Dial("tcp", <-addr)
	defer client.Close()

	if err != nil {
		log.Println("client start error")
	}

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := fmt.Sprintf("geerpc req %d", i)
			var reply string
			if err := client.Call(context.Background(), "Foo.Sum", args, &reply); err != nil {
				log.Fatal("call Foo.Sum error:", err)
			}
			log.Println("reply:", reply)
		}(i)
	}
	wg.Wait()

}
