# NCode

NCode 是一个中文项目协作与交付平台。当前实现依据所提供的 SRS v4.0，需求文档标注为「待评审」。本仓库仍处于开发阶段，不是生产发布，也不构成 P0 验收结论。

## 当前范围

- 用户可注册并验证邮箱、登录和注销。开发环境会返回一次性验证令牌，便于在未配置邮件服务时测试；生产环境在邮件服务接入前会关闭注册和邀请创建。
- 用户可使用个人工作区或创建团队工作区，通过七天有效的一次性令牌邀请已验证账户、撤销待处理邀请，并切换所属工作区。项目、工作项、活动记录和幂等重放均按当前工作区隔离。
- 用户可创建和查看工作区内可见的项目，填写目标、标识和类型，并查看按工作项状态计算的进度。
- 用户可创建和编辑工作项，维护负责人、优先级、截止日期、验收清单和变更历史。工作项可在待处理、就绪、进行中、待评审、待验收和已验收之间流转。
- 验收前必须勾选当前验收清单的每一项。更新请求携带 `expectedVersion`；过期写入返回 HTTP 409。项目和工作项创建支持 `Idempotency-Key` 重放。
- 用户可在项目下创建私有 Git 仓库。系统会在配置的本地对象存储中初始化 `main` 分支和 README 首次提交，并提供工作区权限保护的目录及 UTF-8 文本浏览。文件预览上限为 1 MiB。
- 桌面与移动端宽度使用同一套界面。

项目进度按工作项状态计算：待处理或已取消为 0%，就绪为 20%，进行中为 50%，待评审为 75%，待业务验收为 90%，已验收为 100%，然后对项目内工作项取平均。没有工作项的项目显示 0%。

当前范围不包含密码找回、公开项目或仓库、HTTPS Git 克隆/拉取/推送、分支与拉取请求、隔离执行工作区或 CI、学习评估、成果分享、招聘，以及发布和部署。仓库浏览目前为只读；其他导航入口会明确显示尚未接入。API 默认只绑定回环地址并拒绝非回环绑定。请勿将此开发构建暴露到网络，也不要用它处理真实候选人、企业或生产数据。

## 技术栈

- Go 1.25+（已在 Go 1.27.2 上测试）、Gin、GORM、Fx
- PostgreSQL 17：账户、工作区、项目、仓库元数据、工作项、清单、活动记录和幂等记录
- 原生 Git：创建本地 bare 仓库并读取文件；默认将仓库对象存储在 `var/git`
- Vue 3、Vite、Element Plus 和 JavaScript
- Compose 中提供 Redis 7，供后续队列与协调功能使用；当前范围不依赖 Redis

## 后端模块

Fx 组合根位于 `internal/app`，组合配置、日志、数据库、API 和 HTTP 服务模块。账户/工作区逻辑位于 `internal/identity`，仓库逻辑位于 `internal/repository`；`cmd/api` 负责启动应用。数据库模块负责连接检查、结构迁移、演示数据灌入和连接池清理。HTTP 服务模块在 Fx 启动时绑定端口、报告绑定错误，并在数据库关闭前优雅关闭。

## 配置

应用从 `configs/config.yaml` 读取服务、PostgreSQL 和 Git 存储设置。使用 `CONFIG_FILE` 加载其他 YAML 文件。`DATABASE_PASSWORD` 可以覆盖 YAML 中的密码；`DATABASE_URL` 可以覆盖完整 PostgreSQL 地址。`API_ADDR`、`APP_ENV`、`SEED_DEMO` 和 `GIT_ROOT` 也可以覆盖 YAML 配置。生产环境始终关闭演示数据灌入。

已提交的 YAML 只包含本地 Compose 数据库的开发凭据。真实部署凭据应保存在版本控制之外。若要连接私有 PostgreSQL，可把 JDBC 地址和用户名写入被忽略的 `configs/config.local.yaml`，不要在文件中保存密码，通过环境密钥提供 `DATABASE_PASSWORD`，并设置 `CONFIG_FILE=configs/config.local.yaml`。API 启动时会执行 GORM AutoMigrate，因此只在确认目标数据库允许结构变更时启动服务。

## 本地启动

启动 PostgreSQL：

```sh
cd /workspace/ncode
docker compose up -d --wait postgres
```

构建 Vue 并将静态资源嵌入 Go 程序：

```sh
cd /workspace/ncode
export PATH=/workspace/.tools/go/bin:$PATH
export GOCACHE=/workspace/.cache/go-build
export GOPATH=/workspace/.cache/go
make build
```

启动 API 与内嵌 Vue：

```sh
cd /workspace/ncode
SEED_DEMO=false ./bin/ncode
```

打开 `http://127.0.0.1:8080`。注册后使用页面显示的开发验证令牌确认邮箱。`make build` 会先将 Vue 构建到 `web/dist`，再通过 `//go:embed all:dist` 编入可执行文件；缺少 Vite 时会按锁文件安装依赖。使用 `make web-deps` 可重新安装前端依赖。

开发 Vue 时可在一个终端运行 `go run ./cmd/api`，另一个终端进入 `web` 运行 `npm run dev`；Vite 会代理 API 请求并提供热更新。普通运行不需要单独启动 Vite。默认配置使用本地 Compose PostgreSQL；开发环境默认会向空数据库写入演示数据，设置 `SEED_DEMO=false` 可关闭。

## 验证

```sh
cd /workspace/ncode
docker compose up -d --wait postgres
export PATH=/workspace/.tools/go/bin:$PATH
export GOCACHE=/workspace/.cache/go-build
export GOPATH=/workspace/.cache/go
export TEST_DATABASE_URL='postgres://ncode:ncode_local_dev@127.0.0.1:54329/ncode?sslmode=disable'
make test
go vet ./...
make build
```

测试覆盖环境校验、Fx 依赖图、HTTP 服务生命周期、登录保护、生产环境邮件配置边界、内嵌静态资源、账户与工作区邀请、工作区数据隔离、项目标识范围，以及本地 Git 仓库初始化和文件浏览。启动构建后的二进制后，可用 `curl -fsS http://127.0.0.1:8080/healthz` 验证服务。浏览器账户到仓库的完整交互验收仍待补充。

## API

业务操作使用 `POST /api/v1/{resource}/{action}`，返回 `{ "code", "msg", "data" }`。分页响应还会在根级包含 `page`、`size`、`total`、`totalPage` 和 `responseType`。ID 为 JavaScript 安全整数；状态为数字并带有 `stateName`；日期格式为 `yyyy-MM-dd`；更新操作携带 `expectedVersion`。项目和工作项创建需要 `Idempotency-Key`。

主要路由：

- 账户：`POST /api/v1/auth/register`、`auth/verify`、`auth/login`、`auth/logout`；`GET /api/v1/auth/me`
- 工作区：`POST /api/v1/workspace/create`、`workspace/invite/create`、`workspace/invite/accept`、`workspace/invite/revoke`
- 项目：`POST /api/v1/project/create`、`project/query/page`、`project/query/one`
- 工作项：`POST /api/v1/workItem/create`、`workItem/update`、`workItem/transition`、`workItem/accept`、`workItem/query/page`、`workItem/query/activity`
- 仓库：`POST /api/v1/repository/create`、`repository/query/page`、`repository/query/tree`、`repository/query/file`
- 保护的业务 API 需要会话 Cookie；`X-Workspace-ID` 用于选择工作区，服务端会验证用户的有效成员关系。
- 健康检查：`GET /healthz`
