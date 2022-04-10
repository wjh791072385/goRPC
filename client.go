package goRPC

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/wjh791072385/gorpc/codec"
)

//Call 封装rpc的客户端请求
type Call struct {
	Seq           uint64
	ServiceMethod string
	Args          interface{}
	Reply         interface{}
	Error         error
	Done          chan *Call //实现异步调用，调用完成后通知调用方
}

func (call *Call) done() {
	call.Done <- call
}

type Client struct {
	cc  codec.Codec
	opt *Option

	sending sync.Mutex // protect following
	header  codec.Header

	mu       sync.Mutex // protect following
	seq      uint64
	pending  map[uint64]*Call //pending 存储未处理完的请求，键是编号，值是 Call 实例
	closing  bool             // user has called Close
	shutdown bool             // server has told us to stop
}

// 断言client实现了closer接口
var _ io.Closer = (*Client)(nil)

var ErrShutdown = errors.New("connection is shut down")

func (client *Client) Close() error {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.closing {
		return ErrShutdown
	}
	client.closing = true
	return client.cc.Close()
}

// IsAvailable return true if the client does work
func (client *Client) IsAvailable() bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	return !client.shutdown && !client.closing
}

// 注册调用请求，返回唯一序号
func (client *Client) registerCall(call *Call) (uint64, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.shutdown || client.closing {
		return 0, ErrShutdown
	}

	call.Seq = client.seq
	client.pending[call.Seq] = call
	client.seq++
	return call.Seq, nil
}

func (client *Client) removeCall(seq uint64) *Call {
	client.mu.Lock()
	defer client.mu.Unlock()
	call := client.pending[seq]
	delete(client.pending, seq)
	return call
}

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

// NewHTTPClient 支持HTTP
func NewHTTPClient(conn net.Conn, opt *Option) (*Client, error) {
	_, _ = io.WriteString(conn, fmt.Sprintf("CONNECT %s HTTP/1.0\n\n", defaultRPCPath))

	//发送connect请求，接收响应
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "CONNECT"})
	if err == nil && resp.Status == connected {
		return NewClient(conn, opt)
	}

	if err == nil {
		err = errors.New("unexpected HTTP response: " + resp.Status)
	}
	return nil, err

}

// NewClient 初始化Client对象，传出conn和opt
func NewClient(conn net.Conn, opt *Option) (*Client, error) {
	//当前只支持gob编码
	f := codec.NewCodeFuncMap[opt.CodecType]
	if f == nil {
		err := fmt.Errorf("invalid codec type %s", opt.CodecType)
		log.Println("rpc client: get codec error:", err)
		return nil, err
	}

	//发送初始的option给server,采用json编码
	if err := json.NewEncoder(conn).Encode(opt); err != nil {
		log.Println("rpc client : json encode error")
		_ = conn.Close()
		return nil, err
	}

	client := &Client{
		cc:      f(conn),
		opt:     opt,
		seq:     1, //0表示invalid,从1开始
		pending: make(map[uint64]*Call),
	}

	go client.receive() //开启goroutine接收请求
	return client, nil
}

// 接收请求
func (client *Client) receive() {
	var err error
	for err == nil {
		var h codec.Header
		//从连接中获取请求头
		err = client.cc.ReadHeader(&h)
		if err != nil {
			break
		}

		call := client.removeCall(h.Seq)
		switch {
		//call不存在
		case call == nil:
			err = client.cc.ReadBody(nil)

		//call存在，但服务端处理出问题了
		case h.Error != "":
			call.Error = fmt.Errorf(h.Error)
			err = client.cc.ReadBody(nil)
			call.done()

		//call存在，一切正常，从body中读取消息
		default:
			err = client.cc.ReadBody(call.Reply)
			if err != nil {
				call.Error = errors.New("reading body " + err.Error())
			}

			//调用done函数，通知Call调用以完成
			call.done()
		}
	}

	//如果循环中断，表明存在ReadHeader出错，终止所有请求
	client.terminateCalls(err)
}

type clientResult struct {
	client *Client
	err    error
}

func Dial(network, address string, opts ...*Option) (cli *Client, err error) {
	opt, err := parseOptions(opts...)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialTimeout(network, address, opt.ConnectTimeout)
	if err != nil {
		return nil, err
	}

	defer func() {
		//最终err还是不为空的话，关闭连接
		if err != nil {
			_ = conn.Close()
		}
	}()

	//创建channel用于超时处理
	ch := make(chan clientResult)
	go func() {
		cli, err = NewClient(conn, opt)
		ch <- clientResult{client: cli, err: nil}
	}()

	//ConnectTimeout=0表示不需要超时处理
	if opt.ConnectTimeout == 0 {
		res := <-ch
		return res.client, res.err
	}

	//通过select处理超时
	select {
	case <-time.After(opt.ConnectTimeout):
		return nil, fmt.Errorf("rpc client: connect timeout: expect within %s", opt.ConnectTimeout)
	case res := <-ch:
		return res.client, res.err
	}
}

// 将opt变为可选参数，初始化摸男人参数
func parseOptions(opts ...*Option) (*Option, error) {
	if len(opts) == 0 || opts[0] == nil {
		return DefaultOption, nil
	}

	if len(opts) != 1 {
		return nil, errors.New("number of options is more than 1")
	}

	opt := opts[0]
	opt.MagicNumber = DefaultMagicNumber //表示微服务
	if opt.CodecType == "" {
		opt.CodecType = DefaultOption.CodecType
	}
	return opt, nil
}

func (client *Client) send(call *Call) {
	client.sending.Lock()
	defer client.sending.Unlock()

	//先在client注册,在注册函数中为call的seq赋值
	seq, err := client.registerCall(call)
	if err != nil {
		call.Error = err
		call.done()
		return
	}

	//将请求封装成符合目标服务端接收方式
	client.header.Seq = seq
	client.header.ServiceMethod = call.ServiceMethod
	client.header.Error = ""

	//encode and send request
	if err := client.cc.Write(&client.header, call.Args); err != nil {
		c := client.removeCall(seq)
		if c != nil {
			c.Error = err
			c.done()
		}
	}
}

// Go 异步调用方法，调用完成后直接返回*Call
func (client *Client) Go(serviceMethod string, args, reply interface{}, done chan *Call) *Call {
	if done == nil {
		done = make(chan *Call, 10)
	} else if cap(done) == 0 {
		log.Println("rpc client : done channel is unbuffered")
		return nil
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

// Call 同步调用方法，等待call完成
func (client *Client) Call(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	//call := <-client.Go(serviceMethod, args, reply, make(chan *Call, 1)).Done
	call := client.Go(serviceMethod, args, reply, make(chan *Call, 1))
	select {
	case c := <-call.Done:
		return c.Error
	case <-ctx.Done():
		//超时的话，需要移除该call
		call = client.removeCall(call.Seq)
		//return fmt.Errorf("rpc client : call timeout %s", call.Error.Error())  //call.Error为空报错
		return fmt.Errorf("rpc client : call timeout %s %d", call.ServiceMethod, call.Seq)
	}

}
