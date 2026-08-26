# 图片上传与文件附件

本文固定用户上传、历史媒体和服务聚合媒体的边界。目标是恢复安全、可维护的正文图片与文件附件，
但不把内部 Storage 暴露成通用对象存储 API。

## 现状

仓库同时存在三种媒体表达：

| 表达 | 权威位置 | 当前用途 | 运行时状态 |
| --- | --- | --- | --- |
| Plate `img` 节点 | `Entry.rawBody`，并序列化进 `Entry.body` | 用户正文内联图片 | 可读取、可编辑，但没有插入/上传入口 |
| `Entry.thumbnails` | Entry protobuf | RSS/Twitter/Archive 等来源附带的预览图 | 可镜像、可渲染；不作为用户上传格式 |
| `Entry.files` | Entry protobuf | FriendFeed 历史文件附件 | 迁移和镜像仍保留；Web 页面没有上传或展示入口 |

`POST /a/upload` 是 2021 年 Plate 剪贴板图片上传的残留后端。对应前端在 2026 年删除旧 editor
options 时一并消失；当前生产前端没有调用方。该 handler 目前：

1. 仅要求登录，接收单个 multipart `file`，请求上限 20 MiB；
2. 把文件完整读进内存，以内容 SHA-256 写入 ffweb 本地 `media_path`；
3. 无条件按图片解码并尝试生成 1024 px 缩略图；
4. 返回同站 `/file/<path>` 与 `/file/<thumbnail-path>`；
5. 不修改 Entry，也不产生 `Entry.thumbnails` 或 `Entry.files`。

这条残留链路不能直接恢复前端使用：

- 它信任 multipart 声明的 Content-Type，未按文件字节做 allowlist 校验；
- 任意非图片会先写入原件，再因缩略图解码失败返回 500，留下孤儿对象；
- 20 MiB 压缩图片可能解码为远大于请求体的像素缓冲，只有请求体上限，没有像素上限；
- ffweb 使用 `LocalStorage`，绕过 `media.NewStorage` 的 local + R2 写入契约；
- 返回 `/file` 同站 URL，绕过 `media_url` 的独立媒体 origin；
- thumbnail 直接写磁盘，未原子发布、未写 R2，也不是完整的内容寻址对象；
- 上传与发帖不在一个事务中，取消编辑会留下无引用对象；
- `Entry.files` 没有 Web renderer，把任意文件接进当前接口不会形成完整功能。

## 目标边界

### 用户内联图片

本版本支持用户选择文件和直接粘贴正文图片；剪贴板上传是核心行为，不是后续增强。上传结果写成
Plate `img` 节点；`rawBody` 是可编辑权威结构，`body` 是服务端消毒后的 HTML。它不转换成
`Entry.thumbnails`。

支持 JPEG、PNG、GIF 和 WebP。根据实际字节识别类型并完成图片解码后才允许持久化：

- SVG 不进入第一阶段。它可以包含脚本、外链和复杂解析行为，不能仅按 `image/svg+xml` 当普通图片；
- TIFF、BMP、PDF、视频和未知格式拒绝；
- multipart 声明只作提示，不能覆盖字节检测结果；
- 继续保留 20 MiB HTTP 请求硬上限，同时增加图片像素上限，防止小压缩包展开为巨大内存；
- 一次请求只接受一张图片，不在 handler 内实现批量上传。

编辑器展示使用缩略图，点击可打开原图。新 `img` 节点允许增量保存 `url`、`originalUrl`、尺寸和
caption；读取历史仅含 `url` 的节点必须保持兼容。静态 renderer、编辑器 renderer 和 HTML serializer
必须同时覆盖该结构，不能只修其中一层。

### 剪贴板与粘贴 HTML

一次 paste 可能同时携带图片二进制、`text/html` 和 `text/plain`。处理顺序固定为：

1. Clipboard `items/files` 中存在图片二进制时，优先上传二进制，只处理一次，不再重复消费 HTML 中
   对应的 `<img>`；截图、复制本机图片和浏览器提供 image blob 都走这条路径；
2. 没有图片二进制但 HTML 含 `data:image/*` 时，把 data URI 在浏览器端转成 Blob，再走相同上传 API；
   `blob:` URL 也必须先读取为 Blob，不能保存进 Entry；
3. HTML 中的 `http/https` `<img src>` 默认由服务端 mirror，成功后把节点 URL 换成 canonical
   `media_url`；不把新粘贴内容继续保存成外站热链；
4. 其他 scheme、无效 URL 和超过 2048 字节的 URL 不创建图片节点；普通文字和其他安全 HTML 仍按
   Plate 的既有 paste/消毒规则处理。

HTML 必须使用 DOM/Plate 节点解析，不能用正则改写整段 markup。一个 paste 中多张图片按原顺序插入，
以小并发上传；总数和并发数必须有明确上限，避免一次粘贴触发无界内存、网络请求和 R2 PUT。第一版
固定为每次最多 20 张、并发 2；超出部分不上传并向用户显示错误，不能静默生成外链。

远程图片 mirror 使用与 `media.LocalStorage.Fetch` 相同的受控网络边界：只允许公网 HTTP/HTTPS，DNS
解析后拒绝 loopback/private/link-local/CGNAT，每次 redirect 重新验证，限制跳转、总耗时和响应体，
不携带用户 Cookie、Authorization 或 Referer。下载完成后仍须执行与本地图片完全相同的字节类型、解码、
像素和缩略图校验；远端 `Content-Type` 不可信。

编辑器为每个上传保留 pending/error 状态。存在 pending 或失败图片时禁止发布；失败项必须允许重试或
删除，不能把 `data:`、`blob:`、临时 URL 或未完成的外链序列化进 `rawBody/body`。mirror 成功后节点的
`url`/`originalUrl` 指向本地缩略图/原图，来源 URL 可作为非权威 `sourceUrl` 快照保留，但渲染不得再次
依赖它。

兼容边界是“新写入收口、历史读取不破坏”：打开或展示历史 Entry 时绝不因为看到外部 `<img>` 就触发
网络 mirror；历史外链继续按现状渲染。只有用户明确新粘贴/插入的图片进入 mirror 管线，避免普通 Feed
读取产生写入、SSRF 面或大规模后台抓取。

### 聚合来源缩略图

`Entry.thumbnails` 继续表示外部 Service/Archive 提供的媒体预览。`mirrorMedia` 在 `PutEntry` 前同步
完成 Fetch、Post、URL 改写并随 Entry 持久化的既有契约不变。用户图片上传不得伪造为 thumbnail，
否则编辑器结构、镜像生命周期和来源 metadata 会再次混在一起。

### 文件附件

`Entry.files` 是文件附件的持久化权威，本版本一并恢复上传和展示。它与正文图片是两条入口：图片写
Plate `img`，附件写 `Entry.files`，两者不能根据扩展名在发帖后互相转换。

附件上传使用 `POST /a/upload_file`，每次一个 multipart `file`。由于新帖上传时最终 Entry UUID 尚未
产生，成功响应返回一个有时效的签名 asset token，而不是让客户端自行构造 `pb.File`。token 绑定：

- 上传用户 UUID；
- 内容 digest/object key；
- 安全化后的展示名、服务端检测类型和字节数；
- 签发与过期时间。

token 使用服务端 HMAC 防篡改，不包含凭据；默认 24 小时过期。`POST /a/share` 增加附件 token 列表，
在当前用户、签名、期限、数量和总大小全部通过后才生成 `Entry.files`。同一 token 可安全重试发帖，
同一 Entry 内按 digest 去重。编辑 Entry 时，服务端只允许保留该 Entry 已有文件、移除已有文件，或增加
当前用户的新 token；客户端不能提交任意外部 `File.url`。

第一版每个 Entry 最多 10 个附件、合计不超过 100 MiB，每个文件最多 20 MiB；multipart request 上限
应给 framing 留出固定余量，而不是让“20 MiB 请求上限”实际拒绝略小于 20 MiB 的文件。空文件拒绝。
展示名只取客户端 basename，去掉控制字符和路径，限制为 255 UTF-8 字节；原始名称永不进入 object key。

附件允许 HTML、SVG、音频和视频；“允许上传”只表示可以经强制下载路由取回，不表示浏览器可以 inline
执行或播放。首版只接受以下七类 allowlist，不能归入清单的文件一律拒绝：

- 图片：JPEG、PNG、GIF、WebP、SVG；
- 文档与文本：PDF、TXT、Markdown、CSV、JSON、XML、HTML、RTF；
- Office/OpenDocument：DOC/DOCX、XLS/XLSX、PPT/PPTX、ODT/ODS/ODP；
- 归档：ZIP、7z、RAR、TAR、GZIP、BZIP2、XZ；
- 音频：MP3、M4A/AAC、OGG/Opus、FLAC、WAV、WMA；
- 视频：MP4/M4V、WebM、MOV、OGV、MPEG、MKV、AVI、WMV、3GP；
- 电子书：EPUB、MOBI/AZW。

HTML/SVG 等主动内容即使 MIME 合法也只能作为附件下载；正文图片接口仍拒绝 SVG。文件下载
始终使用 `application/octet-stream`，不能因 allowlist 类型改成 inline。明确拒绝原生可执行程序、动态
库、脚本安装包、系统安装镜像及无法归类的二进制，例如 EXE/DLL/MSI、Mach-O、ELF、APK、DMG、ISO。
源码、脚本、日志、字幕、日历、联系人、字体、磁盘镜像及其他未明确列出的格式均拒绝；不能因为内容
可按文本读取就自动归入“文档”。压缩包内部不做递归解包或病毒扫描，UI 必须把附件标为用户提供的
下载内容，不能暗示文件安全。

类型判断以 magic/sniffed MIME 为主，安全化扩展名为辅助；容器格式（OOXML/ODF/EPUB）需要验证 ZIP
结构中的格式标识，不能仅凭 `.docx`/`.epub` 后缀放行。纯文本类需验证不是带 NUL 的未知二进制。以后
扩大 allowlist 必须补类型探测和下载测试，不能退化为相信 multipart MIME 或扩展名。

当前单文件 20 MiB 上限同样适用于音视频，因此首版只支持短音频和小视频附件。扩大上限前必须先把
上传/R2 PUT 从完整 `[]byte` 内存模型改为有界流式或临时文件模型，不能直接把 20 MiB 常量放大。

Feed、permalink、SSR 和 React 使用同一附件列表语义：在正文下方显示文件图标、安全名称、格式和
大小，不做 inline preview。普通粘贴的 `<a href>` 仍是链接，不自动下载或 mirror；剪贴板/拖放中的
非图片 File 才进入附件上传。图片 File 默认进入正文图片，用户通过“附件”按钮选择图片时才作为附件。

附件 URL 固定为应用下载路由：

```text
/e/:entry_uuid/files/:sha256/:escaped_name
```

handler 必须先用当前 viewer 读取 Entry 并执行与 permalink 相同的可见性检查，再确认 digest/name 确实
出现在该 Entry 的 `files` 中。未授权返回 403，不存在返回 404。响应支持 HEAD、Range、Content-Length，
并强制：

```text
Content-Disposition: attachment; filename*=UTF-8''<encoded-name>
Content-Type: application/octet-stream
X-Content-Type-Options: nosniff
```

即使文件声明为 PDF/图片也不在下载路由 inline 展示。对象内容从本地镜像流式读取，不把整文件再次载入
内存；local 是运行时读取副本，R2 是同 key 的远端副本/恢复来源。`Entry.files[].url` 保存上述稳定应用
URL，`type/name/size` 保存服务端确认的 metadata，`icon` 保持历史兼容但新写入为空。

历史 `Entry.files` 的外部 URL 继续兼容显示为外部下载链接，不在读路径自动 mirror；新写入只允许应用
下载 URL。删除 Entry 或移除附件不直接删除内容寻址对象，统一遵循下文的延迟回收规则。

Profile/Group picture 当前仍是 HTTPS URL 字段。以后可以复用图片上传结果填入该 URL，但不能另外复制
一套 avatar 上传和存储实现。

## 存储与 URL 契约

- 新上传图片、文件与 archive mirror 一样使用 `media.NewStorage`；完整 R2 配置时 local + R2 双写，零 R2
  配置时明确 local-only，部分配置继续 fail loud。
- 对象仍按完整内容 SHA-256 确定性寻址并分片，不能重新引入原始文件名目录。相同内容幂等复用。
- 原图和缩略图都是独立的完整内容寻址对象。缩略图必须先在内存或临时文件中完成编码，再通过
  Storage 发布；不能在最终路径原地写。
- 图片 API 返回 `media_url` 下的绝对公开 URL，不返回 ffweb 的 `/file` 路径。附件保存带 Entry 权限
  检查的应用下载 URL。两者都不保存 R2 API endpoint 或本地路径。
- `media.Object.Bucket`、`Object.Url` 和 `Storage.Fetch/Post` 契约保持不变。
- media origin 必须返回准确的图片 Content-Type、长期 immutable cache header 和
  `X-Content-Type-Options: nosniff`。在无扩展名内容 key 下不能依赖 nginx 按文件名猜类型；部署方案必须
  在启用新上传前证明 local 与 R2 两条 serving 路径行为一致。

## API 与失败语义

保留现有认证路由 `POST /a/upload`，但把它明确收窄为单图片 multipart API，字段名仍为 `file`，避免
制造第二条上传路径。剪贴板二进制、data URI 和文件选择最终都调用它。远程 HTML 图片使用同一图片
处理 primitive，但通过单独的 `POST /a/upload/mirror` 接收一个 `sourceUrl`，避免让 multipart API
出现互斥参数和含混语义。两个入口返回相同结构。成功响应至少包含：

```json
{
  "url": "https://media.example/<original-key>",
  "thumbUrl": "https://media.example/<thumbnail-key>",
  "width": 1600,
  "height": 900,
  "mimeType": "image/jpeg",
  "size": 123456
}
```

状态码固定为：

- `400`：multipart 缺失、格式不支持、图片损坏或尺寸非法；
- `401`：未登录；
- `413`：请求体或文件超过上限；
- `429`：命中上传并发/速率保护；
- `500`：本地编码或持久化错误；
- `502/503`：配置完整但 R2 写入失败或暂不可用。

错误响应不回显路径、凭据或完整上游响应。只有原图和缩略图都发布成功后才返回 200。由于上传先于
Entry 提交，上传成功、发帖失败仍可能产生孤儿对象；这不是 Entry 事务的一部分。

`/a/upload/mirror` 还必须把 URL 解析/协议错误映射为 `400`，受控下载超时映射为 `504`，远端拒绝、
非 2xx、响应超限或内容无效映射为 `422`；不能把内部 DNS/IP 判定细节返回给浏览器。

`POST /a/upload_file` 使用同一认证、容量、并发和存储错误规则，但成功响应返回 `{assetToken, name,
mimeType, size}`，不返回可绕过 Entry 权限的 media URL。token 仅用于 `/a/share`，不能作为下载凭据。

## 生命周期与回收

内容寻址对象可能被多个 Entry、Profile、Group 或历史迁移记录共享，因此删除 Entry 时不得直接删
media object。第一版接受未引用上传继续保留；回收只能做离线 mark-and-sweep：

1. 流式扫描 Profile picture、Entry body/rawBody、thumbnails 和 files，收集 canonical media key；
2. 与对象清单对比并设置足够长的安全期；
3. dry-run 输出计数与样本；
4. 确认 local/R2 一致后才删除。

在已有可验证工具前，不添加请求路径上的自动删除，也不把 R2 List 引入 ffdb 运行时。

## 实施顺序与验收

1. **锁定残留行为**：测试 `/a/upload` 登录边界、20 MiB 限制，并证明非图片会留下原件的现有缺陷；
   不把错误行为固化为目标契约。
2. **统一图片处理 primitive**：字节识别、decode config、像素上限、缩略图编码；覆盖损坏图片、伪造
   MIME、超大尺寸、透明 PNG、GIF/WebP。
3. **统一存储**：ffweb 改用 `media.NewStorage`，原图/缩略图都走 Storage，返回 canonical media URL；
   验证 local-only、完整 R2 和部分 R2 配置。
4. **恢复编辑器入口**：实现剪贴板优先级，覆盖图片 Blob、data URI、HTML 单图/多图和文件选择；远程
   HTML 图片默认 mirror。pending/失败阻止发布，成功后插入 `img` 节点。同步静态渲染、HTML 序列化、
   编辑回读和 URL 安全测试。
5. **实现文件附件**：上传生成签名 token，发帖/编辑绑定到 `Entry.files`，SSR/React 渲染一致；下载
   handler 覆盖 public/private、403/404、HEAD/Range、文件名编码和强制 attachment。历史外部 File 保持
   可读，新写入不能伪造 URL。
6. **部署验证**：nginx/R2 对同一图片对象返回正确 Content-Type、`nosniff` 和 immutable cache；检查 CSP、
   CDN 与最大请求体限制一致。
7. **运维观测**：只记录结果、字节数、尺寸、耗时和错误类别，不记录图片/文件内容、session、原始本地路径
   或凭据。

验收时必须证明：未登录不能上传；非图片在任何存储都不落对象；相同图片字节得到确定且幂等的 URL；R2 失败不
返回成功 URL；剪贴板 binary 优先且不会与 HTML 重复上传；data/blob URL 不进入 Entry；粘贴远程 HTML
图片后只保存 canonical media URL；失败和未完成上传不能发布；粘贴、选择、发帖、编辑和 SSR/React
展示一致；附件 token 不能跨用户使用或篡改；私有 Feed 附件不能绕过 403；附件始终下载而不 inline；
取消编辑只产生可识别的延迟回收对象；历史 `img`、`thumbnails` 和 `files` 仍可读取。
