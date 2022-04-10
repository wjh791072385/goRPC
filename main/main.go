package main

import (
	"context"
	"log"
	"net"
	"sync"
	"time"

	goRPC "github.com/wjh791072385/gorpc"
	"github.com/wjh791072385/gorpc/xclient"
)

type Foo int

type Args struct{ Num1, Num2 int }

func (f Foo) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

func (f Foo) Sleep(args Args, reply *int) error {
	time.Sleep(time.Second * time.Duration(args.Num1))
	*reply = args.Num1 + args.Num2
	return nil
}

func startServer(addr chan string) {
	var foo Foo
	//if err := goRPC.Register(&foo); err != nil {
	//	log.Fatalln("register error", err)
	//}

	l, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal("network error:", err)
	}
	// //使用默认的defaultServer
	//log.Println("start rpc server on", l.Addr())
	addr <- l.Addr().String()
	//goRPC.Accept(l)
	server := goRPC.NewServer()
	err = server.Register(&foo)
	if err != nil {
		log.Fatalln("register error", err)
		return
	}
	server.Accept(l)
}

//用与测试单点call
func call(addr1, addr2 string) {
	d := xclient.NewMultiServersDiscovery([]string{addr1, addr2})
	xc := xclient.NewXClient(d, xclient.RandomSelect, nil)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var reply int
			args := &Args{Num1: i, Num2: i * i}
			xc.Call(context.Background(), "Foo.Sum", args, &reply)
			log.Printf("call %d + %d = %d", args.Num1, args.Num2, reply)
		}(i)
	}
	wg.Wait()

}

func broadcast(addr1, addr2 string) {
	d := xclient.NewMultiServersDiscovery([]string{addr1, addr2})
	xc := xclient.NewXClient(d, xclient.RandomSelect, nil)
	defer func() { _ = xc.Close() }()
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var reply int
			args := &Args{Num1: i, Num2: i * i}
			err := xc.Broadcast(context.Background(), "Foo.Sum", args, &reply)
			if err != nil {
				log.Printf("broadcast %d + %d = %d failed", args.Num1, args.Num2, reply)
			} else {
				log.Printf("broadcast %d + %d = %d", args.Num1, args.Num2, reply)
			}

			// expect 2 - 5 timeout
			ctx, _ := context.WithTimeout(context.Background(), time.Second*2)
			err = xc.Broadcast(ctx, "Foo.Sleep", args, &reply)
			if err != nil {
				log.Printf("broadcast %d + %d = %d failed %s", args.Num1, args.Num2, reply, err)
			} else {
				log.Printf("broadcast %d + %d = %d", args.Num1, args.Num2, reply)
			}
		}(i)
	}
	wg.Wait()
}

func main() {
	ch1 := make(chan string)
	ch2 := make(chan string)
	// start two servers
	go startServer(ch1)
	go startServer(ch2)

	addr1 := <-ch1
	addr2 := <-ch2

	time.Sleep(time.Second)
	call(addr1, addr2)
	broadcast(addr1, addr2)
}
