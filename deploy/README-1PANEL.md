# 1Panel 生产部署（Ubuntu）

小程序 API 域名以 `https://api.yujixinjiang.cn` 为准（与 `weixin/config/api.js`、`deploy/1panel.env.example` 一致）。

## 一、1Panel 里先装好基础服务

1. **应用商店**安装：`OpenResty`（或 Nginx）、`MySQL 8`、`（可选）1Panel 运行环境 / 进程守护`。
2. **安全 / 防火墙**：放行 `80`、`443`；**不要**对公网放行 `8080`、`3306`。
3. **数据库**：
   - 新建库 `yujixinjiang`（utf8mb4）
   - 新建用户（非 root），授权仅该库
   - 在「数据库 → 导入」执行本仓库 `sql/schema.sql`（**新库只跑这一份**；已内含全部 migrations / updates 最终形态，不要再跑 `sql/updates/*` 或 `migrations/*`，也不要跑 `weixin/database/schema.sql`）

## 二、上传后端

建议目录：

```text
/opt/yujixinjiang/
  bin/server          # Linux 二进制
  .env                # 从 deploy/1panel.env.example 复制并填写
  certs/              # apiclient_cert.pem / apiclient_key.pem（权限 600）
  uploads/
  backups/
```

本机 Windows 交叉编译：

```powershell
cd back_end
$env:GOOS="linux"; $env:GOARCH="amd64"; $env:CGO_ENABLED="0"
go build -ldflags="-s -w" -o bin/server ./cmd/server
```

把 `bin/server`、`sql/schema.sql`、证书、`.env` 传到服务器。

## 三、填写 `.env`（生产必填）

见 `deploy/1panel.env.example`。至少保证：

- `GIN_MODE=release`
- `JWT_SECRET` ≥ 32 位随机（`openssl rand -base64 48`）
- `PAYMENT_PROVIDER=wechat` + `WECHAT_PAY_ENABLED=true` + 商户证书齐全
- `WECHAT_PAY_NOTIFY_URL=https://你的域名/api/payments/wechat/notify`
- `UPLOAD_PUBLIC_BASE` / `AVATAR_PUBLIC_BASE` 为 HTTPS 域名

`GIN_MODE=release` 时若仍是 mock / 示例 JWT，**进程会拒绝启动**（这是预期行为）。

## 四、用 1Panel 跑进程

**方式 A（推荐）：进程守护 / Supervisor**

- 启动命令：`/opt/yujixinjiang/bin/server`
- 运行目录：`/opt/yujixinjiang`
- 环境变量：加载 `/opt/yujixinjiang/.env`（或在面板里逐项填写）
- 开机自启、异常重启

**方式 B：systemd**

```bash
sudo cp deploy/yujixinjiang.service /etc/systemd/system/
# 按实际用户修改 User=（1Panel 常见 www 或 root）
sudo systemctl daemon-reload
sudo systemctl enable --now yujixinjiang
sudo journalctl -u yujixinjiang -f
```

确认本机：

```bash
curl -s http://127.0.0.1:8080/api/health
```

## 五、1Panel 建网站 + HTTPS

1. **网站 → 创建** → 域名 `api.yujixinjiang.cn`
2. 类型选 **反向代理** → 目标 `http://127.0.0.1:8080`
3. **SSL**：申请 Let’s Encrypt（或上传证书），强制 HTTPS
4. 在站点配置中合并 `deploy/nginx-1panel.conf.example` 或使用 `deploy/api.yujixinjiang.cn.nginx.conf`（至少拦截 `/swagger`，并设 `client_max_body_size`）
5. 公网验证：

```bash
curl -s https://api.yujixinjiang.cn/api/health
```

`/swagger` 应 404。

## 六、微信侧

1. 小程序后台 → 开发 → 开发管理 → **服务器域名**：
   - request / uploadFile / downloadFile：`https://api.yujixinjiang.cn`
2. 微信支付商户平台：JSAPI、绑定小程序 AppID、支付回调 URL 与 `.env` 一致
3. 体验版实测：真实支付 0.01 元 → 订单变已支付；取消/超时关单

## 七、管理员账号

管理员只能改库，没有「注册成管理员」接口。用小程序先登录一次生成 `account`，再：

```sql
UPDATE account SET type = 3 WHERE openid = '你的openid' AND is_deleted = 0;
```

商家同理绑定 `merchant_profile.account_id`。

## 八、从旧服务器迁数据（若已有库）

不要对旧库再执行 `schema.sql`。用 mysqldump 导出旧库，在新库导入；确认 `PAYMENT_*` / 证书 / 域名已切到新机后再切 DNS。

## 九、上线检查清单

- [ ] `GET /api/health` 正常
- [ ] `GET /api/user/payment/provider` 返回 `provider=wechat` 且 `immediate_settle=false`
- [ ] `/swagger` 外网不可访问
- [ ] 上传一张图，返回 HTTPS URL
- [ ] 真实微信支付 + 回调入账
- [ ] `BACKUP_ENABLED=true` 且备份目录有文件
- [ ] 小程序正式版域名与隐私指引已配置
