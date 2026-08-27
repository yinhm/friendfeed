# 图片上传与文件附件

本文固定用户上传、历史媒体和服务聚合媒体的边界。目标是恢复正文图片与文件附件能力，同时保持当前项目
简单的单机/本地媒体架构：用户上传先进入 temporary staging，只有实际发布 Entry 时才进入 canonical
LocalStorage；R2 是异步副本，不进入用户发布成功的同步关键路径。

本版本明确采用 **URL capability** 模型：媒体和附件 URL 本身不做 Feed/Entry 权限校验。拿到正确 URL
即可访问对应对象。private Feed/Group 的媒体不额外鉴权；这一选择已记录到 `docs/open_decisions.md`，
未来如需 signed URL 或 Entry-aware download route 再单独设计。

## 现状

仓库同时存在三种媒体表达：

| 表达 | 权威位置 | 当前用途 | 运行时状态 |
| --- | --- | --- | --- |
| Plate `img` 节点 | `Entry.rawBody`，并序列化进 `Entry.body` | 用户正文内联图片 | 可读取、可编辑，但没有插入/上传入口 |
| `Entry.thumbnails` | Entry protobuf | RSS/Twitter/Archive 等来源附带的预览图 | 可镜像、可渲染；不作为用户上传格式 |
| `Entry.files` | Entry protobuf | FriendFeed 历史文件附件 | 迁移和镜像仍保留；Web 页面没有上传或展示入口 |

`POST /a/upload` 是旧 Plate 图片上传的残留后端。当前 handler：

1. 登录后接收单个 multipart `file`；
2. 使用 20 MiB request 上限并把文件完整读入内存；
3. 先写本地 `media_path`，再尝试按图片解码/生成缩略图；
4. 返回同站 `/file/<path>`；
5. 不修改 Entry。

现状缺口：

- 信任 multipart Content-Type，没有以实际字节 + decode 结果确定类型；
- 非图片可能先落盘再因为 thumbnail 失败留下对象；
- 没有像素上限；
- ffweb 直接写 canonical LocalStorage，取消编辑会形成长期 orphan；
- 用户上传路径和 archive/RSS mirror 的职责混在一起；
- thumbnail 写最终路径的方式不适合作为新的上传 primitive；
- `Entry.files` 没有新的 Web upload/bind/render 流程。

---

# 1. 权责边界

## 1.1 ffweb：用户上传、staging 与 publish owner

用户主动上传属于 Web/BFF transport 能力，由 ffweb 负责：

- `POST /a/upload`：正文图片（本地 `file` 或 remote `sourceUrl`）；
- `POST /a/upload_file`：文件附件；
- multipart/request limit；
- 实际字节类型识别；
- 图片 decode、像素检查、thumbnail；
- 文件 allowlist；
- temporary staging；
- staging 状态 / asset token；
- 上传并发限制；
- 发布时把 staging object promote 到 canonical LocalStorage；
- 把 Plate 中 staging URL 改写为 canonical URL；
- 构造最终 `Entry.files`；
- 删除/过期 temporary objects。

staging 位于 `media_path/upload-staging/`，并通过当前 media serving 路径提供临时可访问 URL。
Plate 在编辑状态直接使用 staging 图片 URL 展示预览；staging URL 是 temporary capability URL，
24 小时后可以失效。staging 阶段不写 R2。

## 1.2 ffdb：Entry 与后端媒体职责

ffdb 继续负责：

- `Entry` / `Entry.files` 的领域持久化；
- Entry mutation 的现有 author/feed/group authorization；
- 保持现有 `PostEntry(Entry)` RPC；
- 在 Entry 成功持久化后，从最终 Entry 中识别本站 canonical user-upload media refs；
- R2 已启用时，由 ffdb 自己 enqueue 并执行 `media.mirror_r2` Task；
- archive / RSS / Service 等服务器侧来源需要抓取远程媒体时，继续使用现有受控 `media.Storage`；
- 已有 `mirrorMedia` 兼容契约保持不变，本项不顺带重写 ArchiveFeed。

ffdb **不负责浏览器 upload validation，也不解析 asset token/staging state**。这些状态只属于 ffweb。
ffdb 收到的是已经由 ffweb 完成 promote、URL rewrite、`Entry.files` 构造后的 canonical Entry。

`pb.Entry.Files` 在 ffdb 仍按 Entry 数据持久化；本版本不为用户上传建立第二套持久化 asset metadata 表。

## 1.3 media package

`media` 包提供可复用 primitive：

- safe remote fetch；
- content digest；
- canonical key；
- atomic local publish；
- image decode/thumbnail helper；
- 从 canonical media URL 还原 local object key；
- R2 PUT primitive。

新的用户上传路径应复用 primitive，但不能直接调用当前“同步 local + R2 dual-write”的
`MirrorStorage.Post` 作为用户请求关键路径。

## 1.4 LocalStorage 与 R2

对用户上传：

~~~text
upload
  |
  v
staging (public temporary URL)
  |
  | user publishes
  v
ffweb promote + rewrite
  |
  v
canonical LocalStorage
  |
  v
ffdb PostEntry(Entry)
  |
  | after Entry persistence, if R2 enabled
  v
media.mirror_r2 Task
  |
  v
R2 replica
~~~

LocalStorage 是发布成功时必须已经存在的运行时副本；R2 是异步 replica。

当前 `m.friendfeed.me` nginx 本来就以 `media_path` 为 root，并与 `media_url` 使用同一公开 URL
语义，因此 canonical local object 发布后无需等待 R2 Task，页面即可正常读取。

ffdb 不解析 Plate `rawBody`，也不解析 `Entry.body`。任何需要理解正文/Plate 的工作都由调用方在
`PostEntry` 前完成。用户上传媒体以结构化字段交给 ffdb：

- 图片：`Entry.thumbnails[]`；
- 文件附件：`Entry.files[]`。

这样保持现有 `PostEntry(Entry)` RPC，同时 ffdb 只处理自己已有的 protobuf 结构，不理解编辑器内容。

# 2. 当前访问模型：不做媒体权限控制

本版本明确不实现：

- private Feed 图片鉴权；
- private Group 图片鉴权；
- private Entry 附件鉴权；
- signed media URL；
- attachment URL 与 Entry visibility 的二次检查。

规则固定为：

> 能拿到正确 canonical media/file URL 的调用方，即被视为有权读取该对象。

这包括 private Feed/Group 的正文图片和附件。

该 URL **不是密码学授权凭据**；内容 SHA-256 也不被视为 secret。这里是当前产品/架构主动接受的简化，
而不是依靠“hash 猜不到”实现访问控制。

未来如修改这一点，需要整体评估 media origin、缓存、历史 Entry URL、R2、附件下载和 private Feed
兼容性，不能在单个 handler 中局部加鉴权。

---

# 3. 用户正文图片

## 3.1 支持格式

正文图片 V1：

- JPEG；
- PNG；
- GIF；
- WebP。

明确拒绝：

- SVG；
- TIFF；
- BMP；
- PDF；
- 视频；
- 未知格式。

multipart 声明的 MIME 只能作提示，最终类型必须由实际字节识别并完成图片 decode 后确认。

## 3.2 图片资源限制

固定限制：

- 单文件最大：20 MiB；
- HTTP multipart request 最大：21 MiB，为 framing 留约 1 MiB 余量；
- 最大单边：16,384 px；
- 最大总像素：50,000,000；
- 同一用户同时上传请求：最多 2；
- ffweb 全进程同时执行 image decode/resize：最多 2；
- ffweb 全进程同时处理 upload request：最多 8；
- staging TTL：24 小时。

超过 request/file 大小返回 `413`；命中并发限制返回 `429`。

图片先用 decode-config/等价轻量方式读取尺寸，在进入完整像素 decode 前执行 side/pixel limit。不能先分配
完整超大像素缓冲后再判断限制。

## 3.3 Thumbnail 生成与 Entry.thumbnails

用户上传图片严格使用 `Entry.thumbnails[]` 管理，**不同时写入 `Entry.files[]`**。

V1 数据职责：

- `Entry.thumbnails[]`：图片；
- `Entry.files[]`：非图片文件附件。

因此 `POST /a/upload_file` 遇到 JPEG/PNG/GIF/WebP 应拒绝或引导走图片上传流程，避免同一媒体在
两个结构中出现两份权威引用。

Thumbnail 由 ffweb 在 upload/staging 阶段生成；ffdb 不负责 resize/decode。

生成规则：

- 目标最大宽度：1024 px；
- 原图宽度 ≤ 1024 px：不生成第二份，`thumb == original`；
- 原图宽度 > 1024 px：按比例缩到 1024 px；
- 不放大小图；
- GIF 动图 thumbnail 可以使用静态首帧；
- WebP V1 只支持静态 WebP；animated WebP 拒绝；
- JPEG thumbnail 使用固定 quality；
- 透明 PNG 优先生成 PNG thumbnail，避免透明背景在 JPEG 中产生不确定结果。

upload 成功后 staging 同时存在 original 与 thumbnail；无需缩放时两者是同一个 staging object。

Plate 编辑态可以：

~~~text
url         = staging thumbnail URL
originalUrl = staging original URL
~~~

这样正文编辑时优先加载较小的 thumbnail；需要查看原图时使用 originalUrl。

### 3.3.1 Entry.Thumbnails 结构

发布时 ffweb 根据最终 Plate 图片节点构造 `Entry.thumbnails[]`，不让 ffdb 解析 body。

固定语义：

~~~text
Thumbnail.url    = thumbnail URL
Thumbnail.link   = original image URL
Thumbnail.width  = thumbnail intrinsic width
Thumbnail.height = thumbnail intrinsic height
~~~

如果无需单独 thumbnail：

~~~text
Thumbnail.url  = original URL
Thumbnail.link = original URL
~~~

`Thumbnail.width/height` 是生成后图片的 intrinsic size，不是 Plate 中用户拖拽得到的展示宽度。

本文只定义媒体数据、持久化和 mirror contract；`Entry.thumbnails[]` / `Entry.files[]` 的具体 UI
展示方式不属于本文档范围，也不为展示需求增加额外 protobuf 字段。

### 3.3.2 canonical object key

original、thumbnail、file 都使用同一套 `media_path` content-addressed key 规则，不按类型建立目录
namespace。

例如：

~~~text
a/b/<remaining-sha256>.jpg
c/d/<remaining-sha256>.png
e/f/<remaining-sha256>.xlsx
~~~

每个对象按自己的真实 bytes 计算完整 SHA-256，并加 server-derived extension。

因此：

- original 和 thumbnail 内容不同 → 各自有自己的 canonical key；
- `thumb == original` → 只有一个 canonical object；
- 图片/thumbnail/file 的类型来自 Entry 中的结构化引用和 server-verified metadata，不来自目录名。


---

# 4. Temporary staging

## 4.1 路径与 serving

staging 属于媒体文件体系，不属于数据库文件体系。默认目录固定为：

~~~text
<media_path>/upload-staging/
~~~

`media_path` 与 `db_path` 是两个独立配置边界：

- `db_path`：Pebble / ffdb 数据；
- `media_path`：本地媒体对象与 upload staging。

upload staging 的路径只能从 resolved `media_path` 派生，不从 `db_path` 派生。

staging 是可访问的临时媒体区：

~~~text
<media_url>/upload-staging/<upload-id>.jpg
~~~

- production 由 nginx 直接从 `media_path` serve；
- dev/local 可由 Gin local-media route serve；
- Plate 图片上传成功后直接使用 staging URL 展示；
- 不要求 staging URL 长期稳定；
- 不开启 directory listing；
- 文件名使用高熵随机 upload ID；
- 当前仍采用 URL capability 语义，不增加 staging 鉴权层。

文件名只使用随机 upload ID + server-derived extension，例如：

~~~text
<upload-id>.jpg
<upload-id>.png
<upload-id>.pdf
~~~

原始客户端 filename 永不成为 staging 或 canonical filesystem path。

## 4.2 server-derived extension

extension 只由服务端确认后的真实类型产生。

正文图片：

~~~text
image/jpeg -> .jpg
image/png  -> .png
image/gif  -> .gif
image/webp -> .webp
~~~

附件按 V1 allowlist 使用对应的 canonical extension。

## 4.3 staging state / asset token

上传成功后，ffweb 使用无状态 HMAC asset token 保存发布所需 staging state，使 `/a/share` 能确认
该对象确实由当前用户通过 upload endpoint 创建，而不是客户端任意拼接 staging path。

token 表示一次逻辑 upload asset，至少绑定：

- upload user UUID；
- upload ID；
- kind：`image` / `file`；
- issued-at / expires-at；
- sanitized display name（file）；
- image width/height（image）；
- 一个或多个 staging objects。

每个 staging object 至少绑定：

~~~text
object_id
sha256
server_derived_extension
verified_mime
bytes
role: original | thumbnail | file
~~~

image asset 通常包含 original + thumbnail 两个 object；若无需单独 thumbnail，可以只包含一个 object，
同时承担 original/thumbnail 两个角色。file asset 只包含一个 file object。

默认 24 小时过期。

### HMAC key

asset token 直接复用 ffweb 已有的 `SecretKey`（启动参数 `-s`）作为 HMAC secret，不新增新的配置项。
当前该 secret 已用于 ffweb session/OAuth cookie store；asset token 只是新增一个签名用途。

V1 要求：

- token 使用 HMAC-SHA256；
- payload 使用稳定版本号，例如 `v=1`；
- non-debug deployment 必须显式配置 `-s`，不能依赖源码里的默认示例 secret；
- non-debug 启动时如果检测到仍使用默认示例 secret，ffweb 必须 fail loud 并拒绝启动；
- token 比较使用 constant-time compare；
- token/staging state 不作为下载权限；
- token 不持久化进 Entry；
- token 不写日志。

asset token 的职责只是：

> 允许当前用户把一个已经通过 ffweb 验证过的 staging object promote 成 canonical Entry media。

它不是媒体读取权限；staging URL 仍采用当前 URL capability 语义。

## 4.4 cleanup

ffweb：

- 启动时清理超期 staging；
- 运行中每小时扫描一次；
- 删除已超过 24 小时的 staging object；
- 清理失败只记录安全错误类别，不终止服务。

取消编辑、关闭页面、上传后不发布，都只留下有 TTL 的 staging object，不进入 canonical storage。

# 5. Canonical LocalStorage

## 5.1 promote 放在 ffweb

本版本固定：

> staging -> canonical LocalStorage 的 promote 由 ffweb 在调用 `PostEntry` **之前**完成。

ffweb 已经掌握 staging state / asset token 和 Plate/附件编辑状态，因此由它决定“本次保存实际仍引用哪些
staging objects”最简单。ffdb 不需要知道 upload ID、asset token 或 staging URL，现有
`PostEntry(Entry)` RPC 保持不变。

代价是：

~~~text
promote success
  ->
PostEntry fails
  ->
canonical local orphan
~~~

V1 接受这个小窗口，未来离线 mark-and-sweep；不为此建立 filesystem + Pebble 跨系统事务。

## 5.2 新建 Entry 的 promote

新建 Entry 时，ffweb：

1. 从最终 Plate/附件编辑态找出仍被引用的 staging objects；
2. 校验当前 session user、expiry、size、SHA-256；
3. 只 promote **仍被引用**的 staging original / thumbnail / files；
4. 使用同一 `media_path` filesystem 内的 atomic publish；
5. canonical object 已存在时幂等复用；
6. 把 staging image URL 改写成 canonical URL；
7. 构造最终 `Entry.thumbnails[]`；
8. 构造最终 `Entry.files[]`；
9. 重新序列化最终 `rawBody/body`；
10. 调用现有 `PostEntry(Entry)`。

最终传给 ffdb 的 Entry 不得包含 `upload-staging/` URL。

## 5.3 编辑 Entry 的 promote

编辑保存时必须区分三类对象：

### A. 旧 Entry 已有 canonical object，编辑后仍保留

~~~text
old canonical ref
    ↓
new Entry still references it
~~~

行为：

- 不重新 promote；
- 不重新生成 thumbnail；
- URL 保持不变；
- 不因为编辑 caption、文字、Plate 展示宽度而改 media object。

### B. 本次编辑中新上传，并且保存时仍被引用

~~~text
new staging object
    ↓
still present when Save
    ↓
promote to canonical
~~~

行为与新建 Entry 相同：promote original/thumbnail/file，改写 URL，再构造最终
`thumbnails[]/files[]`。

### C. 本次编辑中上传过，但保存前又删除

~~~text
upload staging
    ↓
user removes it before Save
~~~

不 promote；留在 staging，等待 24h TTL cleanup。

### D. 旧 Entry 中的 canonical media 被用户删除

从新的 `Entry.thumbnails[]/files[]` 中移除引用，但：

- 不同步删除 LocalStorage；
- 不同步删除 R2；
- 未来由离线 GC 处理。

因此 ffweb 的 promote 只处理“当前保存动作中新引入的 staging refs”，不是每次编辑把整篇 Entry 的媒体重新
落盘一遍。

## 5.4 canonical key

所有 canonical media 共用同一 content-addressed key scheme：

~~~text
<first-hex>/<second-hex>/<remaining-sha256>.<server-derived-ext>
~~~

不按 image/thumbnail/file 再分 namespace。

同 bytes + 同 verified type 得到同 canonical object。thumbnail 如果和 original 是同一对象，直接复用。

## 5.5 Content-Type metadata

V1 不新增本地 sidecar metadata 数据库。

- 图片/thumbnail 的可信 MIME 可从 server-derived extension/生成结果恢复；
- `Entry.files[].type` 保存服务端确认的文件 MIME；
- R2 task payload 可携带最终 verified MIME；
- attachment 下载使用 `Content-Disposition: attachment`，不依赖 storage path namespace。

# 6. 编辑器与剪贴板

一次 paste 可能同时有 image binary、`text/html` 和 `text/plain`。顺序：

1. Clipboard `items/files` 有图片 binary 时优先，只消费一次；
2. 没 binary、HTML 中有 `data:image/*` 时，浏览器转 Blob 后走 `POST /a/upload`；
3. `blob:` 只能转 Blob 后上传，不能持久化；
4. HTML 中 `http/https` 图片通过 `POST /a/upload` 的 `sourceUrl` 分支拉取到 staging；
5. 其他 scheme、非法 URL、URL > 2048 bytes 不创建新图片节点。

一个 paste：

- 最多 20 张图；
- browser upload concurrency = 2；
- server 仍执行第 3.2 节自己的并发/容量限制。

HTML 必须经 DOM/Plate node 解析，不能用 regex 改写整段 HTML。

React editor 可在未提交阶段保留：

- staging URL；
- asset token / upload handle；
- pending/error state。

图片上传成功后直接显示 staging URL。发布时 ffweb promote 并把 staging URL 改写成 canonical URL；
staging URL 和 asset token 都不是最终 Entry 格式。

发布前必须保证所有用户新增图片已经 upload 成功；pending/error 时禁止提交。ffweb 在调用
`PostEntry` 之前完成 promote + rewrite。最终写入 `rawBody/body` 的只能是 canonical media URL，
不得包含：

- `assetToken`；
- `data:`；
- `blob:`；
- temporary staging path；
- remote upload source URL。

历史 Entry 中已经存在的外部 image URL 继续兼容读取，不在 read path 自动 mirror。

---

# 7. Remote image upload

remote image 不设独立 endpoint，统一走 `/a/upload` 的 `sourceUrl` 分支：

~~~text
POST /a/upload
~~~

请求必须二选一：

~~~text
file      = multipart binary
sourceUrl = http/https URL
~~~

即 `file XOR sourceUrl`：必须且只能提供一个。

两种输入最终进入同一个 image staging pipeline：

~~~text
local file -----------┐
                      ├-> sniff -> decode config -> limits -> decode -> thumbnail -> staging
remote sourceUrl -----┘
~~~

## 7.1 remote fetch 边界

`sourceUrl` 分支复用现有 media safe-fetch 的网络安全能力，但**不直接原样复用当前
`LocalStorage.Fetch()` 的全部行为**。

必须复用/保持：

- 仅 HTTP/HTTPS；
- DNS 解析后拒绝 loopback/private/link-local/CGNAT；
- redirect 每跳重新验证；
- redirect 数有界；
- 总 timeout 有界；
- 不发送用户 Cookie；
- 不发送 Authorization；
- 不发送 Referer；
- remote Content-Type 不可信。

用户 remote upload 的 response body 上限固定为：

~~~text
20 MiB
~~~

与本地图片单文件上限一致。读取时应使用 `20 MiB + 1` 的 bounded reader，超过立即拒绝，不沿用 archive
路径当前的 32 MiB `maxFetchBytes`。

## 7.2 sourceUrl 生命周期

`sourceUrl` 只存在于当前 `/a/upload` request 的内存上下文。

成功进入 staging 后立即丢弃：

- 不写 `rawBody`；
- 不写 `Entry.body`；
- 不写 protobuf；
- 不写 staging metadata；
- 不写 asset token；
- 不作为 renderer fallback。

## 7.3 URL 与错误脱敏

当前 `media.LocalStorage.Fetch()` 的部分错误文本会包含完整 URL，因此用户 `sourceUrl` 分支不能把
其 raw error 直接返回浏览器或写入日志。

尤其 query / fragment 可能携带 signed token。日志和浏览器错误中不得出现：

~~~text
?token=...
?signature=...
?X-Amz-...
#...
~~~

实现可以继续复用 safe HTTP client / DialContext，但必须在 upload handler/adapter 层把 fetch 错误映射为
安全错误类别。运行日志最多记录安全化 hostname、结果类别、bytes、耗时。

错误映射：

- URL 解析/协议错误：`400`；
- remote 非 2xx、响应超 20 MiB、内容非允许图片：`422`；
- timeout：`504`；
- 本地 staging/encode 失败：`500`。

内部 DNS/IP 拒绝细节不返回浏览器。

# 8. 文件附件

## 8.1 V1 allowlist

第一版收紧为：

图片不进入 `Entry.files[]`。JPEG/PNG/GIF/WebP 必须走图片上传并进入 `Entry.thumbnails[]`。

### 文档与文本

- PDF；
- TXT；
- Markdown；
- CSV；
- JSON；
- HTML。

### Office

- DOC；
- DOCX；
- XLS；
- XLSX。

### 归档

- ZIP。

### 音频

- MP3。

其他格式默认拒绝，包括 SVG、XML、RTF、PPT/PPTX、OpenDocument、7z/RAR、视频、可执行程序、
安装包、磁盘镜像、字体等。以后按真实需求逐项增加并补类型检测测试。

HTML 即使允许上传，也只作为附件下载，不作为正文 HTML 执行。

## 8.2 类型判断

不能只信 multipart MIME 或 extension。

至少要求：

- PDF：magic；
- DOC/XLS legacy：OLE Compound File magic + extension/type 一致；
- DOCX/XLSX：ZIP container + Office 内容标识；
- ZIP：ZIP container；
- MP3：ID3 或有效 MPEG audio frame；
- TXT/Markdown/CSV/JSON/HTML：文本校验，无 NUL；JSON 额外可 parse；
- HTML 只作为 attachment type，不进入正文 sanitizer bypass。

客户端 extension 仅用于帮助区分本来共享容器格式的类型，最终 canonical extension 由 server 生成。

## 8.3 文件限制

- 单文件 ≤ 20 MiB；
- 单 Entry ≤ 10 个附件；
- 单 Entry 附件总大小 ≤ 100 MiB；
- 空文件拒绝；
- display name 取 basename、去控制字符、限制 255 UTF-8 bytes；
- 原始名称不进入 object key。

上传和 staging 使用第 3/4 节相同的并发限制和 TTL。

## 8.4 Entry.files

`POST /a/upload_file` 返回：

~~~json
{
  "assetToken": "...",
  "name": "report.xlsx",
  "mimeType": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
  "size": 123456
}
~~~

不允许客户端直接提交 arbitrary `pb.File.url` 作为“新上传附件”。

`/a/share` 验证 token 并 promote 后构造：

- `File.url`：公开、稳定的下载 URL；
- `File.type`：服务端确认 MIME；
- `File.name`：sanitized display name；
- `File.size`：服务端确认 size；
- `File.icon`：新写入为空，保留历史 wire compatibility。

ffdb 只持久化这些 Entry 数据，不重新实现用户 upload/token protocol。

## 8.5 /a/share 的 asset 提交契约

现有 `/a/share` multipart form 保留：

~~~text
id
feedUuid
body
rawBody
~~~

并新增一个可选字段：

~~~text
assets
~~~

`assets` 是 JSON string array，只包含本次编辑器当前仍持有的 asset tokens，例如：

~~~json
[
  "<image-asset-token>",
  "<file-asset-token>"
]
~~~

upload ID、digest、MIME、original/thumbnail staging object ID 等都已经由 token 签名，不在 form 中重复
提交为可信字段。

ffweb 处理顺序：

1. parse `assets`；
2. verify 每个 token HMAC、版本、当前 user、expiry；
3. 得到 `uploadID -> verified asset` map；
4. 对 image asset，由 ffweb 解析当前 Plate，只有最终仍出现对应 staging URL 的 token 才视为被引用；
5. 对 file asset，`assets` 数组本身就是本次保存的附件引用声明，不从 `rawBody/body` 反向寻找文件引用；
6. 每一个最终被引用的 staging object 都必须能对应到一个 verified asset；
7. image token 若已不再被最终 Plate 引用则忽略，不 promote；
8. promote 被引用的 original/thumbnail/files；
9. 将 image staging URL 改写为 canonical URL；
10. 构造 `Entry.thumbnails[]` / `Entry.files[]`；
11. 最后调用现有 `PostEntry(Entry)`。

编辑旧 Entry 时，已有 canonical media 不需要 token；只有本次新增的 staging media 需要 token。

asset token 列表只是 ffweb 的 publish/staging protocol，不进入 ffdb RPC，也不持久化进 Entry。

## 8.6 下载 URL

当前不做授权下载。

新用户图片、thumbnail 和附件的 canonical URL 都统一由 `media_url + canonical key` 形成，不使用类型
namespace：

~~~text
<media_url>/a/b/<remaining-sha256>.jpg
<media_url>/c/d/<remaining-sha256>.xlsx
~~~

新上传 canonical media 的目标 serving path 是独立 `media_url`，production 由 nginx 直接从
`media_path` serve；dev/local 可以由 ffweb Gin local-media route serve。当前 ffweb 仍无条件注册
`/file -> media_path`，本项目是否移除该 production fallback 不属于本 spec；本版本也不处理历史
`/file/*` URL 的迁移。

实现必须满足：

- URL 不含 staging ID/token；
- URL 可由 canonical key 确定；
- 不检查 Feed/Entry visibility；
- HTML 等主动内容不能以 inline 页面执行。

附件 response 强制：

~~~text
Content-Type: application/octet-stream
Content-Disposition: attachment
X-Content-Type-Options: nosniff
~~~

display filename 可以从 `Entry.files.name` / URL 参数提供，但不能参与真实 filesystem object lookup。

由于 LocalStorage 不按类型分目录，文件下载语义不能依赖 object path namespace。V1 的 `File.url` 应使用
同一个 canonical object URL 并带显式 download 语义，例如 query/下载路由，使 production nginx 和 dev Gin
都能返回：

~~~text
Content-Disposition: attachment
X-Content-Type-Options: nosniff
~~~

实现不应从路径目录名判断“这是文件还是图片”。当前 production serving source 是 nginx + local
`media_path`；R2 是异步 replica。

---

# 9. R2 后台 mirror

对 user-upload canonical object，R2 不同步写。保持现有：

~~~protobuf
rpc PostEntry(Entry) returns (Entry)
~~~

ffdb 不解析 `Entry.body` / `Entry.rawBody`，只处理结构化的：

- `Entry.thumbnails[]`；
- `Entry.files[]`。

## 9.1 media refs

对用户图片：

~~~text
Thumbnail.url  = thumbnail
Thumbnail.link = original
~~~

两者都是结构化 media refs。若 `url == link`，集合去重后只算一个对象。

对附件：

~~~text
File.url = canonical file object URL
~~~

图片不进入 `Entry.files[]`。

canonical ref 到 object key 的转换只接受当前 `media_url` 下合法的 content-addressed path；不解析正文，也
不主动抓 arbitrary external URL。

## 9.2 create 与 edit 的 mirror 差集

ffdb 在写 Entry 前已经可以根据 Entry ID 判断 create/edit；edit 时先读取旧 Entry。

定义：

~~~text
oldRefs = collect(oldEntry.thumbnails, oldEntry.files)
newRefs = collect(newEntry.thumbnails, newEntry.files)

addedRefs   = newRefs - oldRefs
keptRefs    = newRefs ∩ oldRefs
removedRefs = oldRefs - newRefs
~~~

### Create

~~~text
oldRefs = empty
addedRefs = all new canonical refs
~~~

R2 开启时，对 `addedRefs` enqueue `media.mirror_r2`。

### Edit

只处理 `addedRefs`：

- 旧图片/文件仍被引用：不重新 enqueue；
- 新增图片：original + thumbnail 都进入 addedRefs；
- 新增文件：File.url 进入 addedRefs；
- 删除旧图片/文件：只从 Entry 移除，不删除 Local/R2；
- 替换图片：新 original/thumbnail mirror，旧对象保持等待未来 GC；
- 新上传内容与已有 canonical object hash 相同：canonical key 相同，差集或 task idempotency 会去重。

Task idempotency key：

~~~text
media-r2:<canonical-key>
~~~

所以即使同一 canonical object 首次出现在另一个 Entry，重复 enqueue 也不会造成重复 R2 object work。

## 9.3 enqueue 时序

~~~text
ffweb
  -> promote only newly staged refs still used by this Save
  -> construct final Thumbnails[] + Files[]
  -> PostEntry(Entry)

ffdb PostEntry
  -> load old Entry when editing
  -> collect old structured refs
  -> authorize / persist new Entry
  -> collect new structured refs
  -> addedRefs = new - old
  -> if R2 enabled: enqueue addedRefs
  -> return Entry

ffdb background worker
  -> LocalStorage
  -> R2
~~~

R2 是 replica，V1 不要求 Entry + mirror task 同 Pebble transaction。Entry 已持久化但 enqueue 失败时，
记录 `mirror_enqueue_failed`，Entry 仍成功，local canonical object 继续 serving。

## 9.4 Thumbnail mirror

Thumbnail mirror 的单位是“对象”，不是“图片关系”。

大图：

~~~text
original canonical object  ----┐
                               ├-> 两个独立 media.mirror_r2 refs
thumbnail canonical object ----┘
~~~

小图：

~~~text
Thumbnail.url == Thumbnail.link
        ↓
一个 canonical object
        ↓
一个 mirror ref
~~~

Thumbnail 本身不依赖 original mirror 成功；两个 task 都按 canonical key 独立幂等、独立重试。

R2 未配置时不 enqueue；完整配置时 enqueue；partial config fail loud。

# 10. 生命周期与回收

## 10.1 staging

- 位于 `media_path/upload-staging/`；
- 通过 media URL 临时可访问，供 Plate 编辑态预览；
- 24h TTL；
- ffweb 自动清；
- 不进入 R2；
- staging URL 不进入最终 Entry；发布时必须重写成 canonical URL。

## 10.2 canonical local orphan

可能来源：

- promote 已成功但 `PostEntry` 失败；
- Entry 后续删除；
- 编辑时移除附件/图片；
- 多个 Entry 共享同 digest 后只删除其中一个。

canonical object 是 content-addressed 且可共享，所以 request path 不直接删除。

未来只允许离线 mark-and-sweep：

1. 流式扫描 Entry `body/rawBody/thumbnails/files` 与 Profile/Group picture；
2. 收集 canonical keys；
3. 对 local/R2 object 清单做差集；
4. 应用安全期；
5. dry-run；
6. 再删除。

首版只要求 staging 自动 cleanup；canonical GC 不作为上线 blocker。

---

# 11. API 与状态码

## `POST /a/upload`

图片 staging endpoint。multipart request 必须二选一：

### 本地图片

~~~text
file=<binary>
~~~

### remote 图片

~~~text
sourceUrl=https://example.com/image.jpg
~~~

`file XOR sourceUrl`，不能同时提供，也不能都为空。

成功返回统一结构：

~~~json
{
  "assetToken": "...",
  "url": "https://m.friendfeed.me/upload-staging/<thumb-or-original-id>.jpg",
  "originalUrl": "https://m.friendfeed.me/upload-staging/<original-id>.jpg",
  "width": 1024,
  "height": 683,
  "mimeType": "image/jpeg",
  "size": 123456
}
~~~

其中：

- `url`：Plate 默认显示的 staging thumbnail URL；
- `originalUrl`：staging original URL；
- `width/height`：`url` 对应图片对象的 intrinsic size，也就是 thumbnail 的实际像素尺寸；若无需单独 thumbnail，则是 original 的 intrinsic size；
- 若无需 thumbnail，`url == originalUrl`；
- canonical URL 只在用户发布时由 ffweb promote 后生成。

## `POST /a/upload_file`

单个非图片附件 multipart `file`。

成功：

~~~json
{
  "assetToken": "...",
  "name": "report.xlsx",
  "mimeType": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
  "size": 123456
}
~~~

## `POST /a/share`

沿用现有 multipart form，并新增：

~~~text
assets=<JSON array of asset tokens>
~~~

`assets` 可为空。旧 Entry 中已有 canonical media 不需要 token；新 staging media 必须由对应 token 证明后
才可 promote。

## 通用状态码

- `400`：请求/格式错误；
- `401`：未登录；
- `413`：request/file 超限；
- `422`：remote/type 内容不符合约束；
- `429`：并发限制；
- `500`：本地处理/持久化错误；
- `504`：remote source fetch timeout。

R2 不是同步 upload response 的 `502/503` 来源，因为用户上传请求不等待 R2。

# 12. 实施顺序

1. **锁定残留行为**
   - `/a/upload` 登录边界；
   - 当前 20 MiB 行为；
   - 非图片先写后失败的缺陷测试。

2. **staging primitive**
   - `<media_path>/upload-staging`；
   - production nginx / dev Gin 均可 serve staging URL；
   - server-derived extension；
   - asset token + existing ffweb SecretKey HMAC；
   - `/a/share assets` contract；
   - TTL cleanup；
   - concurrent limits。

3. **图片 validation**
   - bytes sniff；
   - decode-config；
   - 16,384 side；
   - 50MP limit；
   - JPEG/PNG/GIF/静态 WebP；
   - thumbnail staging。

4. **canonical promotion**
   - digest key + extension；
   - atomic stream/copy publish；
   - staging re-hash；

5. **编辑器恢复**
   - file picker；
   - paste binary；
   - data URI/blob；
   - remote `sourceUrl` upload；
   - pending/error；
   - canonical URL serialization；
   - sourceUrl 丢弃。

6. **附件**
   - V1 allowlist；
   - asset token；
   - `Entry.files`；
   - public forced-download semantics。

7. **R2 task**
   - 保持现有 `PostEntry(Entry)` RPC；
   - ffweb 从最终 Plate 图片节点构造 `Entry.thumbnails[]`；
   - ffdb 只从 `Entry.thumbnails[] + Entry.files[]` 取得 canonical refs；
   - edit 计算 oldRefs/newRefs，仅 mirror addedRefs；
   - idempotent `media.mirror_r2`；
   - local -> R2；
   - image/file headers；
   - failure/retry tests。

8. **部署**
   - 新 canonical `media_url` production 由 `nginx_media.conf` 类配置直接 serve `media_path`；
   - nginx 允许 `/upload-staging/` 临时访问，禁止 directory listing；
   - nginx 按 server-derived extension 正确 serve 图片/thumbnail；
   - 文件下载通过显式 download 语义返回 attachment/nosniff，不依赖 storage path namespace；
   - dev/local 允许由 Gin serve `media_path`，包括 staging preview，并保持 attachment headers；
   - `media_url` 从 local canonical object 立即可读，不依赖 R2；
   - R2 mirror 后 key/content metadata 一致。

---

# 13. 验收

必须证明：

### 上传与 staging

- 未登录不能上传；
- staging 位于 `media_path/upload-staging/`，production nginx 与 dev Gin 都可通过高熵 staging URL 读取；
- staging 不允许目录索引，24h 后可失效；
- 超过 20 MiB file / 21 MiB request 被拒绝；
- per-user concurrent > 2 被限制；
- process image decode slots 有界；
- 24h staging 可自动清理；
- 取消编辑不产生 canonical object。

### 图片

- multipart MIME 伪造无效；
- 非图片不进入 canonical storage；
- corrupt image 拒绝；
- >50MP 或 side >16384 在完整 decode 前拒绝；
- JPEG/PNG/GIF/静态 WebP 正常；
- animated WebP 拒绝；
- 同 bytes 得到相同 canonical key；
- sourceUrl/query/token 不持久化、不出日志；
- remote source body > 20 MiB 被拒绝；
- remote fetch raw error 不直接返回/记录完整 URL；
- data/blob/staging URL 不进入最终 Entry；
- historical external image URL 仍能读；
- ffdb media mirror 不解析 `body/rawBody`；
- Thumbnail original + thumbnail 都进入结构化 refs，且相同 URL 去重；
- edit 保留引用不重复 mirror，删除引用不触发同步删除；
- 新上传图片在 Entry 发布成功时 canonical local URL 已可读取；
- R2 尚未完成时页面仍正常。

### 附件

- allowlist 外格式拒绝；
- DOC/DOCX/XLS/XLSX/MP3/HTML 正常；
- token 使用现有 ffweb SecretKey 做 HMAC-SHA256；
- token 不能跨 user、不能篡改、会过期；
- /a/share 中每个被实际引用的 staging object 必须有对应有效 token；
- image token 未被最终 Plate 引用时不触发 promote；
- file token 以 assets 数组中的存在本身作为附件引用声明；
- client 不能用 arbitrary URL 冒充新的 upload token；
- max 10 files / 100 MiB per Entry；
- display name 不影响 filesystem path；
- `Entry.files` 正确持久化；
- URL 无额外 Feed/Entry auth，已知 URL 可直接下载；
- HTML attachment 强制 attachment/octet-stream/nosniff，不能 inline 执行。

### R2

- Entry 发布不等待 R2；
- mirror task 幂等；
- R2 failure 可 retry；
- task/log 不含 token/source URL/file content；
- existing Archive/Service mirror contract 不回归。

---

# 14. 本版本明确不解决

- media/file URL authorization；
- private Feed media protection；
- signed URL；
- 一个媒体对象的引用计数；
- canonical object 在线删除；
- 全量病毒扫描；
- 大于 20 MiB 的音视频；
- 视频附件；
- R2 作为发布同步条件；
- 将历史 media key 全部改成带扩展名的新格式；
- UI 如何展示 `Entry.thumbnails[]` / `Entry.files[]`；
- 历史 `/file/*` URL 的兼容或迁移；
- per-user staging byte quota；

这些项只有出现真实产品需求后再单独设计。
