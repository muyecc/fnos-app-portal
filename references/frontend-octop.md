# 前端适配与 Octop 参考实现

## 其他应用先判断原生能力

优先使用原应用支持的 base path、public URL、router basename 和 API/WS 基地址。检查运行时配置是否真的影响所有请求；不能只改首页资源地址。

没有原生支持时，在新建、版本固定的源码目录构建接入前端。原后端和独立访问前端保留。补丁集中管理，并对目标位置和数量做断言。

至少检查：JS/CSS、Router、根 /api/ 请求、WS、服务端返回的文件 URL、图片、上传下载、深链接、OAuth 重定向、PWA 和手机布局。

## 前端产物接口

render_project.py 要求 dashboard 目录包含 index.html 和 portal-build.json。格式如下：

~~~json
{
  "schema": 1,
  "upstream_version": "1.0.0",
  "upstream_commit": "完整的源码commit",
  "prefix": "/app/example-portal/",
  "assets": {
    "index.html": "该文件的SHA256",
    "assets/main.js": "该文件的SHA256"
  }
}
~~~

上面是字段示意，不是可通过哈希检查的真实产物。assets 必须完整记录发布文件；使用相对路径，不包含 manifest 自己。版本和 prefix 必须匹配计划。渲染器只复制已登记文件，不把整个源码或私有工作目录打包。

## Octop 适配器做了什么

scripts/build_octop_frontend.py 是实质补丁和构建脚本，配套模块在 assets/octop-frontend/。

已研究源码基线是 Octop 1.0.3b4。脚本要求官方源码归档、固定 commit、校验值以及 prepare_octop_source.py 写入的来源记录。升级其他版本需先审查差异，不声称它自动适配未来版本。

| 部位 | 适配 |
| --- | --- |
| WebSocket | 处理聊天、终端、桌面、浏览器等构建位置；基线预期 6 处，数量变化即停止 |
| API | 前缀只加一次；后端返回的 /api/ 文件地址也能使用 |
| 文件预览 | 将 Markdown 本地/工作区链接交给原 ChatFilePreviewContext，保留后端工作区鉴权 |
| 登录存储 | 使用应用 ID 专用键，例如 example-portal:auth_token，不读取、迁移或清理其他应用的 auth_token |
| PWA | 本接入版本停用注册和跨应用清缓存逻辑，保留独立上游版本 |
| 手机 | 统一接入版顶部安全区，避免宿主原生栏与 PWA 顶部预留叠加，保留底部/左右安全区 |
| Images | 保留基线中已修复的图像提供商能力判断，不替换业务配置 |
| 构建证据 | 记录原文件/补丁后哈希、上游来源和最终资源哈希 |

工作区链接识别支持正确的应用前缀，不把链接简单导航到 NAS 根路由。拒绝目录穿越、控制字符及任意外部 URL，最终文件权限仍由原后端决定。

## 执行方式

命令见 SKILL.md。关键顺序：

1. prepare_octop_source.py --dry-run 校验归档，再解压到全新目录。
2. npm ci 安装锁定依赖。
3. build_octop_frontend.py --dry-run 验证补丁目标和来源。
4. 去掉 dry-run 后构建，执行类型检查和针对性测试。
5. render_project.py 验证版本、prefix、每个资源哈希后，生成独立接入项目。

--app-id 同时改变构建 base 和配套测试的路径。不要只在 FPK 改应用名而保留旧前端前缀。

手机顶部修改基于嵌入场景，不是给所有 PWA 清零 safe-area 的通用建议；其他 WebView 必须实测。

## 不覆盖独立应用

此包不包含 Octop 后端、私有业务镜像、生产数据库、用户文件或完整上游源码。需要从合法来源取得目标源码，并遵循其许可证。生成的接入前端与原应用版本分别记录，不能用旧接入前端覆盖新后端而不检查接口。
