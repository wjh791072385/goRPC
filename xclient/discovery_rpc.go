package xclient

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"time"
)

// RegistryDiscovery 用于注册中心的服务发现
type RegistryDiscovery struct {
	*MultiServersDiscovery        //复用之前的
	registry               string //注册中心地址
	timeout                time.Duration
	lastUpdate             time.Time //表示注册中心最后的更新时间，如果距离当前时间超过了timeout，那么就要重新获取服务列表
}

const defaultUpdateTimeout = time.Second * 10

func NewGeeRegistryDiscovery(registerAddr string, timeout time.Duration) *RegistryDiscovery {
	if timeout == 0 {
		timeout = defaultUpdateTimeout
	}
	d := &RegistryDiscovery{
		MultiServersDiscovery: NewMultiServersDiscovery(make([]string, 0)),
		registry:              registerAddr,
		timeout:               timeout,
	}
	return d
}

// Update 手动更新
func (r *RegistryDiscovery) Update(servers []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	//更新的是RegistryDiscovery的匿名结构体中的servers字段
	r.servers = servers
	r.lastUpdate = time.Now()
	return nil
}

// Refresh 更新注册中心
func (r *RegistryDiscovery) Refresh() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lastUpdate.Add(r.timeout).After(time.Now()) {
		//表示还不用更新
		return nil
	}
	log.Println("rpc registry: refresh servers from registry", r.registry)

	//通过Http的get方法返回当前所有可用的服务
	resp, err := http.Get(r.registry)
	if err != nil {
		log.Println("rpc registry refresh err:", err)
		return err
	}
	data, err := ioutil.ReadAll(resp.Body)
	servers := make([]string, 0)
	json.Unmarshal(data, &servers)

	r.servers = servers
	r.lastUpdate = time.Now()
	return nil
}

// Get 根据负载均衡策略来做选择
func (r *RegistryDiscovery) Get(mode SelectMode) (string, error) {
	//先进行更新
	if err := r.Refresh(); err != nil {
		return "", err
	}
	return r.MultiServersDiscovery.Get(mode)
}

func (r *RegistryDiscovery) GetAll() ([]string, error) {
	if err := r.Refresh(); err != nil {
		return nil, err
	}
	return r.MultiServersDiscovery.GetAll()
}
