package xclient

import (
	"errors"
	"math"
	"math/rand"
	"sync"
	"time"
)

type SelectMode int

const (
	RandomSelect     SelectMode = iota //随机选择策略
	RoundRobinSelect                   //加权轮询
)

type Discovery interface {
	Refresh() error                      //从注册中心更新列表
	Update(servers []string) error       //手动更新列表
	Get(mode SelectMode) (string, error) //根据策略选择一个服务实例
	GetAll() ([]string, error)           //返回所有服务实例
}

// MultiServersDiscovery 实现一个不需要注册中心，服务列表由手工维护的服务发现的结构体,用于测试负载均衡
type MultiServersDiscovery struct {
	r       *rand.Rand // 随机数产生
	mu      sync.RWMutex
	servers []string
	index   int // 记录轮询位置
}

func NewMultiServersDiscovery(servers []string) *MultiServersDiscovery {
	d := &MultiServersDiscovery{
		r:       rand.New(rand.NewSource(time.Now().UnixNano())),
		servers: servers,
	}
	d.index = d.r.Intn(math.MaxInt32 - 1)
	return d
}

var _ Discovery = (*MultiServersDiscovery)(nil)

func (m *MultiServersDiscovery) Refresh() error {
	return nil
}

func (m *MultiServersDiscovery) Update(servers []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.servers = servers
	return nil
}

func (m *MultiServersDiscovery) Get(mode SelectMode) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	n := len(m.servers)
	if n == 0 {
		return "", errors.New("rpc discover: no available servers")
	}

	switch mode {
	case RandomSelect:
		return m.servers[m.r.Intn(n)], nil
	case RoundRobinSelect:
		//从index处开始轮询
		s := m.servers[m.index%n]
		m.index = (m.index + 1) % n
		return s, nil
	default:
		return "", errors.New("rpc discovery: not supported select mode")
	}
}

func (m *MultiServersDiscovery) GetAll() ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	//返回拷贝
	servers := make([]string, len(m.servers), len(m.servers))
	copy(servers, m.servers)
	return servers, nil
}
