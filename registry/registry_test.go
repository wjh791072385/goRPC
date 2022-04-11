package registry_test

import (
	"net"
	"net/http"
	"testing"

	"github.com/wjh791072385/gorpc/registry"
)

// 通过http启动注册中心
func TestStartRegistry(t *testing.T) {
	l, _ := net.Listen("tcp", ":9999")
	registry.HandleHTTP() //注册路由，默认路径是/gorpc/registry
	_ = http.Serve(l, nil)
}
