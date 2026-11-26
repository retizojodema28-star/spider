# 应用配置

运行时配置通过环境变量提供。开发环境建议使用 `scripts/prepare-env.py --preview` 生成 `deployments/docker/.env`；该文件只保存在本机，不提交到仓库。

| 变量 | 用途 | 默认或要求 |
| --- | --- | --- |
| `DATABASE_DSN` | MySQL 连接串 | 必填 |
| `APP_ENV` | 运行环境 | `development`、`test` 或 `production` |
| `ADMIN_USERNAME` | 初始管理员账号 | 必填 |
| `ADMIN_PASSWORD` | 初始管理员密码 | 本机生成，不写入源码 |
| `ADMIN_PASSWORD_HASH` | 初始 bcrypt 密码 | 与明文密码二选一 |
| `ADMIN_ORIGIN` | 后台来源校验地址 | 生产环境必填 HTTPS |
| `PORT` | Web 容器内部端口 | `8080` |
| `GIN_MODE` | Gin 模式 | `release` |
| `SEED_FILE` | 应用样例数据 | `data/seed/apps.json` |
| `LANGUAGE_DATA_DIR` | 语言数据目录 | `data/lan` |
| `LANGUAGES` | 加载的语言列表 | `id` |
| `ASSET_DIR` | 本地图片和静态资源根目录 | `assets` |

Docker Compose 将容器内部 `8080` 映射到本机 `18082`。Worker 使用独立的容器内部健康检查端口。Redis、外部队列和转发服务不是本工程的配置项。

管理员密码、数据库密码和任何外部转发凭据都不能写入本文件或其他已提交文件。

