# 流量分发功能 - 数据模型设计

## 核心表结构

### distribution_config (全局配置)
全局只有一条记录，控制整个流量分发功能。

```sql
CREATE TABLE distribution_config (
    id INTEGER PRIMARY KEY,
    enabled BOOLEAN DEFAULT false,
    
    -- 监听配置
    listen_port INTEGER NOT NULL DEFAULT 8443,
    domain TEXT NOT NULL,  -- ProxyHub 部署的域名
    
    -- 协议配置（所有 Path 共享）
    protocol TEXT NOT NULL DEFAULT 'vless',  -- vless, vmess
    network TEXT NOT NULL DEFAULT 'grpc',    -- grpc, ws
    uuid TEXT NOT NULL,  -- 用户的认证 UUID
    
    -- TLS 配置
    tls BOOLEAN DEFAULT true,
    cert_path TEXT,  -- TLS 证书路径
    key_path TEXT,   -- TLS 密钥路径
    
    -- 默认值示例
    -- protocol: vless, network: grpc, TLS: true
    
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

### distribution_paths (分发路径)
每个 Path 对应一个节点池，支持负载均衡。

```sql
CREATE TABLE distribution_paths (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,  -- 显示名称，如 "香港高速节点池"
    path TEXT NOT NULL UNIQUE,  -- URL path，如 "/hk", "/us"
    
    -- 上游节点配置（JSON 数组存储 NodeKey 列表）
    upstream_node_keys TEXT NOT NULL,  -- JSON: ["server:port", "server:port:sni"]
    
    -- 负载均衡策略
    lb_strategy TEXT DEFAULT 'round_robin',  -- round_robin, random, least_conn
    
    -- 流量统计（累计值）
    total_upload INTEGER DEFAULT 0,    -- 字节
    total_download INTEGER DEFAULT 0,  -- 字节
    total_connections INTEGER DEFAULT 0,
    last_access TIMESTAMP,
    
    enabled BOOLEAN DEFAULT true,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_distribution_paths_enabled ON distribution_paths(enabled);
CREATE INDEX idx_distribution_paths_path ON distribution_paths(path);
```

### distribution_stats (流量统计时间序列)
按小时/天聚合的流量统计，用于图表展示。

```sql
CREATE TABLE distribution_stats (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    path_id INTEGER NOT NULL,
    timestamp TIMESTAMP NOT NULL,  -- 统计时间点
    
    upload INTEGER DEFAULT 0,      -- 该时间段上传字节
    download INTEGER DEFAULT 0,    -- 该时间段下载字节
    connections INTEGER DEFAULT 0, -- 该时间段连接数
    
    FOREIGN KEY (path_id) REFERENCES distribution_paths(id) ON DELETE CASCADE
);

CREATE INDEX idx_distribution_stats_path_time ON distribution_stats(path_id, timestamp);
```

## Go 数据结构

```go
// DistributionConfig 全局分发配置
type DistributionConfig struct {
    ID      int64
    Enabled bool
    
    // 监听配置
    ListenPort int
    Domain     string
    
    // 协议配置（全局共享）
    Protocol string // vless, vmess
    Network  string // grpc, ws
    UUID     string // 认证 UUID
    
    // TLS 配置
    TLS      bool
    CertPath string
    KeyPath  string
    
    CreatedAt time.Time
    UpdatedAt time.Time
}

// DistributionPath 分发路径
type DistributionPath struct {
    ID   int64
    Name string // 显示名称
    Path string // URL path
    
    // 上游节点
    UpstreamNodeKeys []string // NodeKey 列表
    
    // 负载均衡
    LBStrategy string // round_robin, random, least_conn
    
    // 统计
    TotalUpload      int64
    TotalDownload    int64
    TotalConnections int64
    LastAccess       *time.Time
    
    Enabled   bool
    CreatedAt time.Time
    UpdatedAt time.Time
}

// DistributionStat 流量统计记录
type DistributionStat struct {
    ID          int64
    PathID      int64
    Timestamp   time.Time
    Upload      int64
    Download    int64
    Connections int64
}
```

## 协议支持

只支持能通过 Path/ServiceName 区分的协议组合：

### 支持的协议组合
- ✅ VLESS + gRPC (通过 serviceName)
- ✅ VLESS + WebSocket (通过 ws path)
- ✅ VMess + gRPC (通过 serviceName)
- ✅ VMess + WebSocket (通过 ws path)
- ✅ Trojan + WebSocket (通过 ws path)

### 不支持的协议组合
- ❌ VLESS/VMess + TCP (无 HTTP 层，无法识别 path)
- ❌ Shadowsocks (基于 SOCKS5)
- ❌ Hysteria (基于 QUIC)
- ❌ Trojan + TCP (基于 TLS，无 HTTP)
