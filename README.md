# NCode

NCode 是一个中文项目协作与交付平台。当前实现依据所提供的 SRS v4.0，是面向项目作业与验收的本地开发范围。需求文档标注为「待评审」；本实现不是生产发布，也不构成 P0 验收结论。

## 当前范围

- 创建并列出项目，包含目标、标识（slug）、类型、可见性，以及计算得出的进度。
- 创建并编辑工作项，包含负责人、优先级、截止日期、验收清单和变更历史。
- 工作项可在待处理、就绪、进行中、待评审、待验收和已验收之间流转。
- 验收工作项前，当前验收清单的每一项都必须勾选。写操作携带 `expectedVersion`；版本过期的写入返回 HTTP 409。项目与工作项的创建支持 `Idempotency-Key` 重放。
- 桌面与移动端宽度使用同一套界面。

工作台中的项目进度由工作项状态计算：待处理 / 已取消为 0%，就绪为 20%，进行中为 50%，待评审为 75%，待业务验收为 90%，已验收为 100%，再对项目内全部工作项取平均。没有工作项的项目显示 0%。

当前范围不包含身份认证、租户授权、Git 托管、拉取请求执行、隔离工作区或 CI、学习评估、成果分享、招聘，以及发布 / 部署。其他导航入口会明确显示尚未接入。API 与前端默认只绑定回环地址；API 会拒绝非回环绑定地址。请不要把这个开发构建暴露到网络上，也不要用它处理真实的候选人、企业或生产数据。

## 技术栈

- Go 1.27.2、Gin、GORM、Fx
- PostgreSQL 17，用于项目、工作项、验收清单、活动记录和幂等记录
- Redis 7 已包含在 Compose 中，供下一阶段的队列 / 协调使用；当前范围不依赖它
- Vue 3、Vite、Element Plus 和 JavaScript

## 后端模块

Fx 组合根位于 `internal/app`。它组合配置、日志、数据库、API 和 HTTP 服务模块；`cmd/api` 只负责启动该应用。数据库模块负责连接检查、结构迁移、演示数据灌入和连接池清理。HTTP 服务模块在 Fx 启动时绑定端口，把绑定错误回报给启动流程，并在数据库关闭之前优雅关闭。

## 配置

应用从 `configs/config.yaml` 读取服务和 PostgreSQL 设置。设置 `CONFIG_FILE` 可加载另一份 YAML。`DATABASE_PASSWORD` 可覆盖 YAML 中的密码，`DATABASE_URL` 可覆盖部署环境中的完整 PostgreSQL 地址。`API_ADDR`、`APP_ENV` 和 `SEED_DEMO` 也可以覆盖对应的 YAML 值。生产环境始终关闭演示数据灌入。

已提交的 YAML 只包含仅限回环地址的 Compose 数据库凭据，仅供本地开发使用。请把真实部署凭据放在版本控制之外。

使用私有 PostgreSQL 实例时，把 JDBC 地址和用户名写入被忽略的 `configs/config.local.yaml`，不要把密码写进该文件，通过环境的密钥设置提供 `DATABASE_PASSWORD`，并以 `CONFIG_FILE=configs/config.local.yaml` 启动。API 启动时会执行 GORM AutoMigrate，因此只在打算变更表结构时，才对着目标数据库启动。

## 本地启动

在仓库根目录启动 PostgreSQL 和 Redis：

```sh
cd /workspace/ncode
docker compose up -d --wait postgres redis
```

在一个终端中启动 API：

```sh
cd /workspace/ncode
export PATH=/workspace/.tools/go/bin:$PATH
export GOCACHE=/workspace/.cache/go-build
export GOPATH=/workspace/.cache/go
go run ./cmd/api
```

在另一个终端中启动前端：

```sh
cd /workspace/ncode/web
npm run dev
```

打开 `http://127.0.0.1:5173`。API 默认使用本地 Compose 数据库 `127.0.0.1:54329`，并会向空数据库灌入三个演示项目及其工作项。设置 `SEED_DEMO=false` 可关闭灌入。本地 Compose 数据库密码仅供开发使用。

## 验证

```sh
cd /workspace/ncode
export PATH=/workspace/.tools/go/bin:$PATH
export GOCACHE=/workspace/.cache/go-build
export GOPATH=/workspace/.cache/go
go test ./...
go vet ./...
cd /workspace/ncode/web
npm run build
```

自动化测试覆盖环境校验、Fx 依赖图，以及 HTTP 服务的启动与关闭。本次搭建过程中，还对着临时 PostgreSQL 数据库验证了项目创建、幂等重放与冲突、工作项状态流转、验收清单约束、过期版本冲突、分页和活动历史。浏览器流程创建了项目和工作项，走完生命周期，并检查了桌面端与移动端工作台。

## 接口形态

业务操作使用 `POST /api/v1/{resource}/{action}`，返回 `{ "code", "msg", "data" }`。分页响应还会在根级包含 `page`、`size`、`total`、`totalPage` 和 `responseType`。ID 是对 JavaScript 安全的整数；状态为数字并带有 `stateName`；日期使用 `yyyy-MM-dd`；更新操作携带 `expectedVersion`。创建项目和工作项需要 `Idempotency-Key`。

可用路由包括：

- `POST /api/v1/project/create`、`project/query/page`、`project/query/one`
- `POST /api/v1/workItem/create`、`workItem/update`、`workItem/transition`、`workItem/accept`
- `POST /api/v1/workItem/query/page`、`workItem/query/activity`
- `GET /healthz`
