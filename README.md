# 豫记信疆 · Go 后端

## 第一次运行（按顺序来）

### 1. 确认 Go 已安装

```bash
go version
```

应看到类似 `go version go1.22.x`。

未安装时（Ubuntu）：

```bash
sudo apt update && sudo apt install -y golang-go
```

### 2. 准备 MySQL 数据库

确保 MySQL 已启动，然后执行项目里的建表脚本：

```bash
mysql -u root -p < ../sql/schema.sql
```

### 3. 配置环境变量

```bash
cd backend
cp .env.example .env
```

编辑 `.env`，至少改这几项：

```
DB_PASSWORD=你的MySQL密码
JWT_SECRET=随便一串长字符
```

### 4. 下载依赖并启动

国内网络建议先设置 Go 代理（只需执行一次）：

```bash
go env -w GOPROXY=https://goproxy.cn,direct
```

然后：

```bash
go mod tidy
go run cmd/server/main.go
```

看到 `服务已启动: http://localhost:8080` 即成功。

浏览器打开 **Swagger 文档**：`http://localhost:8080/swagger/index.html`

### 5. 测试接口

健康检查：

```bash
curl http://localhost:8080/api/health
```

微信登录（小程序 `wx.login` 取得 code）：

```bash
curl -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"code":"wx.login返回的code"}'
```

返回里的 `token` 可用于后续请求：

```bash
curl http://localhost:8080/api/auth/me \
  -H "Authorization: Bearer 这里填上面的token"
```

## 目录说明

```
backend/
├── cmd/server/main.go      # 程序入口
├── internal/
│   ├── config/             # 读取 .env 配置
│   ├── database/           # MySQL 连接
│   ├── handler/            # HTTP 接口
│   ├── middleware/         # 鉴权、跨域
│   ├── model/              # 数据库表对应的结构体
│   ├── response/           # 统一 JSON 返回格式
│   └── router/             # 路由注册
├── .env.example
└── go.mod
```

## Swagger 接口文档

启动服务后访问：

```
http://localhost:8080/swagger/index.html
```

（端口以 `.env` 中 `PORT` 为准。）

### 在线调试步骤

1. 在小程序或微信开发者工具中取得 `code`，调用 **认证 → 微信登录**
2. 复制响应中的 `token`
3. 点击页面右上角 **Authorize**，输入 `Bearer 你的token`（注意保留 `Bearer ` 前缀）
4. 即可调试需登录的接口

新增或修改接口注解后，重新生成文档：

```bash
make swagger
# 或
swag init -g cmd/server/main.go -o docs --parseDependency --parseInternal
```

## 常用命令

| 命令 | 作用 |
|------|------|
| `go run cmd/server/main.go` | 启动开发服务 |
| `make swagger` | 重新生成 Swagger 文档 |
| `go mod tidy` | 整理/下载依赖 |
| `go build -o bin/server cmd/server/main.go` | 编译成可执行文件 |

编译后运行：

```bash
./bin/server
```

## 数据库定时备份

在 `.env` 中开启（需本机已安装 `mysqldump`，Ubuntu 一般为 `mysql-client` 包）：

```
BACKUP_ENABLED=true
BACKUP_DIR=backups          # 相对 backend 工作目录
BACKUP_INTERVAL=24h         # 支持 Go duration：1h、12h、24h
BACKUP_RETAIN_DAYS=7        # 过期自动删除
BACKUP_COMPRESS=true        # 输出 .sql.gz
```

服务启动后会在**独立后台 goroutine** 中按间隔执行备份；启动时立即备份一次。备份文件已加入 `.gitignore`，不会进仓库。

恢复示例：

```bash
# 未压缩
mysql -u root -p yujixinjiang < backups/yujixinjiang_20260701_120000.sql

# gzip 压缩
gunzip -c backups/yujixinjiang_20260701_120000.sql.gz | mysql -u root -p yujixinjiang
```

## 架构与并发说明

本项目 **不是** Node.js 那种 async/await 单线程事件循环，而是 **Go 经典同步 HTTP + goroutine 并发**：

| 层级 | 模型 |
|------|------|
| HTTP（Gin） | 每个请求由 net/http 分配一个 **goroutine**，handler 内代码是**同步顺序执行** |
| 数据库（GORM） | **连接池**（MaxOpen=50 / MaxIdle=10），多 goroutine 复用连接，非每请求一新连接 |
| 定时备份 | **单独 goroutine** + `time.Ticker`，与 HTTP 处理并行，互不阻塞 |
| 业务逻辑 | 无独立 worker 队列、无消息中间件；订单/拼团等在请求 goroutine 内同步写库 |

因此：整体是 **多 goroutine 并发**，但单个 API handler **不是异步回调链**；若某接口耗时过长，会占住该请求的 goroutine（应用内暂无后台任务队列）。

## 用户个人中心接口（需登录，type=1）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/user/overview` | 个人中心概览（资料摘要 + 各项数量统计） |
| GET | `/api/user/profile` | 完整个人信息（账号 + 扩展资料 + 地址列表 + 统计） |
| GET | `/api/user/orders?page=1&page_size=10&status=` | 历史订单（status 可选） |
| GET | `/api/user/orders/:id` | 订单详情 |
| GET | `/api/user/cart` | 购物车内容 |
| GET | `/api/user/coupons?status=0` | 优惠券列表及未使用数量 |
| GET | `/api/user/inventory` | 背包库存 |
| POST | `/api/user/inventory/{id}/use` | 使用背包商品（可指定数量） |
| GET | `/api/user/inventory/usages` | 使用记录列表 |
| POST | `/api/user/inventory/usages/{id}/cancel` | 取消使用（回滚库存） |
| GET | `/api/user/deliveries?scope=` | 配送单列表（active/pending_confirm/history） |
| POST | `/api/user/deliveries/{id}/confirm` | 确认收货 |
| POST | `/api/user/orders/{id}/confirm-receipt` | 按订单确认收货 |

### 收货地址（需登录，type=1）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/user/addresses` | 地址列表 |
| POST | `/api/user/addresses` | 新增地址 |
| GET | `/api/user/addresses/:id` | 地址详情 |
| PUT | `/api/user/addresses/:id` | 更新地址 |
| DELETE | `/api/user/addresses/:id` | 删除地址（逻辑删除） |
| PATCH | `/api/user/addresses/:id/default` | 设为默认地址 |

新增/更新 body 示例：

```json
{
  "contact_name": "张三",
  "contact_phone": "13800138000",
  "province": "河南省",
  "city": "信阳市",
  "district": "浉河区",
  "detail": "某某路 1 号",
  "is_default": 0
}
```

测试示例：

```bash
TOKEN="你的token"

curl http://localhost:8080/api/user/overview -H "Authorization: Bearer $TOKEN"
curl http://localhost:8080/api/user/orders?page=1 -H "Authorization: Bearer $TOKEN"
curl http://localhost:8080/api/user/inventory -H "Authorization: Bearer $TOKEN"
curl http://localhost:8080/api/user/addresses -H "Authorization: Bearer $TOKEN"
```

## 逻辑删除

全表使用 `is_deleted` 字段（0=正常，1=已删除），查询默认过滤已删除记录，删除操作均为逻辑删除。

- 新库：直接使用 `sql/schema.sql` 建表
- 存量库：依次执行 `sql/updates/004_add_soft_delete.sql`、`005_add_product_sale_flags.sql`
- 背包流水与使用记录：`009_inventory_usage.sql`
- 自取/订座：`012_add_pickup_and_reservation.sql`（product 加 `allow_pickup`、merchant_profile 加 `allow_reservation`）
- 配送开关：`013_add_allow_delivery.sql`（product 加 `allow_delivery`）

## 商品管理接口（管理端 / 商家端）

创建/更新商品 body 字段（拼团与优惠券）：

| 字段 | 说明 |
|------|------|
| `enable_group_buy` | 0=不支持拼团，1=支持 |
| `group_buy_target_count` | 成团人数（≥2），开启拼团时必填 |
| `group_buy_price` | 团购价，须低于 `price` |
| `enable_coupon` | 0=不可用券，1=可用（创建时默认 1） |

管理端 `POST /api/admin/products`，商家端 `POST /api/merchant/products`。  
`PUT` / `PATCH .../products/:id` 为**选择性更新**：只传需要修改的字段，未传字段（含图片、名称、价格等）保留原值。

单独修改的 PATCH 接口：

| 方法 | 管理端 | 商家端 | 说明 |
|------|--------|--------|------|
| PATCH | `.../products/:id` | 同左 | **选择性更新**（只传要改的字段） |
| PATCH | `.../products/:id/status` | 同左 | 上架/下架 |
| PATCH | `.../products/:id/price` | 同左 | 改价（拼团商品新价须高于团购价） |
| PATCH | `.../products/:id/stock` | 同左 | 改库存 |
| PATCH | `.../products/:id/group-buy` | 同左 | 单独改拼团配置 |
| PATCH | `.../products/:id/coupon` | 同左 | 单独改优惠券开关 |
| PATCH | `.../products/:id/sale` | 同左 | **编辑页推荐**：一次性保存拼团+优惠券 |

拼团配置 PATCH body 示例：

```json
{
  "enable_group_buy": 1,
  "group_buy_target_count": 3,
  "group_buy_price": 79.9
}
```

销售方式一次性保存（编辑页 `PATCH .../products/:id/sale`）：

```json
{
  "enable_group_buy": 1,
  "group_buy_target_count": 3,
  "group_buy_price": 79.9,
  "enable_coupon": 1
}
```

用户端按商家查商品（无需登录，仅返回**已上架**商品 `status=1`）：

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/products?merchant_id={id}` | 推荐：query 传商家 ID |
| GET | `/api/merchants/{id}/products` | 等价，path 传商家 ID |

可选 query：`page`、`page_size`、`category_id`、`keyword`。

用户端商品响应含 `can_group_buy`、`can_use_coupon`、`group_buy_id` 及 `sale_options`（单独购买/拼团的价格、是否可用券）。

## 鉴权说明

除以下公开接口外，**所有 `/api/*` 信息类接口均需登录**：

- `GET /api/health` — 健康检查
- `POST /api/auth/login` — 微信登录
- `GET /api/merchants` 等商城浏览接口

请求时在 Header 携带：

```
Authorization: Bearer <token>
```

路由分层：

| 前缀 | 角色 | 条件 |
|------|------|------|
| `/api/auth/me` 等 | 已登录任意角色 | — |
| `/api/...` 用户接口 | 客户端 | `account.type=1` |
| `/api/merchant/...` | 商家端 | `type=2`（管理员 `type=3` 也可，需传 `merchant_id`） |
| `/api/admin/...` | 管理端 | `type=3` |
| `/api/rider/...` | 骑手端 | `account.is_rider=1`（与 type 独立，用户/商家均可成为骑手） |

账号字段说明：`type` 表示平台角色（用户/商家/管理员）；`is_rider` 表示是否已通过骑手审核。审核通过后 **无需重新登录**，下次请求即生效。

管理员进入商家端：请求 `/api/merchant/*` 时在 query 或 Header 带上 `merchant_id`（或 `X-Merchant-Id`）。

**管理员联调**：`type=3` 的管理员账号可 bypass 用户端、骑手端的路由角色校验，用于测试 `/api/user/*`、`/api/rider/*` 等全部接口（商家端仍需 `merchant_id`）。

创建商家（`POST /api/admin/merchants`）仅需 `shop_name`，`phone`、`openid` 可选，后续再绑定微信登录。

新增业务接口请注册到 `internal/router/router.go` 对应分组，勿放到公开路由。

## 微信登录

在 `.env` 中配置小程序凭证后，小程序端调用 `wx.login()` 取得 `code`，再请求：

```bash
curl -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"code":"wx.login返回的code"}'
```

成功返回 `token`、`account`（含 `type` 与 `is_rider`）、`is_new`。已预绑定 openid 的商家/管理员将按原角色登录。

| HTTP | 说明 |
|------|------|
| 400 | code 无效/已使用等微信侧错误 |
| 403 | 账号已被禁用 |
| 503 | 未配置 `WECHAT_APPID` / `WECHAT_SECRET` |

## 下一步

- 部署后访问 `/swagger/index.html` 查看完整 API（约 55+ 路径）
- 存量库依次执行：`004` … `007_add_merchant_images.sql`、`008_add_account_is_rider.sql`、`012_add_pickup_and_reservation.sql`、`013_add_allow_delivery.sql`

## 前端对接说明

### 权威状态定义

后端以 `docs/state-machines.md` 为准。订单响应含：

- `status`（int 0–12）：主状态
- `merchant_review_stage`（int 0–5）：商家两阶段审核
- `status_code`（string）：小程序映射码
- `status_text`（string）：中文说明

| status_code | 含义 | 条件 |
|-------------|------|------|
| `pending_group` | 待成团 | status=1 |
| `pending_merchant` | 待订单审核 | status=2, review=1 |
| `rejected` | 已拒绝 | review=2 |
| `approved` | 已通过待申请使用 | review=3 |
| `pending_use_merchant` | 待库存确认 | review=4 |
| `ready_pickup` | 待自取/待核销 | status=5 |
| `pending_rider` | 待骑手接单 | status=3 |
| `delivering` | 配送中 | status=4 |
| `completed` | 已完成 | status=7 |

列表筛选：`GET /user/orders?status_code=pending_group`

### 商品拼团字段

商品响应含 `enable_group_buy`、`group_buy_target_count`（拼团人数）、`group_buy_price`、`enable_coupon`。

### 本次新增 API 摘要

| 模块 | 路径 |
|------|------|
| 购物车 | POST/PATCH/DELETE `/user/cart` |
| 背包 | GET `/user/inventory`；POST `/user/inventory/{id}/use`；GET `/user/inventory/usages`；POST `.../usages/{id}/cancel` |
| 地址 | POST/PUT/DELETE/PATCH `/user/addresses`（已有） |
| 下单 | POST `/user/orders`，POST `.../cancel|request-use|confirm-pickup|confirm-receipt` |
| 配送 | GET `/user/deliveries`；POST `.../deliveries/{id}/confirm` |
| 拼团进度 | GET `/products/{id}/group?team_id=` |
| 商家订单 | GET/PATCH `/merchant/orders`，POST `/merchant/verify` |
| 背包使用 | GET `/merchant/inventory-usages?status=5`；PATCH `.../cancel-review` |
| 骑手 | GET/POST `/rider/orders`（accept/start/complete） |
| 统计 | GET `/admin/dashboard`，GET `/merchant/dashboard` |
| 分类 | GET `/categories`（创建商品也可传 `category_name` 自动建分类） |
| 资料 | PATCH `/auth/profile` |
| 商家资料 | GET/PATCH `/merchant/profile`；PATCH `/merchant/profile/images` |
| 图片 | POST `/upload`；PATCH `/admin|merchant/products/{id}/images`；PATCH `/admin/merchants/{id}/images`、`/merchant/profile/images`；静态访问 `/uploads/...` |

### 图片上传

1. **上传**：`POST /api/upload`（需登录），`multipart/form-data` 字段 `file`，支持 jpg/png/gif/webp，默认最大 10MB。
2. **响应**：`url`（如 `/uploads/2026/06/30/xxx.jpg`）、`full_url`（含域名，供小程序展示）。
3. **绑定商品**：
   - 创建/更新商品时在 body 中带 `images` 数组与可选 `cover_url`；
   - 或 `PATCH /api/admin/products/{id}/images` / `PATCH /api/merchant/products/{id}/images`，body：`{"images":["/uploads/..."],"cover_url":"/uploads/..."}`。
4. **绑定店铺**：
   - 创建商家时可带 `images` 与可选 `shop_logo`；
   - 或 `PATCH /api/admin/merchants/{id}/images` / `PATCH /api/merchant/profile/images`，body：`{"images":["/uploads/..."],"shop_logo":"/uploads/..."}`。
5. **访问**：服务启动后 `GET /uploads/{path}` 直接读本地 `UPLOAD_DIR`（默认 `uploads/`）。
6. **环境变量**：见 `.env.example` 中 `UPLOAD_*`。

### 背包与使用记录

1. **购买入库**：商家**购买审核通过**（`PATCH /merchant/orders/{id}/review` approve=true）后写入背包；幂等，不会重复入库。库存确认（use-review）时若尚未入库会补入。
2. **使用**：`POST /user/inventory/{id}/use`，body 示例：`{"quantity":2,"delivery_type":1}`；自提返回 `verify_code`，并写入 `user_inventory_usage`。
3. **取消使用**：`POST /user/inventory/usages/{id}/cancel`（可选 `reason`）。自提/配送未接单：**立即取消**并回滚库存；骑手已接单：**取消待审核**（status=5），商家 `PATCH /merchant/inventory-usages/{id}/cancel-review` 同意后回滚。
4. **订单取消回滚**：待审核前用户取消、或商家拒单时，若该订单已入库则按订单数量回滚背包（库存不足则拒绝取消）。
5. **核销**：商家 `POST /merchant/verify` 扫使用单核销码，完成使用记录。
6. **迁移**：执行 `009_inventory_usage.sql`、`010_inventory_usage_cancel.sql`、`011_delivery_deliver_proof.sql`。

### 骑手配送流程

1. 待接单：`GET /rider/orders?scope=pending`
2. 接单：`POST /rider/orders/{id}/accept`
3. 开始配送：`POST /rider/orders/{id}/start`
4. 送达：`POST /rider/orders/{id}/complete`，body 示例：`{"remark":"已放门口","photos":["/uploads/.../proof.jpg"]}`
5. 用户配送中列表：`GET /user/deliveries?scope=active`
6. 用户待确认：`GET /user/deliveries?scope=pending_confirm`
7. 用户确认收货：`POST /user/deliveries/{id}/confirm` 或 `POST /user/orders/{id}/confirm-receipt`

### 支付

- 默认 `PAYMENT_PROVIDER=mock`：下单事务内记 `pay_status=已支付`；取消/拒单/拼团超时记已退款
- 预留：`GET /user/payment/provider`、`POST /user/orders/{id}/pay`、`POST /payments/wechat/notify`
- 切换 `PAYMENT_PROVIDER=wechat` 前需配置 `WECHAT_PAY_*` 并完成统一下单/回调实现；未完成前会拒绝下单结算
