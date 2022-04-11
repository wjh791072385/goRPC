package xclient

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"sync"

	goRPC "github.com/wjh791072385/gorpc"
)

type XClient struct {
	d       Discovery
	mode    SelectMode
	opt     *goRPC.Option
	mu      sync.Mutex
	clients map[string]*goRPC.Client
}

var _ io.Closer = (*XClient)(nil)

func NewXClient(d Discovery, mode SelectMode, opt *goRPC.Option) *XClient {
	return &XClient{d: d, mode: mode, opt: opt, clients: make(map[string]*goRPC.Client)}
}

func (xc *XClient) Close() error {
	xc.mu.Lock()
	defer xc.mu.Unlock()
	for key, client := range xc.clients {
		_ = client.Close()
		delete(xc.clients, key)
	}
	return nil
}

func (xc *XClient) dial(addr string) (*goRPC.Client, error) {
	xc.mu.Lock()
	defer xc.mu.Unlock()

	client, ok := xc.clients[addr]
	if ok && !client.IsAvailable() {
		_ = client.Close()
		delete(xc.clients, addr)
		client = nil
	}

	if client == nil {
		//如果没有缓存的client,那么重新拨号建立连接
		var err error
		client, err = goRPC.Dial("tcp", addr, xc.opt)
		if err != nil {
			return nil, err
		}
		xc.clients[addr] = client
	}

	return client, nil
}

func (xc *XClient) call(rpcAddr string, ctx context.Context, serviceMethod string, args, reply interface{}) error {
	client, err := xc.dial(rpcAddr)
	if err != nil {
		return fmt.Errorf("%s is disabled, err : %s", rpcAddr, err)
	}
	return client.Call(ctx, serviceMethod, args, reply)
}

// Call 通过实现已经实现的Dicover接口中的get方法，得到可用的服务地址
func (xc *XClient) Call(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	rpcAddr, err := xc.d.Get(xc.mode)
	//log.Println("selected rpcAddr : ", rpcAddr)
	if err != nil {
		return err
	}
	return xc.call(rpcAddr, ctx, serviceMethod, args, reply)
}

// Broadcast 实现广播方法，对所有服务进行调用
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
				cancel() // 只要其中一个call调用失败，就结束全部
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
