# Engine Helm Chart

Helm Chart，用于在 Kubernetes 集群中部署 Engine 游戏后端。支持单机（standalone）、集群（cluster）和多区域（multi-region）三种模式，并内置 HPA、健康检查和 Prometheus ServiceMonitor。

## 先决条件

- Kubernetes 1.23+
- Helm 3.10+
- 已安装 `prometheus-operator`（启用 `monitoring.serviceMonitor.enabled=true` 时必需）
- 已安装 `metrics-server`（启用 HPA 时必需）

## 目录结构

```
deploy/helm/engine/
├── Chart.yaml              Chart 元数据
├── values.yaml             默认值
├── README.md               本文档
└── templates/
    ├── deployment.yaml     主 Deployment
    ├── service.yaml        ClusterIP Service（gate / ws / dashboard / gossip）
    ├── hpa.yaml            HorizontalPodAutoscaler（根据 gate 连接数伸缩）
    ├── configmap.yaml      引擎配置 ConfigMap
    └── servicemonitor.yaml Prometheus ServiceMonitor
```

## 安装

```bash
# 从本仓库根目录
helm install my-engine ./deploy/helm/engine

# 指定自定义 values
helm install my-engine ./deploy/helm/engine -f my-values.yaml

# 或通过 --set 临时覆盖
helm install my-engine ./deploy/helm/engine \
  --set mode=cluster \
  --set replicaCount=3 \
  --set autoscaling.enabled=true
```

## 升级

```bash
helm upgrade my-engine ./deploy/helm/engine -f my-values.yaml

# 强制重启（比如镜像未变更但需要重载配置）
helm upgrade my-engine ./deploy/helm/engine --recreate-pods
```

## 卸载

```bash
helm uninstall my-engine

# 若使用了 PVC 需要手动清理
kubectl delete pvc -l app.kubernetes.io/instance=my-engine
```

## 常见部署场景（Preset）

### 1. Standalone 单节点（开发/调试）

```yaml
# values-standalone.yaml
mode: standalone
replicaCount: 1
nodeRole: all
autoscaling:
  enabled: false
monitoring:
  enabled: false
```

### 2. Cluster 生产集群（3+ 节点 + HPA）

```yaml
# values-cluster.yaml
mode: cluster
replicaCount: 3
nodeRole: all
autoscaling:
  enabled: true
  minReplicas: 3
  maxReplicas: 20
cluster:
  name: engine-prod
  seedNodes:
    - engine-0.engine-headless:9100
    - engine-1.engine-headless:9100
  gossipInterval: 200ms
  heartbeatInterval: 500ms
monitoring:
  enabled: true
  serviceMonitor:
    enabled: true
```

### 3. Multi-region 跨区域联邦

```yaml
# values-multi-region.yaml
mode: multi-region
replicaCount: 3
nodeRole: game
cluster:
  name: engine-asia
  seedNodes:
    - engine-asia-seed:9100
    - engine-us-seed:9100    # 跨区 seed
    - engine-eu-seed:9100
resources:
  requests:
    cpu: "1"
    memory: 2Gi
  limits:
    cpu: "4"
    memory: 8Gi
```

## values.yaml 字段说明

| 键 | 类型 | 默认值 | 说明 |
|----|------|--------|------|
| `mode` | string | `standalone` | 部署模式：`standalone` / `cluster` / `multi-region` |
| `image.repository` | string | `engine/engine` | 镜像仓库 |
| `image.tag` | string | `1.8.0` | 镜像标签（生产建议固定到具体版本） |
| `image.pullPolicy` | string | `IfNotPresent` | 拉取策略 |
| `replicaCount` | int | `1` | 副本数。`cluster` / `multi-region` 模式下推荐 ≥3 |
| `nodeRole` | string | `all` | 节点角色：`gate` / `game` / `all` |
| `service.type` | string | `ClusterIP` | Service 类型（对外暴露时改为 `LoadBalancer` 或配合 Ingress） |
| `service.gatePort` | int | `7100` | 客户端 TCP 网关端口 |
| `service.wsPort` | int | `7200` | WebSocket 端口 |
| `service.dashboardPort` | int | `8080` | Dashboard / 管理 API 端口 |
| `service.gossipPort` | int | `9100` | 集群 Gossip 端口 |
| `resources.requests/limits` | object | 见 values.yaml | Pod 资源请求/上限 |
| `healthcheck.liveness.path` | string | `/healthz` | 存活检查路径 |
| `healthcheck.readiness.path` | string | `/readyz` | 就绪检查路径 |
| `autoscaling.enabled` | bool | `false` | 是否启用 HPA |
| `autoscaling.minReplicas` | int | `2` | HPA 最小副本数 |
| `autoscaling.maxReplicas` | int | `20` | HPA 最大副本数 |
| `autoscaling.metrics` | list | 见 values.yaml | HPA 指标数组（默认以 `engine_gate_connection_count` 为准） |
| `cluster.name` | string | `engine-cluster` | 集群名称 |
| `cluster.seedNodes` | list | `[]` | Gossip 种子节点列表 |
| `cluster.gossipInterval` | duration | `500ms` | Gossip 广播间隔 |
| `cluster.heartbeatInterval` | duration | `2s` | 心跳间隔 |
| `cluster.kinds` | list | `[game, gate]` | 节点承载的 Actor Kind |
| `config.enabled` | bool | `false` | 是否挂载 ConfigMap |
| `config.mountPath` | string | `/etc/engine/config` | 配置挂载路径 |
| `config.data` | map | `{}` | 内联配置（创建 ConfigMap） |
| `monitoring.enabled` | bool | `true` | 暴露 Prometheus 端点 |
| `monitoring.serviceMonitor.enabled` | bool | `false` | 生成 ServiceMonitor（需要 prometheus-operator） |
| `monitoring.serviceMonitor.interval` | duration | `15s` | 抓取间隔 |
| `nodeSelector` | map | `{}` | Pod 调度选择器 |
| `tolerations` | list | `[]` | 污点容忍 |
| `affinity` | map | `{}` | 亲和性规则 |

## 调试

```bash
# 渲染模板查看最终 YAML（不部署）
helm template my-engine ./deploy/helm/engine -f values-cluster.yaml

# 查看 Pod 日志
kubectl logs -l app.kubernetes.io/instance=my-engine -f

# 端口转发到本地 Dashboard
kubectl port-forward svc/my-engine 8080:8080
# 浏览器打开 http://localhost:8080
```

## 版本与兼容性

| Chart 版本 | App 版本 | Kubernetes |
|-----------|---------|------------|
| 0.1.0 | 1.8.x - 1.11.x | 1.23+ |

## 常见问题

- **Pod 频繁重启**：检查 `resources.requests.memory` 是否过低，以及 `/readyz` 是否在启动后立即可达。默认 `initialDelaySeconds=5`，首次加载大配置可能需要调大。
- **集群无法收敛**：确认 `cluster.seedNodes` 使用 **Headless Service 的 StatefulSet Pod DNS**，而不是 ClusterIP（后者会把 gossip 流量负载均衡到随机 Pod）。
- **HPA 不触发**：确认 `metrics-server` 已安装且 `autoscaling.metrics[].type=Pods` 的自定义指标通过 `prometheus-adapter` 暴露到 Kubernetes Metrics API。

## 相关文档

- 架构：参见仓库根目录 `CLAUDE.md`
- Operator 模式：`deploy/operator/`（CRD + Controller）
- 纯 YAML 部署：`deploy/k8s/`
