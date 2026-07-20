# 流量分发功能 - 架构设计

## 整体架构

```
┌──────────────────────────────────────────────────────────────┐
│                     ProxyHub 服务                             │
├──────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌─────────────────┐        ┌────────────────────┐          │
│  │  HTTP API       │        │  订阅生成服务       │          │
│  │  (现有功能)     │◄──────►│  (扩展)            │          │
│  └─────────────────┘        └────────────────────┘          │
│                                      │                        │
│                                      ▼                        │
│  ┌──────────────────────────────────────────────┐           │
│  │      Distribution Manager (新增)              │           │
│  ├──────────────────────────────────────────────┤           │
│  │  • 配置管理 (Config)                         │           │
│  │  • 路由规则生成 (Routing)                    │           │
│  │  • 流量统计收集 (Stats Collector)            │           │
│  │  • 负载均衡器 (Load Balancer)                │           │
│  └──────────────────────────────────────────────┘           │
│                        │                                      │
│                        ▼                                      │
│  ┌──────────────────────────────────────────────┐           │
│  │       Xray Core Integration (新增)            │           │
│  ├──────────────────────────────────────────────┤           │
│  │  • 进程管理 (Lifecycle)                      │           │
│  │  • 动态配置 (Dynamic Config)                 │           │
│  │  • Stats API 对接                            │           │
│  └──────────────────────────────────────────────┘           │
│                        │                                      │
└────────────────────────┼──────────────────────────────────────┘
                         │
                         ▼
              ┌──────────────────┐
              │   Xray 进程       │
              │   (监听 :8443)    │
              └──────────────────┘
                         │
        ┌────────────────┼────────────────┐
        ▼                ▼                ▼
   [上游节点1]      [上游节点2]      [上游节点3]
   (机场/自建)      (机场/自建)      (机场/自建)
```

## 模块划分

### 1. Distribution Manager (internal/distribution)

**职责**：
- 管理流量分发的配置和路由规则
- 收集流量统计
- 提供负载均衡逻辑

**子模块**：

#### 1.1 Config Manager (`config.go`)
```go
package distribution

type ConfigManager struct {
    st *store.Store
}

// GetConfig 获取全局分发配置
func (cm *ConfigManager) GetConfig() (*store.DistributionConfig, error)

// UpdateConfig 更新全局配置
func (cm *ConfigManager) UpdateConfig(cfg *store.DistributionConfig) error

// GetPaths 获取所有启用的分发路径
func (cm *ConfigManager) GetPaths() ([]*store.DistributionPath, error)

// CreatePath 创建新路径
func (cm *ConfigManager) CreatePath(path *store.DistributionPath) error

// UpdatePath 更新路径
func (cm *ConfigManager) UpdatePath(id int64, path *store.DistributionPath) error

// DeletePath 删除路径
func (cm *ConfigManager) DeletePath(id int64) error
```

#### 1.2 Routing Builder (`routing.go`)
```go
// RoutingBuilder 根据配置生成 Xray 路由规则
type RoutingBuilder struct {
    cfg   *store.DistributionConfig
    paths []*store.DistributionPath
    nodes NodeSource // 获取节点信息
}

// BuildXrayConfig 生成完整的 Xray 配置
func (rb *RoutingBuilder) BuildXrayConfig() (*XrayConfig, error)

// XrayConfig Xray 配置结构
type XrayConfig struct {
    Inbounds  []Inbound
    Outbounds []Outbound
    Routing   RoutingRules
    Stats     StatsConfig
}
```

#### 1.3 Stats Collector (`stats.go`)
```go
// StatsCollector 定期从 Xray 收集流量统计
type StatsCollector struct {
    xrayAPI XrayStatsAPI
    store   *store.Store
    logger  *slog.Logger
}

// Start 启动统计收集（后台协程）
func (sc *StatsCollector) Start(ctx context.Context)

// CollectOnce 执行一次统计收集
func (sc *StatsCollector) CollectOnce() error
```

#### 1.4 Load Balancer (`loadbalancer.go`)
```go
// LoadBalancer 负载均衡器接口
type LoadBalancer interface {
    // SelectNode 从节点列表中选择一个
    SelectNode(nodes []string) string
}

// RoundRobinLB 轮询负载均衡
type RoundRobinLB struct { ... }

// RandomLB 随机负载均衡
type RandomLB struct { ... }

// LeastConnLB 最少连接负载均衡
type LeastConnLB struct { ... }
```

### 2. Xray Integration (internal/xray)

**职责**：
- 管理 Xray 进程的生命周期
- 动态更新 Xray 配置
- 对接 Xray Stats API

**子模块**：

#### 2.1 Process Manager (`process.go`)
```go
package xray

type ProcessManager struct {
    configPath string
    cmd        *exec.Cmd
    logger     *slog.Logger
}

// Start 启动 Xray 进程
func (pm *ProcessManager) Start() error

// Stop 停止 Xray 进程
func (pm *ProcessManager) Stop() error

// Restart 重启 Xray 进程
func (pm *ProcessManager) Restart() error

// IsRunning 检查进程是否运行
func (pm *ProcessManager) IsRunning() bool
```

#### 2.2 Config Writer (`config.go`)
```go
// ConfigWriter 将配置写入 Xray 配置文件
type ConfigWriter struct {
    configPath string
}

// WriteConfig 写入配置并验证
func (cw *ConfigWriter) WriteConfig(cfg *XrayConfig) error

// ValidateConfig 验证配置合法性（调用 xray -test）
func (cw *ConfigWriter) ValidateConfig(cfg *XrayConfig) error
```

#### 2.3 Stats API Client (`stats.go`)
```go
// StatsClient Xray Stats API 客户端
type StatsClient struct {
    apiAddr string // Xray API 监听地址
}

// GetPathStats 获取指定路径的流量统计
func (sc *StatsClient) GetPathStats(path string) (*PathStats, error)

// GetAllStats 获取所有路径的统计
func (sc *StatsClient) GetAllStats() (map[string]*PathStats, error)

type PathStats struct {
    Upload      int64
    Download    int64
    Connections int64
}
```

### 3. Store 扩展 (internal/store)

在现有的 `store.go` 中添加新表的 CRUD 方法：

```go
// distribution.go

// GetDistributionConfig 获取全局配置
func (s *Store) GetDistributionConfig() (*DistributionConfig, error)

// SaveDistributionConfig 保存全局配置
func (s *Store) SaveDistributionConfig(cfg *DistributionConfig) error

// ListDistributionPaths 列出所有路径
func (s *Store) ListDistributionPaths() ([]*DistributionPath, error)

// GetDistributionPath 获取指定路径
func (s *Store) GetDistributionPath(id int64) (*DistributionPath, error)

// CreateDistributionPath 创建路径
func (s *Store) CreateDistributionPath(path *DistributionPath) error

// UpdateDistributionPath 更新路径
func (s *Store) UpdateDistributionPath(id int64, path *DistributionPath) error

// DeleteDistributionPath 删除路径
func (s *Store) DeleteDistributionPath(id int64) error

// RecordDistributionStat 记录统计数据
func (s *Store) RecordDistributionStat(stat *DistributionStat) error

// GetDistributionStats 获取指定路径的统计（按时间范围）
func (s *Store) GetDistributionStats(pathID int64, from, to time.Time) ([]*DistributionStat, error)

// UpdatePathTotalStats 更新路径的累计统计
func (s *Store) UpdatePathTotalStats(pathID int64, upload, download, connections int64) error
```

### 4. Server 扩展 (internal/server)

在 `server.go` 中添加新的 HTTP 路由：

```go
// distribution_handlers.go

// 全局配置
mux.HandleFunc("GET /api/distribution/config", s.requireAuth(s.handleGetDistributionConfig))
mux.HandleFunc("PUT /api/distribution/config", s.requireAuth(s.handleUpdateDistributionConfig))

// 路径管理
mux.HandleFunc("GET /api/distribution/paths", s.requireAuth(s.handleListPaths))
mux.HandleFunc("POST /api/distribution/paths", s.requireAuth(s.handleCreatePath))
mux.HandleFunc("PUT /api/distribution/paths/{id}", s.requireAuth(s.handleUpdatePath))
mux.HandleFunc("DELETE /api/distribution/paths/{id}", s.requireAuth(s.handleDeletePath))
mux.HandleFunc("POST /api/distribution/paths/{id}/toggle", s.requireAuth(s.handleTogglePath))

// 流量统计
mux.HandleFunc("GET /api/distribution/stats", s.requireAuth(s.handleDistributionStats))
mux.HandleFunc("GET /api/distribution/paths/{id}/stats", s.requireAuth(s.handlePathStats))

// Xray 进程管理
mux.HandleFunc("POST /api/distribution/xray/restart", s.requireAuth(s.handleRestartXray))
mux.HandleFunc("GET /api/distribution/xray/status", s.requireAuth(s.handleXrayStatus))
```

### 5. 订阅生成扩展 (internal/generator)

扩展现有的订阅生成逻辑，支持生成流量分发节点：

```go
// distribution.go

// GenerateDistributionNodes 生成流量分发节点
func GenerateDistributionNodes(cfg *store.DistributionConfig, paths []*store.DistributionPath) []*subscription.Node {
    nodes := make([]*subscription.Node, 0, len(paths))
    
    for _, path := range paths {
        if !path.Enabled {
            continue
        }
        
        node := &subscription.Node{
            Name:   path.Name,
            Type:   cfg.Protocol,
            Server: cfg.Domain,
            Port:   cfg.ListenPort,
            UUID:   cfg.UUID,
            Network: cfg.Network,
            TLS:    cfg.TLS,
            SNI:    cfg.Domain,
            Source: "distribution", // 特殊来源标记
        }
        
        // 根据网络类型设置 Path
        if cfg.Network == "grpc" {
            node.GrpcServiceName = path.Path
        } else if cfg.Network == "ws" {
            node.WSPath = path.Path
        }
        
        nodes = append(nodes, node)
    }
    
    return nodes
}
```

在 `server.go` 的订阅生成逻辑中集成：

```go
func (s *Server) renderSubscription(nodes []*subscription.Node, format string) ([]byte, string, error) {
    // 1. 现有机场节点
    filteredNodes := s.filteredNodes(nodes)
    
    // 2. 如果启用了流量分发，追加分发节点
    if s.distributionEnabled() {
        distNodes, err := s.generateDistributionNodes()
        if err != nil {
            s.logger.Warn("generate distribution nodes failed", "error", err)
        } else {
            filteredNodes = append(filteredNodes, distNodes...)
        }
    }
    
    // 3. 统一生成订阅
    // ... 现有逻辑
}
```

## 配置流程

### Xray 配置示例

```json
{
  "log": {
    "loglevel": "warning"
  },
  "stats": {},
  "api": {
    "tag": "api",
    "services": ["StatsService"]
  },
  "policy": {
    "system": {
      "statsInboundUplink": true,
      "statsInboundDownlink": true
    }
  },
  "inbounds": [
    {
      "tag": "distribution",
      "port": 8443,
      "protocol": "vless",
      "settings": {
        "clients": [
          {
            "id": "uuid-from-config"
          }
        ],
        "decryption": "none"
      },
      "streamSettings": {
        "network": "grpc",
        "security": "tls",
        "tlsSettings": {
          "certificates": [
            {
              "certificateFile": "/path/to/cert.pem",
              "keyFile": "/path/to/key.pem"
            }
          ]
        },
        "grpcSettings": {
          "serviceName": ""
        }
      }
    },
    {
      "tag": "api",
      "port": 10085,
      "protocol": "dokodemo-door",
      "settings": {
        "address": "127.0.0.1"
      }
    }
  ],
  "outbounds": [
    {
      "tag": "hk-pool",
      "protocol": "vless",
      "settings": {
        "vnext": [
          {
            "address": "hk-node1.example.com",
            "port": 443,
            "users": [{"id": "uuid", "encryption": "none"}]
          }
        ]
      }
    },
    {
      "tag": "us-pool",
      "protocol": "vmess",
      "settings": {
        "vnext": [
          {
            "address": "us-node1.example.com",
            "port": 443,
            "users": [{"id": "uuid", "alterId": 0}]
          }
        ]
      }
    }
  ],
  "routing": {
    "rules": [
      {
        "type": "field",
        "inboundTag": ["distribution"],
        "grpcMethod": "/hk",
        "outboundTag": "hk-pool"
      },
      {
        "type": "field",
        "inboundTag": ["distribution"],
        "grpcMethod": "/us",
        "outboundTag": "us-pool"
      },
      {
        "type": "field",
        "inboundTag": ["api"],
        "outboundTag": "api"
      }
    ]
  }
}
```

## 启动流程

```
1. ProxyHub 启动
   ↓
2. 检查 distribution_config 是否启用
   ↓
3. 如果启用：
   a. 读取配置和路径
   b. 生成 Xray 配置文件
   c. 启动 Xray 进程
   d. 启动 Stats Collector
   ↓
4. HTTP 服务启动
```

## 配置更新流程

```
用户在后台更新配置
   ↓
HTTP API 接收请求
   ↓
保存到数据库
   ↓
重新生成 Xray 配置
   ↓
重启 Xray 进程
   ↓
返回成功
```

## 流量统计流程

```
Stats Collector (每 1 分钟)
   ↓
调用 Xray Stats API
   ↓
获取每个 Path 的流量数据
   ↓
保存到 distribution_stats 表
   ↓
更新 distribution_paths 的累计值
```
