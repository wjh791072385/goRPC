package registry

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
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
	defaultTimeout = time.Second * 5
)

func NewRegistry(timeout time.Duration) *Registry {
	return &Registry{
		timeout: timeout,
		servers: make(map[string]*ServerItem), //注意map必须初始化才能使用
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

// 注册中心使用http通信，信息放在body中
func (r *Registry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	//log.Printf("%s %s request\n", req.URL, req.Method)
	switch req.Method {
	case "GET":
		//get方法返回所有可用服务器
		js, err := json.Marshal(r.aliveServers())
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write(js)
	case "POST":
		//设置或更新服务
		body, err := ioutil.ReadAll(req.Body)
		if err != nil || len(body) == 0 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		s := make([]string, 0)
		json.Unmarshal(body, &s)
		r.putServer(s)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (r *Registry) HandleHTTP(registryPath string) {
	http.Handle(registryPath, r)
	log.Println("rpc registry path:", registryPath)
}

// HandleHTTP 对外暴露，使用默认路径，默认注册中心
func HandleHTTP() {
	DefaultRegistry.HandleHTTP(defaultPath)
}

// Heartbeat 心跳检测
func Heartbeat(registry, addr string, duration time.Duration) {
	if duration == 0 {
		//确保足够时间
		duration = defaultTimeout - time.Duration(1)*time.Second
	}
	var err error
	err = sendHeartbeat(registry, addr)
	go func() {
		//间隔duration，重复发送
		t := time.NewTicker(duration)
		for err == nil {
			<-t.C
			err = sendHeartbeat(registry, addr)
		}
	}()
}

func sendHeartbeat(registry, addr string) error {
	//log.Println(addr, "send heart beat to registry ", registry)

	httpClient := &http.Client{}

	//将addr封装成[]string类型发送过去。如果是发送string,那么注册中心接收端应该做出对应修改
	data, _ := json.Marshal([]string{addr})
	reader := bytes.NewReader(data)
	req, _ := http.NewRequest("POST", registry, reader)
	//req.Header.Set("X-gorpc-server", addr)
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")

	//发送json数据过去
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Println("rpc server: status: ", resp.StatusCode, "heart beat err:", err)
		return err
	}
	return nil
}
