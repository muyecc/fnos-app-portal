# HTTP/IP 兼容和可选直达入口

## 域名正常、IP 却提示 NAS authorization required

同一错误文案可能来自不同检查：Unix peer、Host、NAS 用户策略、Origin。先读脱敏日志定位分支，不先给用户扩权。

两个已遇到的原因：

1. 浏览器访问带 :5666，但飞牛转发到 Socket 的 Host 可能只有 IP。
2. Secure Cookie 无法在普通 HTTP 入口正常往返；只增加 Host 仍不够。

HTTPS 优先。只有用户明确选择保留 HTTP 习惯时才增加例外。示例（合入目标配置）：

~~~json
{
  "public_hosts": [
    "nas.example.com",
    "192.168.1.10",
    "192.168.1.10:5666",
    "192.168.1.10:5667"
  ],
  "allowed_origins": [
    "https://nas.example.com",
    "http://192.168.1.10:5666",
    "https://192.168.1.10:5667"
  ],
  "http_cookie_ips": ["192.168.1.10"],
  "secure_cookie": true
}
~~~

access.go 保留完整 Origin 列表，只在已允许的同一主机上兼容转发 Host 去端口。错误协议、未知端口和其他主机仍拒绝。不信任客户端 X-Forwarded-* 来决定安全降级。

IP 例外使用 AppPortalSessionLAN，域名使用 AppPortalSession。IP Cookie 不设置 Secure，仍保留 HttpOnly、SameSite=Strict、Host-only 和应用路径。不能填写域名、网段或公网 IP，也不能关闭全局 Secure。

Cookie 不按端口隔离；同一 IP 的 5666/5667 不构成独立 Cookie 安全边界。IP 与域名是不同 origin，业务应用可能首次需要分别登录。HTTP 本身仍为明文。

## 是否保留原内网链接

用户必须知道两种策略的差别：

- 仅飞牛入口：所有旁路收口后，访问才必须经过飞牛认证。
- 另保留内网直达：直达使用原应用登录，不能宣称受飞牛认证或 MFA 保护。

不要擅自增加公网反代。只绑定指定内网 IP，而不是 0.0.0.0 或 IPv6 全接口；这不等于源地址防火墙。可路由设备或既有 NAT 仍可能访问，网络侧策略需另行确认。

## 用独立转发容器恢复旧端口

若原容器含运行数据，不要为改端口直接重建。assets/lan-access/ 提供独立转发模板：

- Compose 项目与原业务、FPK 分开。
- 唯一挂载为自己的只读配置，没有业务数据或 Docker Socket。
- 非 root、只读 rootfs、cap_drop ALL、no-new-privileges。
- 使用现有 Docker 网络及服务名；resolver 127.0.0.11，动态解析业务地址。
- 支持 Upgrade；关闭请求和响应缓冲，允许长响应。
- 使用用户已确认的固定镜像版本/摘要，先验证 nginx、wget 和文件权限。
- 明确传入 APP_UPSTREAM 服务名及端口，生成配置中的占位域名后再运行。

模板不自动部署；复制到新的独立工作目录后填写 .env 和 nginx.conf。先 docker compose config --quiet、nginx -t，再只启动这个项目。不可读取旧应用配置猜测密码或扩大 NAS ACL。

后续升级保留独立项目和精确绑定，检查 /chat 等真实页面、JS、业务 API 鉴权，以及原飞牛入口仍拒绝匿名。
