# 运行与架构说明

## 交付范围

一期包含公开页面、中文管理后台、后台 API、语言数据刷新 Worker、本地图片和 MySQL 持久化。首期验收语言为印尼语。

页面中的应用资料、文字和图片来自本地数据或本地资源索引。APK 文件本身不采集、不上传、不代理。外部链接是普通跳转地址，由后台管理并在页面的“相关链接”区域展示。

不启用 Redis、PV/UV/下载统计、组合文章池或外站资料图片采集器；外站采集器属于后续阶段。

## 架构

```text
浏览器 / 搜索爬虫
  ↓
Nginx：Bot 过滤、每 IP 限速、JSON 访问日志
  ↓
Gin 路由与页面控制器
  ↓
页面规则、SEO、链接和模板
  ↓ 读取只读快照
内存 Runtime Snapshot
  ↑ 启动加载 / revision 刷新
MySQL ← 后台 API / Worker
```

Web 启动时迁移数据库、导入必要的中性样例并加载运行时快照。后台修改配置、应用或链接后写入 MySQL，再发布新的快照；公开页面不会在每次请求中查询 MySQL。Worker 独立处理语言数据刷新，任务状态、重试和事件写入 MySQL。

## 页面与资源

- `/`、`/apps`、分类路径：应用目录页。
- `/apps/<slug>`：应用详情页，包含图标、截图、版本资料、关键词和“相关链接”页签。
- `/__preview/`：本地样例查看入口，使用 noindex 预览数据。
- `/assets/...`：本地 CSS、图标和截图。
- `/admin/`：中文管理后台。
- `/robots.txt`、`/sitemap.xml`：明确返回 404；未知普通路径仍按兼容规则生成页面。
- 普通 404 通过 Nginx 内部转到 `/index.php?__apk_home_fallback=1`，最终返回首页内容和 HTTP 200；`robots.txt`、`sitemap.xml` 不参与该兜底。

详情页图片由本地资源索引提供。缺失图片使用本地样例回退；公开请求不会访问外站图片地址。链接生成遵循既定兼容规则，页面链接使用相对路径或配置的外部完整地址，不写死本机端口。

## Docker

```sh
python3 scripts/prepare-env.py --preview
docker compose -f deployments/docker/docker-compose.yaml up -d --build
docker compose -f deployments/docker/docker-compose.yaml ps
curl -fsS http://127.0.0.1:18082/healthz
```

公开入口由 Nginx 使用宿主机 `18082`，Go Web 仅在 Compose 内部监听 `8080`；Worker 健康检查使用容器内部端口 `8081`；MySQL 不发布宿主机端口。Nginx 对 Google/Bing 搜索爬虫放行，对其他通用爬虫和采集器返回 403，并对每个 IP 使用 `10r/s`、`burst=40` 限速。访问日志写入 `nginx_logs` 卷，记录请求域名、路径、Referer、User-Agent、状态码和耗时。

外部转发工具必须在工程目录之外启动。Go 工程只提供本地 `18082` 端口，由 Nginx 接收入站请求，不保存转发服务端地址、认证信息或外部端口配置。

## 可选推荐内容 Feed

如需在详情页“外部链接”后追加“推荐网站”区块，设置固定的 `RECOMMEND_FEED_URL`。该值只能是运维配置的绝对 HTTP/HTTPS 地址，不能由公开请求指定。访问页面时追加 `recommend=1` 才会启用，服务端会把当前内容域名和原始请求路径分别作为 `domain`、`path` 查询参数追加到 Feed 地址。

```text
/apps/example?lan=id&recommend=1
```

Feed 返回的 HTML 片段会经过标签和链接白名单过滤，并在内存中短暂缓存；超时、状态码异常、跨主机重定向或内容过大时，页面主体仍正常返回，仅跳过推荐区块。

## 后台操作

后台包括站点设置、语言/图片绑定、应用数据、首页与详情页链接、导入预览和语言刷新任务。保存后会刷新内存快照，无需重启服务。外部链接只保存目标 URL、锚文本、语言、权重和启用状态等业务字段；删除 `/link` 管理入口后，不再提供单独的链接聚合管理页。

## 检查命令

```sh
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
go build ./cmd/site ./cmd/worker
go mod verify
go mod tidy -diff
```

后台和 Worker 验收脚本只能指向独立的临时 Compose 实例，不能指向本地预览端口。

