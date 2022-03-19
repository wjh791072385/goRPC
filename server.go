package goRPC

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"reflect"
	"sync"

	"github.com/wjh791072385/gorpc/codec"
)

const DefaultMagicNumber = 0x3bef5c

//编码思路：RPC 客户端固定采用 JSON 编码 Option，后续的 header 和 body 的编码方式由 Option 中的 CodeType 指定，
//服务端首先使用 JSON 解码 Option，然后通过 Option 的 CodeType 解码剩余的内容

//| Option{MagicNumber: xxx, CodecType: xxx} | Header{ServiceMethod ...} | Body interface{} |
//| <------      固定 JSON 编码      ------>  | <-------   编码方式由 CodeType 决定   ------->|

//具体连接中的报文 | Option | Header1 | Body1 | Header2 | Body2 | ...

type Option struct {
	MagicNumber int
	CodeType    codec.Type
}

var DefaultOption = &Option{
	MagicNumber: DefaultMagicNumber,
	CodeType:    codec.GobType,
}

// Server 服务端实现
type Server struct{}

func NewServer() *Server {
	return &Server{}
}

var DefaultServer = NewServer()

func (s *Server) Accept(lis net.Listener) {
	for {
		conn, err := lis.Accept()
		if err != nil {
			log.Println("rpc server: accept error")
			return
		}

		//协程接收处理
		go s.ServeConn(conn)
	}
}

// Accept 封装一层，方便外部调用
func Accept(lis net.Listener) { DefaultServer.Accept(lis) }

// ServeConn 核心处理逻辑
func (s *Server) ServeConn(conn io.ReadWriteCloser) {
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

	f := codec.NewCodeFuncMap[opt.CodeType]
	if f == nil {
		log.Println("rpc server : invalid CodeType")
		return
	}

	//这里如果opt.CodeType = "application/gob"，那么f(conn)其实返回的是一个实现Codec接口的GobCodec实例
	s.serveCodec(f(conn))
}

// 空结构体
var invalidRequest = struct{}{}

//serveCodec 的过程非常简单。主要包含三个阶段
//读取请求 readRequest
//处理请求 handleRequest
//回复请求 sendResponse
func (s *Server) serveCodec(cc codec.Codec) {
	sending := new(sync.Mutex) //确保发送一个完整的响应
	wg := new(sync.WaitGroup)  //确保所有请求被处理

	for {
		req, err := s.readRequest(cc)
		if err != nil {
			if req == nil {
				break // it's not possible to recover, so close the connection
			}
			req.h.Error = err.Error()
			s.sendResponse(cc, req.h, invalidRequest, sending)
			continue
		}
		wg.Add(1)

		//使其不阻塞，for循环处理请求
		go s.handleRequest(cc, req, sending, wg)
	}

	wg.Wait()
}

// request stores all information of a call
type request struct {
	h            *codec.Header // header of request
	argv, replyv reflect.Value // argv and replyv of request
}

func (s *Server) readRequestHeader(cc codec.Codec) (*codec.Header, error) {
	var h codec.Header
	if err := cc.ReadHeader(&h); err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Println("rpc server: read header error:", err)
		}
		return nil, err
	}
	return &h, nil
}

func (s *Server) readRequest(cc codec.Codec) (*request, error) {
	var req = &request{}

	h, err := s.readRequestHeader(cc)
	if err != nil {
		return nil, err
	}

	req.h = h
	// 暂时假定为string类型
	req.argv = reflect.New(reflect.TypeOf(""))
	if err = cc.ReadBody(req.argv.Interface()); err != nil {
		log.Println("rpc server: read argv err:", err)
	}
	return req, nil
}

func (s *Server) sendResponse(cc codec.Codec, h *codec.Header, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()
	if err := cc.Write(h, body); err != nil {
		log.Println("rpc server: write response error:", err)
	}
}

func (s *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup) {
	defer wg.Done()
	log.Println(req.h, req.argv.Elem())
	req.replyv = reflect.ValueOf(fmt.Sprintf("gorpc resp %d", req.h.Seq))
	s.sendResponse(cc, req.h, req.replyv.Interface(), sending)
}
