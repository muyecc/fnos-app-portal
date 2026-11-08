# 架构、接口与信任边界

## 数据流

浏览器/飞牛 App → 已有飞牛入口 → 飞牛统一网关 → 应用 Unix Socket → 独立 Go 接入层 → 原应用 Docker 服务。

接入层提供匹配版本的 SPA 前端，将应用前缀下的 /api/ 去掉前缀后转发。原应用保持业务账号、权限与数据所有权。FPK 只管理接入层，不初始化第二套业务后端。

前端由目标应用的原生配置或独立适配流程准备；本仓库只规定 [通用产物和续验接口](frontend.md)，不包含特定应用实现。

独立 TCP 内网入口是可选的另一条路径，见 http-lan.md；它不拥有飞牛认证保护。

## 代码位置

- assets/portal/cmd/portal：配置、监听器、健康检查、退出。
- assets/portal/internal/gateway/peer_linux.go：真实 Unix 对端身份、0660 Socket、拒绝覆盖普通文件或活动 Socket。
- server.go：身份和路径校验、静态服务、代理与租约绑定。
- access.go：精确 Origin、Host 去端口兼容、HTTP IP Cookie。
- lease.go：随机租约、UID 绑定、过期取消和连接回收。
- *_test.go：伪造身份、错误来源、路径、Cookie、代理、WS 与真实 Unix peer 测试。

非 Linux 不开放生产监听。Windows 可以运行普通测试，但真实 SO_PEERCRED 测试只在 Linux 执行。

## 模板实际支持的应用接口

1. 前端为已完成子路径适配的 SPA。
2. 业务请求集中在 /api/。
3. 业务凭据使用 Authorization/Bearer，而非 Cookie。
4. 静态资源放在匹配的 dashboard 产物内。
5. 代理的根相对 /api/ 重定向会补应用前缀；其他绝对重定向、OAuth 回调和业务 Cookie 需要独立适配。

模板会删除所有传入 Cookie、X-Trim-* 和后端 Set-Cookie，防止 NAS 凭据进入业务。不能为兼容 Cookie 登录就简单取消整个剥离逻辑；应设计业务 Cookie 的明确允许列表、命名/路径隔离及对应测试。

## 会话接口

- GET /app/<id>/：在可信 NAS 身份下返回前端并发放租约。
- POST /app/<id>/__portal/lease：要求 X-App-Portal: lease，经飞牛重新认证后续租。
- /app/<id>/api/*：要求可信 NAS 身份和有效租约，原后端继续检查业务 JWT。
- /__health：只供网关自身 UID 经本机 Socket 检查，不作为公网旁路。

前端每 5 秒在可见时续验，示例租约 30 秒；服务端每秒清理过期租约。关闭页面或续验失败后，现有 API context 和被跟踪 WS 会关闭。后台标签页可能在恢复前台后重新连接。

续验必须真正经过飞牛认证入口，不能由内部定时任务无限续租。需要测试浏览器退出、会话失效以及已有连接，而不仅是删除某个 Cookie 后新建请求。

## 凭据与浏览器边界

业务登录保留，不自动映射账号。前端 localStorage 必须使用应用专用键，但命名空间只是避免误碰，不能抵御同源恶意脚本。同域应用的 XSS 仍可能影响 NAS 会话；HttpOnly 也不能阻止同源脚本发请求。

Socket peer 校验不防宿主 root/Docker 管理员。共享 Docker 网络也不是应用间强隔离。此方案减少独立入口，不消除 NAS 或业务漏洞。
