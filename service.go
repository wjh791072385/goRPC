package goRPC

import (
	"go/ast"
	"log"
	"reflect"
	"sync/atomic"
)

//	定义方法类型结构体，方便后续封装

type methodType struct {
	method    reflect.Method //方法
	ArgType   reflect.Type   //入参
	ReplyType reflect.Type   //出参
	numCalls  uint64
}

func (m *methodType) NumCalls() uint64 {
	return atomic.LoadUint64(&m.numCalls)
}

func (m *methodType) newArgv() reflect.Value {
	var argv reflect.Value
	if m.ArgType.Kind() == reflect.Ptr {
		argv = reflect.New(m.ArgType.Elem())
	} else {
		argv = reflect.New(m.ArgType).Elem()
	}
	return argv
}

func (m *methodType) newReplyv() reflect.Value {
	// reply必为指针类型，因此直接创建
	replyv := reflect.New(m.ReplyType.Elem())
	switch m.ReplyType.Elem().Kind() {
	case reflect.Map:
		replyv.Elem().Set(reflect.MakeMap(m.ReplyType.Elem()))
	case reflect.Slice:
		replyv.Elem().Set(reflect.MakeSlice(m.ReplyType.Elem(), 0, 0))
	}
	return replyv

}

// 定义服务结构体，一个服务对应一个结构体，一个结构体对应多个方法
type service struct {
	name   string                 //服务名
	typ    reflect.Type           //结构体类型
	rcvr   reflect.Value          //结构体本身，保留 rcvr 是因为在调用call方法时需要 rcvr 作为第 0 个参数
	method map[string]*methodType //method 是 map 类型，存储映射的结构体的所有符合条件的方法
}

// rcvr表示需要映射为服务的结构体实例
func newService(rcvr interface{}) *service {
	s := new(service)
	s.rcvr = reflect.ValueOf(rcvr)
	s.name = reflect.Indirect(s.rcvr).Type().Name()
	s.typ = reflect.TypeOf(rcvr)

	// IsExported 报告名称是否为导出的 Go 符号（即，它是否以大写字母开头）
	if !ast.IsExported(s.name) {
		log.Fatalf("rpc server: %s is not a valid service name", s.name)
	}

	s.registerMethods()
	return s
}

func (s *service) registerMethods() {
	s.method = make(map[string]*methodType)

	for i := 0; i < s.typ.NumMethod(); i++ {
		method := s.typ.Method(i)
		mType := method.Type

		//两个导出或内置类型的入参（反射时为 3 个，第 0 个是自身，类似于 python 的 self，java 中的 this）
		//返回值有且只有 1 个，类型为 error
		if mType.NumIn() != 3 || mType.NumOut() != 1 {
			continue
		}

		if mType.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
			continue
		}

		argType, replyType := mType.In(1), mType.In(2)
		if !isExportedOrBuiltinType(argType) || !isExportedOrBuiltinType(replyType) {
			continue
		}

		//加入map
		s.method[method.Name] = &methodType{
			method:    method,
			ArgType:   argType,
			ReplyType: replyType,
		}

		log.Printf("rpc server: register %s.%s\n", s.name, method.Name)
	}
}

func isExportedOrBuiltinType(t reflect.Type) bool {
	return ast.IsExported(t.Name()) || t.PkgPath() == ""
}

func (s *service) call(m *methodType, argv, rplyv reflect.Value) error {
	atomic.AddUint64(&m.numCalls, 1)
	f := m.method.Func

	returnValues := f.Call([]reflect.Value{s.rcvr, argv, rplyv})

	if errInter := returnValues[0].Interface(); errInter != nil {
		return errInter.(error)
	}
	return nil
}
