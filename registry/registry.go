package registry

import (
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// GoRegistry
// @Description: 服务注册中心，默认心跳超时时间5min，添加服务注册项收到心跳时，更新注册时间，返回所有存活注册项，删除死去注册项
type GoRegistry struct {
	timeout time.Duration // 服务超时时间
	mu      sync.Mutex
	servers map[string]*ServerItem
}

// ServerItem
// @Description: 服务注册项
type ServerItem struct {
	Addr  string    // 服务地址
	start time.Time // 服务注册时间
}

const (
	defaultPath    = "/_gorpc_/registry"
	defaultTimeOut = 5 * time.Minute
)

// New
//
//	@Description: 创建服务注册中心
//	@param timeOut 服务超时时间
//	@return *GoRegistry
func New(timeout time.Duration) *GoRegistry {
	return &GoRegistry{
		timeout: timeout,
		servers: make(map[string]*ServerItem),
	}
}

// DefaultGoRegistry 默认服务注册中心，默认心跳超时时间5min
var DefaultGoRegistry = New(defaultTimeOut)

// putServer
//
//	@Description: 添加服务注册项，收到心跳时，更新注册时间
//	@receiver r 服务注册中心
//	@param addr 服务地址
func (r *GoRegistry) putServer(addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.servers[addr]

	if s == nil {
		r.servers[addr] = &ServerItem{
			Addr:  addr,
			start: time.Now(),
		}
	} else {
		s.start = time.Now()
	}
}

// aliveServers
//
//	@Description: 返回所有存活注册项，删除死去注册项
//	@receiver r 服务注册中心
//	@return []string 所有存活注册项
func (r *GoRegistry) aliveServers() []string {
	now := time.Now()
	var alive []string
	for addr, s := range r.servers {
		if r.timeout == 0 || s.start.Add(r.timeout).After(now) {
			alive = append(alive, addr)
		} else {
			delete(r.servers, addr)
		}
	}
	sort.Strings(alive)
	return alive
}

// ServeHTTP
//
//	@Description: 处理HTTP请求，根据请求方法返回所有存活注册项或添加服务注册项
//	@receiver r 服务注册中心
//	@param w HTTP响应写入器
//	@param req HTTP请求
func (r *GoRegistry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		w.Header().Set("X-GoRpc-Servers", strings.Join(r.aliveServers(), ","))
	case http.MethodPost:
		addr := req.Header.Get("X-GoRpc-Server")
		if addr == "" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		r.putServer(addr)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// HandleHTTP
//
//	@Description: 处理HTTP请求，根据请求方法返回所有存活注册项或添加服务注册项
//	@receiver r
//	@param registerPath 注册路径
func (r *GoRegistry) HandleHTTP(registerPath string) {
	http.Handle(registerPath, r)
	log.Println("rpc registry path:", registerPath)
}

func HandleHTTP() {
	DefaultGoRegistry.HandleHTTP(defaultPath)
}

func Heartbeat(registry, addr string, duration time.Duration) {
	if duration == 0 {
		duration = defaultTimeOut - time.Duration(1)*time.Minute
	}
	var err error
	err = sendHeartbeat(registry, addr)
	go func() {
		t := time.NewTicker(duration)
		for err == nil {
			<-t.C
			err = sendHeartbeat(registry, addr)
		}
	}()
}

// sendHeartbeat
//
//	@Description: 发送心跳到注册中心，证明自己存活
//	@param registry 注册中心地址
//	@param addr 服务地址
//	@return error 发送心跳时可能返回的错误
func sendHeartbeat(registry, addr string) error {
	log.Println(addr, "send heartbeat to registry", registry)
	httpClient := &http.Client{}
	req, _ := http.NewRequest("POST", registry, nil)
	req.Header.Set("X-GoRpc-Server", addr)
	if _, err := httpClient.Do(req); err != nil {
		log.Println("rpc server: heartbeat error", err)
		return err
	}
	return nil
}
