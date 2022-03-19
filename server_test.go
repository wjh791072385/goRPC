package goRPC_test

import (
	"log"
	"net"
	"testing"

	goRPC "github.com/wjh791072385/gorpc"
)

// 测试主流程 server端
func TestServer(t *testing.T) {
	//启动服务端
	lis, err := net.Listen("tcp", "localhost:8888")
	if err != nil {
		log.Println("server start failed")
	}
	goRPC.Accept(lis)
	select {}
}

// 模拟客户端进行测试
