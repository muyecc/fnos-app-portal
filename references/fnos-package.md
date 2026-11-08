# 飞牛 FPK、权限与部署

## 先核对当前机制

示例来自 fnOS 1.2.1000 / x86 的已实施结构，不保证未来固件或其他 CPU 可直接照搬。实施时读取当前官方文档及对应工具帮助：

- https://developer.fnnas.com/docs/core-concepts/gateway-registration/
- https://developer.fnnas.com/docs/core-concepts/app-entry/
- https://developer.fnnas.com/docs/core-concepts/index-cgi/

index.cgi 不是本方案的 WebSocket 接入方式。不要为了兼容它删掉原应用的长连接功能。

## 对应文件

- fpk/manifest：appname、version、desktop_uidir、desktop_applaunchname。
- fpk/app/ui/config：.url 中的入口名与 manifest 启动入口对应。
- fpk/config/privilege：独立应用身份，run-as=package。
- fpk/config/resource：只声明本接入层的 Docker 项目。
- fpk/app/docker/docker-compose.yaml：接入镜像、只读子挂载、外部业务网络。
- fpk/cmd/main：本应用生命周期状态。
- fpk/app/config.json：接入策略；由 render_project.py 生成。
- fpk/app/dashboard：匹配前端及其 manifest；由渲染器检查哈希后写入。

关键入口结构：

~~~json
{
  ".url": {
    "example-portal.main": {
      "title": "Example App",
      "icon": "images/icon_{0}.png",
      "type": "iframe",
      "protocol": "",
      "gatewayPrefix": "/app/example-portal",
      "gatewaySocket": "app.sock",
      "url": "/app/example-portal/",
      "allUsers": false,
      "control": {"accessPerm": "readonly"}
    }
  }
}
~~~

图标限制只处理入口展示，服务端仍须检查用户策略。包里带自制中性图标，不使用其他产品商标。

## 容器权限

Compose 使用飞牛提供的 TRIM_UID、TRIM_GID、TRIM_APPDEST，不写死 NAS 的应用 UID。

target 父目录可写用于 Socket；dashboard/config/docker/ui 子挂载为只读。容器非 root、rootfs 只读、cap_drop ALL、no-new-privileges，不发布 TCP 端口，不挂载 Docker Socket。

外部网络使用既有业务网络名。不要创建名相同但与业务不相连的新网络，也不要按一次 inspect 的容器 IP 写死上游。

上传成功不代表应用身份能读到文件。遇到 NAS ACL 拒绝时先检查所有者、应用 UID 和精确路径；不修改整个共享目录权限，不把长期网关改成 root。确需高权限运维时，只操作指定产物并在解析/写入前降权。

## 安装、升级、镜像

先检查实际 CLI 版本和子命令帮助。复用已有有效授权；不能读取或搬运其他产品、用户或 profile 的登录数据。

新安装与已安装应用的升级分开处理。先预检目标包，再确认是否可升级、CPU/系统/卷、向导与依赖。不将命令返回“任务启动”当作完成。

一个实际版本组合曾有以下差异：

- install-fpk 对已安装应用返回 10236，仍走安装流程。
- CLI 0.5.1 的 app update --package-type local 仍先走云下载，不能据参数名就假定支持本地更新。
- 同固件原生本地更新的版本字段为 updateVersion，不能把安装接口的 version 原样复用。

这些是排错线索，不是跨版本固定 API。优先使用应用中心手动更新；自动化时先核对当前官方前端/文档及只读预检，再提交正确的原生 update 流程。不能通过导出令牌、修改认证配置或卸载重装绕过问题。

本地镜像必须先在 NAS 中存在。pull_policy: never 不会生成镜像；它只防止缺失时误向公网拉取。卸载可能清理未引用镜像，回退/重装前再次检查。不得自动公开推送私有业务镜像。

没有输入的包应按当前官方模板省略向导，不随意塞入空向导文件。生命周期脚本不接管原应用数据，卸载接入包也不删除业务项目。
