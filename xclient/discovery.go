package xclient

import (
	"errors"
	"math"
	"math/rand"
	"sync"
	"time"
)

// SelectMode 负载均衡策略
type SelectMode int

// 枚举类型的定义
const (
	RandomSelect     SelectMode = iota // 随机选择
	RoundRobinSelect                   // 轮询选择
)

// Discovery
//
//	@Description: 服务发现接口
type Discovery interface {
	Refresh() error // refresh from remote registry
	Update(servers []string) error
	Get(mode SelectMode) (string, error)
	GetAll() ([]string, error)
}

// MultiServersDiscovery
//
//	@Description: 多服务发现实现
type MultiServersDiscovery struct {
	r       *rand.Rand // 随机数生成器
	mu      sync.Mutex // 并发保护(保证客户端相关操作的并发安全)
	servers []string
	index   int // 轮询选择的索引
}

// NewMultiServerDiscovery
//
//	@Description: 创建一个新的多服务发现实例
//	@param servers 服务列表
//	@return *MultiServersDiscovery
func NewMultiServerDiscovery(servers []string) *MultiServersDiscovery {
	d := &MultiServersDiscovery{
		servers: servers,
		r:       rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	d.index = d.r.Intn(math.MaxInt32 - 1)
	return d
}

var _ Discovery = (*MultiServersDiscovery)(nil)

// Refresh
//
//	@Description: 从注册中心刷新服务列表
//	@receiver d
//	@return error
func (d *MultiServersDiscovery) Refresh() error {
	return nil
}

// Update
//
//	@Description: 更新服务实例列表
//	@receiver d
//	@param servers
//	@return error
func (d *MultiServersDiscovery) Update(servers []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.servers = servers
	return nil
}

// Get
//
//	@Description: 根据负载均衡策略选择一个服务实例
//	@receiver d 多服务发现实例
//	@param mode 负载均衡策略
//	@return string 服务实例地址
//	@return error
func (d *MultiServersDiscovery) Get(mode SelectMode) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := len(d.servers)
	if n == 0 {
		return "", errors.New("discovery: no server available")
	}
	switch mode {
	case RandomSelect:
		return d.servers[d.r.Intn(n)], nil
	case RoundRobinSelect:
		s := d.servers[d.index%n]
		d.index = (d.index + 1) % n
		return s, nil
	default:
		return "", errors.New("discovery: invalid select mode")
	}
}

// GetAll
//
//	@Description: 返回所有的服务实例
//	@receiver d 多服务发现实例
//	@return []string 服务实例列表
//	@return error
func (d *MultiServersDiscovery) GetAll() ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	servers := make([]string, len(d.servers))
	copy(servers, d.servers)
	return servers, nil
}
