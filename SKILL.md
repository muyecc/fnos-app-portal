---
name: fnos-app-portal
description: 将已有 Docker Web 应用接入飞牛 fnOS 桌面和手机 App，复用飞牛统一网关及登录校验。提供通用接入网关、FPK 模板、前端适配接口和验收方法；支持部署、HTTP/IP 兼容及后续升级，不改飞牛底层或自带插件。
metadata:
  author: "微信公众号：就是会玩儿"
---

# 飞牛应用接入

## 目标与适用范围

将现有应用接入飞牛桌面和手机 App，复用用户已有的飞牛远程入口。原应用继续管理业务、账号和数据；新建独立接入包，不替换飞牛 Web 服务。

本 Skill 包含源码、测试和工具，不是已经适配所有应用的一键安装包：

- 网关基线：SPA 静态前端、根路径 /api/、Authorization/Bearer 业务鉴权。
- 应用兼容：先检查原生子路径配置、API 前缀、Cookie 登录、OAuth 回调、绝对重定向和 WebSocket 路径；优先配置，确需代码适配时在目标应用的独立工作目录实施并补测试，不关闭鉴权。
- 本仓库不附带特定应用的补丁、源码下载器或前端构建脚本；不能把通用模板理解为任意服务都可直接部署。
- FPK 模板：x86 参考基线；其他 CPU/固件先核对官方支持。

网关会剥离 Cookie、X-Trim-* 和后端 Set-Cookie。依赖业务 Cookie 的应用不能原样使用此模板。详见 [架构与边界](references/architecture.md)。

## 按任务读取材料

- 第一次接入：先读 [架构与边界](references/architecture.md)、[FPK 打包](references/fnos-package.md)，再读 [通用前端适配接口](references/frontend.md)。
- 域名正常、内网 IP 报无授权，或需要保留直达入口：读 [HTTP 与内网入口](references/http-lan.md)。
- 升级已有接入：读 [发布与升级](references/release.md)，不要从头重装。
- 白屏、401/403、手机留白、文件预览或流式故障：读 [验收与排错](references/acceptance.md)。

以上文件位于 references/，按需读取，不把所有实现内容重复塞回 SKILL.md。

## 执行前：只读建立目标清单

先确认用户授权范围。只要求调查或生成方案时，不部署；已明确要求实现/上线时，继续完成已授权工作，不因普通实现细节反复确认。

使用用户指定的 NAS 和有效授权，检查：

1. fnOS 版本、CPU、现有工具和授权是否可用。
2. 业务容器、Compose 项目/服务、镜像 ID、健康状态、发布端口及 Docker 网络。
3. 持久化挂载和可写层变化；浏览器 profile、下载、截图不应随重建丢弃。
4. 实际域名/IP/协议/端口、NAS 转发时的 Host 和 Unix peer 身份。
5. 用户允许哪些 NAS 用户进入，以及是否保留原内网直达链接。

先文字说明模块职责、预计变更和后续升级方式。不得根据其他实例的 UID、地址、授权 profile 或容器名直接操作目标。

管理工具优先使用现有 fnOS CLI 技能或用户已提供的连接。授权失效才走该工具要求的用户授权流程；不索取密码/验证码，不导出令牌。没有可靠管理通道时，交付本地方案和构建物，不能谎称上线。

## 本地准备与构建

将本 Skill 的位置记为 SKILL_DIR，将新建工作目录记为 WORK。不要在原业务容器或已经打补丁的源码目录重复修改。

1. 复制 references/plan.example.json 到工作目录填写实际参数。只有实测确认 peer UID 后，才将 peer_uid_verified 改为 true。
2. 按 [通用前端适配接口](references/frontend.md) 检查目标应用，使用其原生子路径配置或独立适配流程构建匹配前端，接入浏览器租约续验，并生成 portal-build.json。这里没有通用的一键前端转换命令。
3. 渲染新项目，先 dry-run，再执行：

~~~bash
python "$SKILL_DIR/scripts/render_project.py" \
  --plan "$WORK/plan.json" --dashboard "$WORK/dashboard" \
  --output "$WORK/project" --dry-run
# 核对输出后，同参数去掉 --dry-run；output 必须不存在。
~~~

4. 构建网关，先 dry-run，再执行：

~~~bash
python "$SKILL_DIR/scripts/build_gateway.py" \
  --project "$WORK/project" --dry-run
# 同参数去掉 --dry-run：运行 Go 测试、vet，再交叉编译 Linux x86。
~~~

Windows 使用 py 替代 python，并按 PowerShell 语法写路径/换行。测试平台与生产平台分开记录；Windows 测试不代替 Linux SO_PEERCRED 测试。

5. 本地构建镜像，镜像名/版本使用渲染后 Compose 声明的值：

~~~bash
docker build -t example-portal-local:0.1.0 "$WORK/project"
~~~

示例标签不是目标实例的真实版本。离线部署需导出、传输并在 NAS 导入镜像；本地 build 不等于 NAS 已有镜像，不自动公开推送镜像。

6. 用当前官方 fnpack 打包：

~~~bash
python "$SKILL_DIR/scripts/build_package.py" \
  --project "$WORK/project" --output "$WORK/fpk-stage" \
  --fnpack /path/to/fnpack --dry-run
# 核对后去掉 --dry-run。该工具只打包，不安装到 NAS。
~~~

## 上线顺序

1. 核对现有容器、网络、镜像与前端版本，记录脱敏基线。
2. 按 fnos-package.md 检查 FPK manifest、入口、资源、权限及所需镜像。
3. 优先在隔离项目验证。所有临时容器显式使用独立 Compose project/service 标签。
4. 用户授权上线后，通过原生应用中心安装或更新独立接入包。
5. 查询任务终态、应用版本、健康状态、Socket 权限和实际挂载。
6. 执行匿名/伪造身份检查，并由真实已登录客户端验收电脑和手机。
7. 新入口可用后，才按用户选择关闭或保留直达端口。
8. 核对其他容器状态，精确清理本次临时资源。

不得整机 down、删除数据卷、清理其他项目或改飞牛系统配置。是否备份遵从用户要求；不能把用户说“不备份”理解为允许丢弃未持久化的运行数据。

## 不得破坏的认证边界

- 只在受限 Unix Socket 上接受 NAS 身份，通过 SO_PEERCRED 校验真实转发进程。
- 在服务端检查 NAS 用户策略、精确 Host/Origin 和路径，图标可见性不能代替鉴权。
- 保留原应用 JWT；NAS 用户与业务用户不自动合并，不宣传为账号 SSO。
- 飞牛凭据不转发给业务后端。NAS 同域应用和宿主/Docker 管理员属于额外信任边界，不能作绝对安全承诺。
- 需要退出撤销时，启用浏览器续验和服务端短租约，并验证真实退出后的 API/WS 关闭。
- HTTP 只允许用户指定的精确私网 IP Cookie 例外，不全局取消 Secure。
- 保留直达链接时，明确它不经过飞牛认证；不得再声称整个实例只能登录飞牛后访问。

## 完成标准与交付

按 acceptance.md 检查页面、真实静态资源、API、WS、文件预览、流式响应、手机布局、退出撤销和端口策略。

scripts/check_entry.py 只验证无凭据/伪造头的拒绝情况；不能把它当作真实登录验收，也不能仅根据 HTTP 200 判断成功。

交付匹配版本的接入源码、计划配置、前端 manifest、镜像/FPK 校验值、更新步骤和回退办法。分别说明已完成、仅本地验证和待用户验证的项目。

不安装本 Skill、不推送 GitHub、不公开镜像，除非用户明确要求这些动作。
