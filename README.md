# APK HTML Go

这是一个 Go 版 APK 应用目录站，包含公开页面、中文后台 API/UI、语言数据刷新 Worker、MySQL 持久化和本地图片资源。首期使用印尼语样例数据，页面展示 APK 资料和图片信息；工程不采集、存储或提供 APK 文件本身。

公开页面、后台和 Worker 使用同一套 Go 代码。公开请求读取启动或刷新后发布到内存的只读快照，后台写入 MySQL 后通过 revision 刷新快照；不依赖 Redis，也不需要 PV/UV 统计服务。

可选推荐内容通过运维配置的固定 `RECOMMEND_FEED_URL` 接入；公开页面带 `recommend=1` 时，在“外部链接”后显示经过安全过滤的“推荐网站”区块。

## 本地启动

在工程根目录生成本地开发环境文件，然后启动 Docker Compose：

```sh
python3 scripts/prepare-env.py --preview
docker compose -f deployments/docker/docker-compose.yaml up -d --build
docker compose -f deployments/docker/docker-compose.yaml ps
```

访问：

- 公开页面：`http://127.0.0.1:18082/`
- 后台：`http://127.0.0.1:18082/admin/`
- 健康检查：`http://127.0.0.1:18082/healthz`
- `robots.txt`、`sitemap.xml`：明确返回 404
- 普通 404：按 PHP 兼容规则转首页并返回 HTTP 200

管理员凭据只从本机的 `deployments/docker/.env` 读取，不写入源码、文档或日志。外部转发工具及其配置在工程目录之外单独维护，工程不包含服务端地址、认证信息或转发配置。

## 验证

```sh
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
go build ./cmd/site ./cmd/worker
go mod verify
go mod tidy -diff
```

详细架构和运维入口见 [docs/OPERATIONS.md](docs/OPERATIONS.md)，最终验收项见 [docs/ACCEPTANCE.md](docs/ACCEPTANCE.md)。

## 目录说明

- `cmd/site`：Web 服务启动、迁移、初始种子和后台路由。
- `cmd/worker`：语言数据刷新任务和任务健康检查。
- `internal/site`：公开页面、SEO 字段、链接规则、后台 API、快照和 Worker 逻辑。
- `assets`：本地 CSS、图标和截图资源。
- `data/seed`：中性样例应用和链接数据。
- `data/lan/id`：印尼语词库和句子库。
- `deployments/docker`：Go 应用、Nginx 入口和 Compose 部署文件；外部转发配置不放在这里。

