# 验收、排错与结果边界

## 自动匿名检查

使用实际构建目录中的 JS，不凭空写一个不存在的路径：

~~~bash
python scripts/check_entry.py \
  --base https://nas.example.com \
  --prefix /app/example-portal \
  --dashboard /path/to/dashboard \
  --api-path /api/agents \
  --ws-path /api/browser-stream/ws \
  --output /path/to/local-evidence/anonymous.json \
  --dry-run
~~~

核对后去掉 --dry-run。API/WS 路径按真实应用填写；脚本不登录、不读取 Cookie、不提交业务数据，只发无凭据、带伪造身份头的 GET/Upgrade 请求。不得发到未授权目标。

局域网自签证书可按需使用 --insecure-ip-tls；脚本将该例外限制在显式私网 IP，不可用它关闭公网域名 TLS 验证。

脚本只证明这些请求没有得到可用响应。401/403 也可能来自错误路由或其他上游，因此必须结合 NAS 日志及真实登录检查。HTTP 200 且正文恰好为 invalid token 是一种已见的拒绝表现，不把所有 200 都当拒绝。

## 真实客户端检查

至少记录以下检查分别通过、失败或未测：

- NAS 未登录/普通用户/允许用户的应用入口。
- NAS 登录后，原应用未登录和登录后的业务权限。
- 历史深链接、流式输出、WS、文件链接预览、图片、上传下载。
- 使用该应用时涉及的终端、浏览器、桌面或其他控制功能。
- 电脑和飞牛手机 App 的布局、滚动、输入区及安全区。
- NAS 实际退出/会话过期后，已有 API/WS 是否按短租约失效。
- 用户要求的直达端口策略；保留时验证精确绑定和原应用 JWT。
- 原业务数据/挂载、其他容器 ID 与状态。

剪贴板、麦克风、屏幕捕获可能受 iframe/WebView 权限影响，需要分别测试。手机、FN Connect 和普通浏览器不能相互代替。

## 排错顺序

| 症状 | 先检查 |
| --- | --- |
| NAS authorization required | peer UID、实际 Host、身份头和用户策略，勿直接扩权 |
| 域名正常、HTTP IP 失败 | 无端口 Host、完整 Origin、Secure Cookie、旧 IP Cookie 名称 |
| API 401 | 区分 Portal session expired 与后端 JWT 401；查看租约续验和业务登录 |
| 首页能开但白屏 | index 的实际 JS/CSS 路径、MIME、prefix、旧缓存及 SPA fallback |
| 流式变成整段返回 | 响应缓冲、Flush、业务链路和客户端，不只看健康检查 |
| WS 失败 | 真实 Upgrade、路径前缀、Origin、租约以及后端业务 token |
| 红色文件链接不能预览 | Markdown URL 转换、前缀识别、预览 Context 与真实工作区权限 |
| 手机顶部大留白 | 飞牛原生栏和网页 safe-area 是否叠加 |
| 应用用户读不到文件 | 精确文件所有者和 NAS ACL，不全局 chmod/chown |
| 更新后旧前端仍在 | FPK/静态修复类型、实际版本、manifest 和线上资源哈希 |
| 未登录请求返回 200 | 检查正文是否 invalid token，不按状态码判断应用可用 |

日志不记录 Cookie、Authorization、OAuth token 或完整用户数据。使用脱敏指标和精确拒绝分支定位。

## 回退

独立接入层失败时只处理该包/项目。保留原业务镜像和数据。静态前端修复与包更新采用各自的回退方法。

需要临时恢复直达入口时，要有用户授权并说明认证边界；不能在错误处理里自动重开端口。操作报错或超时后先读回当前状态，禁止盲目重复部署。
