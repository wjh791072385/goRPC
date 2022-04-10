package registry

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

// Registry 简单实现注册中心
// 增加一个服务，接收心跳包keep alive
// 返回所有alive的服务，删除dead服务
type Registry struct {
	timeout time.Duration
	mu      sync.Mutex
	servers map[string]*ServerItem
}

type ServerItem struct {
	Addr  string
	start time.Time
}

const (
	defaultPath    = "/gorpc/registry"
	defaultTimeout = time.Minute * 5
)

func NewRegistry(timeout time.Duration) *Registry {
	return &Registry{
		timeout: timeout,
	}
}

var DefaultRegistry = NewRegistry(defaultTimeout)

// 注册服务
func (r *Registry) putServer(addrs []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, addr := range addrs {
		s, ok := r.servers[addr]
		if ok {
			s.start = time.Now()
		} else {
			r.servers[addr] = &ServerItem{
				Addr:  addr,
				start: time.Now(),
			}
		}
	}
}

func (r *Registry) aliveServers() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var alive []string

	for addr, s := range r.servers {
		if r.timeout == 0 || s.start.Add(r.timeout).After(time.Now()) {
			//如果timeout = 0表示不限制超时，或者还没超时的话，则加入alive
			alive = append(alive, addr)
		} else {
			delete(r.servers, addr)
		}
	}
	return alive
}

// 注册中心使用http通信，信息放在 HTTP Header 中
func (r *Registry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case "GET":
		//get方法返回所有可用服务器，用逗号分割
		w.Header().Set("x-alive-rpc-servers", strings.Join(r.aliveServers(), ","))
	case "POST":
		//设置或更新服务

	}
}
