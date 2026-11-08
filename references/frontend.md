# 通用前端适配与产物接口

本仓库提供网关和打包工具，目标应用的前端由使用者或 AI 按实际项目准备，不附带特定应用的源码补丁或构建脚本。

## 先判断应用是否兼容

网关当前只提供 SPA 静态前端和 `/api/` 代理，业务凭据使用 Authorization/Bearer。先检查目标应用的登录机制、路由、API、WebSocket 和资源加载方式。

优先使用原应用支持的 base path、public URL、router basename 和 API/WS 基地址。检查运行时配置是否真正影响所有请求；只改首页资源地址不能证明适配完成。

没有原生支持时，在独立且版本固定的源码目录做最小适配。按项目实际工具链构建，记录上游版本、源码 commit/发布来源、归档 SHA-256 和变更。不要覆盖原应用的前端或后端，也不要把某个项目的补丁复制成通用修复。

业务 Cookie、自定义 API 前缀、服务端渲染或 OAuth 回调不符合当前模板时，应先设计对应适配并测试；不能简单关闭凭据剥离或 NAS 校验。无法可靠适配时停止部署并说明限制。

## 子路径和多应用边界

接入根路径统一为 `/app/<app_id>/`，计划、前端路由、资源和 FPK 入口必须一致。至少核对：

- JS/CSS、Router 和直接打开深链接后的 SPA fallback。
- API 请求只加一次前缀；根 `/api/` 地址改为接入路径下的 `/api/`。
- WebSocket 的地址、Upgrade、业务凭据、Origin 和重连行为。
- 服务端返回的文件/图片 URL、上传下载、流式响应和应用自己的预览功能。
- 登录存储使用应用专用键，不读取、迁移或清理其他应用的数据。
- Service Worker、缓存、manifest 和 PWA 的作用范围不跨应用；不要清理整个域名的缓存。
- 手机 iframe/WebView 的布局和 safe-area；先实测，不统一清零所有应用的顶部安全区。

不同路径仍属于相同 origin，存储键命名只避免冲突，不构成针对恶意同源脚本的安全隔离。每个应用使用唯一 app_id、独立接入包、配置和 Compose 项目；可选 LAN 转发也要使用独立名称和端口。

## 浏览器租约接口

首页通过飞牛认证后，网关发放应用路径下的 HttpOnly 会话 Cookie。前端必须实现续验，否则默认 30 秒后业务 API/WS 会因租约失效而关闭。该接入租约不代替原应用的业务登录。

在页面可见时约每 5 秒发出：

~~~http
POST /app/<app_id>/__portal/lease
X-App-Portal: lease
~~~

浏览器请求携带 `credentials: "same-origin"`、禁用缓存，并设置合理超时；Cookie 由浏览器处理，不读取 NAS token。成功响应必须同时满足 HTTP 成功、JSON Content-Type 和 `portal_session: true`，不能把登录页面或任意 HTTP 200 当作续验成功。

在恢复前台时立即续验，防止重复启动多个计时器，并在组件卸载时清理。续验失败后停止当前应用的业务连接，重新经过飞牛入口认证或显示需要重新登录的状态。不要由内部定时任务绕过飞牛入口续租。

必须验证实际退出飞牛或会话过期后的 API/WS 回收；仅删除 Cookie 或访问健康检查不能代替这项验收。

## 前端产物接口

`render_project.py` 的输入是构建好的 dashboard 目录，其中包含 `index.html` 和 `portal-build.json`：

~~~json
{
  "schema": 1,
  "upstream_version": "1.0.0",
  "prefix": "/app/example-portal/",
  "assets": {
    "index.html": "该文件的SHA256",
    "assets/main.js": "该文件的SHA256"
  }
}
~~~

以上仅为字段示意，哈希不是可直接使用的真实产物。可在 manifest 另记 upstream_commit 等来源元数据；当前渲染器校验的是计划版本、prefix、文件路径和内容哈希，不代替上游来源核实。

构建完成后，由目标项目的发布脚本生成完整 `assets` 映射：

1. 明确只包含可公开的静态发布文件，不扫描整个源码或私有工作目录。
2. 键为相对于 dashboard 的 POSIX 路径，值为文件内容的 SHA-256。
3. 必须包含 `index.html`，以及实际使用的 JS、CSS、字体、图标等；不包含 manifest 自身、符号链接、私密配置或生产源码映射。
4. prefix 与计划中的 app_id 一致，上游版本与实际后端匹配。

渲染器只复制已登记的文件和 manifest。哈希一致只证明文件没有变化，不能证明前端功能、安全性或来源合法；完成真实客户端验收后才能判定目标实例可用。
