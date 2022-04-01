package goRPC

import (
	"log"
	"reflect"
	"testing"
)

type Foo int

type Args struct {
	Num1 int
	Num2 int
}

func (f Foo) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

// 测试不可导出
func (f Foo) sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

func TestNewService(t *testing.T) {
	var foo Foo
	s := newService(&foo)
	log.Println(s)
	log.Println(len(s.method)) //应该只输出可导出的方法个数

	m := s.method["Sum"]
	log.Println(m)
}

func TestMethodType_Call(t *testing.T) {
	var foo Foo
	s := newService(&foo)
	mType := s.method["Sum"]

	argv := mType.newArgv()
	replyv := mType.newReplyv()
	argv.Set(reflect.ValueOf(Args{Num1: 1, Num2: 5}))
	err := s.call(mType, argv, replyv)
	if err != nil {
		log.Println("call failed")
		return
	}

	log.Println("res = ", *replyv.Interface().(*int))
	log.Println(mType.NumCalls())
}
