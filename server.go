package goRPC

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/wjh791072385/gorpc/codec"
)

const DefaultMagicNumber = 0x3bef5c

//编码思路：RPC 客户端固定采用 JSON 编码 Option，后续的 header 和 body 的编码方式由 Option 中的 CodeType 指定，
//服务端首先使用 JSON 解码 Option，然后通过 Option 的 CodeType 解码剩余的内容

//| Option{MagicNumber: xxx, CodecType: xxx} | Header{ServiceMethod ...} | Body interface{} |
//| <------      固定 JSON 编码      ------>  | <-------   编码方式由 CodeType 决定   ------->|

//具体连接中的报文 | Option | Header1 | Body1 | Header2 | Body2 | ...

type Option struct {
	MagicNumber    int //标识请求类型，DefaultMagicNumber表示rpc请求
	CodecType      codec.Type
	ConnectTimeout time.Duration //规定0表示不限制超时时间
	HandleTimeout  time.Duration
}

var DefaultOption = &Option{
	MagicNumber:    DefaultMagicNumber,
	CodecType:      codec.GobType, //默认采用gdb
	ConnectTimeout: time.Second * 10,
}

// Server 服务端实现
type Server struct {
	serviceMap sync.Map
}

func NewServer() *Server {
	return &Server{}
}

var DefaultServer = NewServer()

func (server *Server) Accept(lis net.Listener) {
	for {
		conn, err := lis.Accept()
		if err != nil {
			log.Println("rpc server: accept error")
			return
		}

		//协程接收处理
		go server.ServeConn(conn)
	}
}

// Accept 封装一层，方便外部调用
func Accept(lis net.Listener) { DefaultServer.Accept(lis) }

// 支持HTTP
const (
	connected        = "200 connected to goRPC" //成功连接的msg
	defaultRPCPath   = "/gorpc/"                //标识rpc访问路径的前缀，比如192.168.1.1:8888/gorpc/Algorithm.Sum
	defaultDebugPath = "/debug/gorpc"           //用于测试
)

//接收http请求
func (server *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != "CONNECT" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = io.WriteString(w, "405 must CONNECT\n")
		return
	}

	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		log.Print("rpc hijacking ", req.RemoteAddr, ": ", err.Error())
		return
	}

	_, _ = io.WriteString(conn, "HTTP/1.0 "+connected+"\n\n")
	server.ServeConn(conn)
}

// HandleHTTP 对默认的rpc路径做出相应的响应
func (server *Server) HandleHTTP() {
	http.Handle(defaultRPCPath, server) //Server实现了ServeHTTP方法，即实现了handler接口
	//http.HandleFunc()
}

// HandleHTTP 对外暴露，采用默认defaultServer
func HandleHTTP() {
	DefaultServer.HandleHTTP()
}

// ServeConn 核心处理逻辑
func (server *Server) ServeConn(conn io.ReadWriteCloser) {
	// 首先使用 json.NewDecoder 反序列化得到 Option 实例，检查 MagicNumber 和 CodeType 的值是否正确。
	//然后根据 CodeType 得到对应的消息编解码器，接下来的处理交给 serverCodec

	defer func() {
		conn.Close()
	}()

	// 解码到Option对象中
	var opt Option
	if err := json.NewDecoder(conn).Decode(&opt); err != nil {
		log.Println("rpc server : option decode error")
		return
	}

	if opt.MagicNumber != DefaultMagicNumber {
		log.Println("rpc server: invalid MagicNumber")
		return
	}

	f := codec.NewCodeFuncMap[opt.CodecType]
	if f == nil {
		log.Println("rpc server : invalid CodeType")
		return
	}

	//这里如果opt.CodeType = "application/gob"，那么f(conn)其实返回的是一个实现Codec接口的GobCodec实例
	server.serveCodec(f(conn), &opt)
}

func (server *Server) Register(rcvr interface{}) error {
	s := newService(rcvr)
	//不存在则插入
	if _, load := server.serviceMap.LoadOrStore(s.name, s); load {
		return errors.New("rpc: service already defined: " + s.name)
	}
	return nil
}

// Register 对外暴露
func Register(rcvr interface{}) error {
	return DefaultServer.Register(rcvr)
}

func (server *Server) findService(serviceMethod string) (sv *service, mt *methodType, err error) {
	//服务名和方法名是按.分割的  比如：algorithm.Sum
	dot := strings.LastIndex(serviceMethod, ".")
	if dot < 0 {
		err = errors.New("rpc server: service/method request ill-formed: " + serviceMethod)
		return
	}

	sName, mName := serviceMethod[:dot], serviceMethod[dot+1:]
	svci, ok := server.serviceMap.Load(sName)
	if !ok {
		err = errors.New("rpc server: can't find service " + sName)
		return
	}

	sv, ok = svci.(*service) //接口断言
	if !ok {
		err = errors.New("rpc server: can't find service " + sName)
		return
	}

	mt = sv.method[mName]
	if mt == nil {
		err = errors.New("rpc server: can't find method " + mName)
	}
	return
}

// 空结构体
var invalidRequest = struct{}{}

//serveCodec 的过程非常简单。主要包含三个阶段
//读取请求 readRequest
//处理请求 handleRequest
//回复请求 sendResponse
func (server *Server) serveCodec(cc codec.Codec, opt *Option) {
	sending := new(sync.Mutex) //确保发送一个完整的响应
	wg := new(sync.WaitGroup)  //确保所有请求被处理

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

		//使其不阻塞，for循环处理请求
		go server.handleRequest(cc, req, sending, wg, opt.HandleTimeout)
	}

	wg.Wait()
}

// request stores all information of a call
type request struct {
	h            *codec.Header // header of request
	argv, replyv reflect.Value // argv and replyv of request
	mtype        *methodType
	svc          *service
}

func (server *Server) readRequestHeader(cc codec.Codec) (*codec.Header, error) {
	var h codec.Header

	if err := cc.ReadHeader(&h); err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Println("rpc server: read header error:", err)
		}
		log.Println("rpc server : readRequestHeader failed, err : ", err)
		return nil, err
	}
	return &h, nil
}

func (server *Server) readRequest(cc codec.Codec) (*request, error) {
	var req = &request{}

	h, err := server.readRequestHeader(cc) //获取请求头
	if err != nil {
		return nil, err
	}

	req.h = h
	req.svc, req.mtype, err = server.findService(h.ServiceMethod) //获取服务指针和方法指针
	if err != nil {
		return req, err
	}

	//初始化argv, replyv
	req.argv = req.mtype.newArgv()
	req.replyv = req.mtype.newReplyv()

	// argv和argvi指向一个interface，确保argvi是指针类型，因为readbody需要传入指针类型，来进行赋值
	argvi := req.argv.Interface()
	if req.argv.Type().Kind() != reflect.Ptr {
		argvi = req.argv.Addr().Interface()
	}

	if err = cc.ReadBody(argvi); err != nil {
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

func (server *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup, timeout time.Duration) {
	defer wg.Done()

	//用于超时控制
	called := make(chan struct{})
	sent := make(chan struct{})

	go func() {
		err := req.svc.call(req.mtype, req.argv, req.replyv) //调用call方法，结果写入到replyv中
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

	//timeout为0表示不做限制
	if timeout == 0 {
		<-called
		<-sent
		return
	}

	select {
	case <-time.After(timeout):
		req.h.Error = fmt.Sprintf("rpc server: request handle timeout: expect within %s", timeout)
		server.sendResponse(cc, req.h, invalidRequest, sending)
	case <-called:
		<-sent
	}
}
