package xclient

import (
	"log"
	"net/http"
	"strings"
	"time"
)

// GoRegistryDiscovery
//
//	@Description: 基于Go语言的注册中心服务发现
type GoRegistryDiscovery struct {
	*MultiServersDiscovery
	registry   string
	timeout    time.Duration
	lastUpdate time.Time
}

const defaultUpdateTimeout = time.Second * 10

// NewGoRegistryDiscovery
//
//	@Description: 创建一个基于Go语言的注册中心服务发现实例
//	@param registerAddr 注册中心地址
//	@param timeout 更新超时时间
//	@return *GoRegistryDiscovery
func NewGoRegistryDiscovery(registerAddr string, timeout time.Duration) *GoRegistryDiscovery {
	if timeout == 0 {
		timeout = defaultUpdateTimeout
	}
	d := &GoRegistryDiscovery{
		MultiServersDiscovery: NewMultiServerDiscovery(make([]string, 0)),
		registry:              registerAddr,
		timeout:               timeout,
	}
	return d
}

// Update
//
//	@Description: 更新服务实例列表
//	@receiver d  GoRegistryDiscovery实例
//	@param servers 服务实例列表
//	@return error 更新过程中可能返回的错误
func (d *GoRegistryDiscovery) Update(servers []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.servers = servers
	d.lastUpdate = time.Now()
	return nil
}

// Refresh
//
//	@Description: 刷新服务实例列表
//	@receiver d GoRegistryDiscovery实例
//	@return error 刷新过程中可能返回的错误
func (d *GoRegistryDiscovery) Refresh() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.lastUpdate.Add(d.timeout).After(time.Now()) {
		return nil
	}
	log.Println("rpc: refresh registry servers from registry", d.registry)
	resp, err := http.Get(d.registry)
	//fmt.Println(d.registry)
	if err != nil {
		log.Println("rpc: refresh registry servers from registry failed", err)
		return err
	}
	servers := strings.Split(resp.Header.Get("X-GoRpc-Servers"), ",")

	d.servers = make([]string, 0, len(servers))
	for _, server := range servers {
		if strings.TrimSpace(server) != "" {
			d.servers = append(d.servers, strings.TrimSpace(server))
		}
	}
	d.lastUpdate = time.Now()
	return nil
}

// Get
//
//	@Description: 获取服务实例地址
//	@receiver d
//	@param mode 负载均衡策略
//	@return string 服务实例地址
//	@return error 获取服务实例地址时可能返回的错误
func (d *GoRegistryDiscovery) Get(mode SelectMode) (string, error) {
	if err := d.Refresh(); err != nil {
		return "", err
	}
	return d.MultiServersDiscovery.Get(mode)
}

// GetAll
//
//	@Description: 获取所有服务实例地址
//	@receiver d  GoRegistryDiscovery实例
//	@return []string 所有服务实例地址
//	@return error 获取所有服务实例地址时可能返回的错误
func (d *GoRegistryDiscovery) GetAll() ([]string, error) {
	if err := d.Refresh(); err != nil {
		return nil, err
	}
	return d.MultiServersDiscovery.GetAll()
}
