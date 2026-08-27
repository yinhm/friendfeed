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

- `POST /a/upload`：正文图片；
- `POST /a/upload/mirror`：用户新粘贴的 remote HTTP/HTTPS 图片；
- `POST /a/upload_file`：文件附件；
- multipart/request limit；
- 实际字节类型识别；
- 图片 decode、像素检查、thumbnail；
- 文件 allowlist；
- temporary staging；
- staging 状态 / asset token；
- 上传并发和 staging quota；
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

ffdb 不理解 Plate `rawBody`。user-upload refs 的提取只基于最终 canonical Entry：

- `Entry.files[].url`：识别 `media_url/u/f/`；
- `Entry.body`：用 HTML parser 提取 `img[src]`，只识别 `media_url/u/i/`；
- staging URL、外部 URL、历史非 canonical media URL 一律忽略。

这样不需要额外的 PostEntry RPC，也不需要 ffdb 理解编辑器内部状态。

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
- 每个用户尚未过期的 staged data：最多 256 MiB；
- staging TTL：24 小时。

超过 request/file 大小返回 `413`；命中并发或 staging quota 返回 `429`。

图片先用 decode-config/等价轻量方式读取尺寸，在进入完整像素 decode 前执行 side/pixel limit。不能先分配
完整超大像素缓冲后再判断限制。

## 3.3 thumbnail

原图和 thumbnail 都先生成在 staging。

目标宽度继续使用当前 1024 px 配置。对于无需缩放的小图：

~~~text
thumb == original
~~~

允许 `thumbUrl == url`，不强制制造第二个对象。

需要 resize 时，thumbnail 是独立的 content-addressed JPEG object；GIF/WebP 动图的 thumbnail 可以是
静态首帧。透明 PNG 的 thumbnail 若编码 JPEG，必须明确使用统一背景处理；实现测试锁定行为，不能产生
未初始化/随机背景。

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

上传成功后，ffweb 必须保存足够的 staging state，使发布时可以验证该对象属于当前用户且未被篡改。
可以继续使用 HMAC asset token 作为无数据库的简化实现。

token 至少绑定：

- upload user UUID；
- upload ID；
- kind：`image` / `file`；
- SHA-256 digest；
- server-derived extension；
- verified MIME/type；
- bytes；
- 图片 width/height（image）；
- sanitized display name（file）；
- issued-at / expires-at。

默认 24 小时过期。

token/staging state 不作为下载权限，不持久化进 Entry，不记录日志。

## 4.4 cleanup

ffweb：

- 启动时清理超期 staging；
- 运行中每小时扫描一次；
- 删除已超过 24 小时的 staging object；
- 清理失败只记录安全错误类别，不终止服务。

取消编辑、关闭页面、上传后不发布，都只留下有 TTL 的 staging object，不进入 canonical storage。

# 5. Canonical LocalStorage

## 5.1 为什么 promote 放在 ffweb

本版本固定：

> staging -> canonical LocalStorage 的 promote 由 ffweb 在调用 `PostEntry` **之前**完成。

原因：

1. ffweb 已经掌握 staging state / asset token；
2. ffweb 已经理解 Plate 节点、附件选择和用户当前编辑状态；
3. ffdb 不需要知道 upload ID、asset token 或 staging URL；
4. 保持现有 `PostEntry(Entry)` RPC，不为媒体上传新增 transport contract；
5. 如果把 promote 放进 ffdb，就必须额外传 staging refs，或让 ffdb 解析 Plate/rawBody；两者都增加不必要耦合。

代价是存在一个很小窗口：

~~~text
promote success
  ->
PostEntry fails
  ->
canonical local orphan
~~~

V1 主动接受这个窗口。canonical object 是 content-addressed，后续可以离线 mark-and-sweep；不为了消灭
少量 orphan 建立 filesystem + Pebble 跨系统事务，也不把 staging 生命周期搬进 ffdb。

## 5.2 发布时 promote

`POST /a/share` 提交时，ffweb：

1. 从 Plate/附件编辑态取得本次仍被引用的 staging state / asset token；
2. 校验当前 session user；
3. 校验 expiry；
4. 校验 staging file 仍存在；
5. 重新确认 size + SHA-256；
6. 按 verified type 计算 canonical key；
7. 使用同一 `media_path` filesystem 内的 atomic rename/publish primitive 写入 canonical path；
8. 若相同 digest + verified type 的 canonical object 已存在，则幂等复用并删除 staging；
9. 把 Plate 节点中的 staging `url/thumbUrl` 改写成 canonical URL；
10. 构造最终 `Entry.files`；
11. 重新序列化最终 `rawBody/body`；
12. 调用现有 ffdb `PostEntry(Entry)`；
13. 成功返回 Entry。

最终传给 ffdb 的 Entry **不得包含** `upload-staging/` URL。

如果 promote 后 `PostEntry` 失败，保留 canonical object，后续由离线 GC 处理；不要在失败路径立即删除，
因为同 digest canonical object 可能已经被其他 Entry 引用。

## 5.3 canonical key

新的 user-upload object 使用完整 SHA-256 + server-derived extension，并继续分片：

~~~text
u/i/a/b/<remaining-sha256>.jpg
u/i/a/b/<remaining-sha256>.png
u/f/a/b/<remaining-sha256>.pdf
u/f/a/b/<remaining-sha256>.docx
~~~

其中：

- `u/i`：user inline image；
- `u/f`：user file attachment；
- `a/b/...`：完整 digest 的分片；
- extension 不是用户输入，而是服务端类型识别结果。

同一内容 + 同一 verified type 得到确定、幂等的 key。

现有 archive/mirror object key 保持兼容，不做批量改名。

## 5.4 Content-Type metadata

V1 **不新增本地 sidecar metadata 数据库**。

使用：

- canonical server-derived extension 让本地 nginx 对 inline image 返回正确 image Content-Type；
- `Entry.files[].type` 保存服务端确认的附件 MIME；
- ffdb 从 canonical URL + `Entry.files[].type` / image extension 恢复 R2 PUT 所需 metadata；
- R2 object metadata 写入可信 Content-Type；
- file attachment serving 强制 attachment/octet-stream。

如果未来出现“无扩展名 object + 多种 serving metadata”的真实需求，再设计独立 metadata。

# 6. 编辑器与剪贴板

一次 paste 可能同时有 image binary、`text/html` 和 `text/plain`。顺序：

1. Clipboard `items/files` 有图片 binary 时优先，只消费一次；
2. 没 binary、HTML 中有 `data:image/*` 时，浏览器转 Blob 后走 `POST /a/upload`；
3. `blob:` 只能转 Blob 后上传，不能持久化；
4. HTML 中 `http/https` 图片通过 `POST /a/upload/mirror` mirror 到 staging；
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
- remote mirror source URL。

历史 Entry 中已经存在的外部 image URL 继续兼容读取，不在 read path 自动 mirror。

---

# 7. Remote image mirror

`POST /a/upload/mirror` 只处理用户新粘贴/插入的 remote image。

网络边界复用 `media` safe fetch primitive：

- 仅 HTTP/HTTPS；
- DNS 解析后拒绝 loopback/private/link-local/CGNAT；
- redirect 每跳重新验证；
- redirect 数有界；
- 总 timeout 有界；
- response body 有界；
- 不发送用户 Cookie；
- 不发送 Authorization；
- 不发送 Referer；
- remote Content-Type 不可信。

下载后进入与本地 upload 相同的：

~~~text
sniff -> decode config -> size/pixel limit -> full decode -> thumbnail -> staging
~~~

## 7.1 sourceUrl

`sourceUrl` 只存在于当前 mirror request 的内存上下文。

mirror 成功后立即丢弃：

- 不写 `rawBody`；
- 不写 `Entry.body`；
- 不写 protobuf；
- 不写 staging metadata；
- 不作为 renderer fallback。

## 7.2 URL 与日志脱敏

remote fetch error 不得包含完整 URL。

尤其 query / fragment 可能携带 signed token。日志和浏览器错误中不得出现：

~~~text
?token=...
?signature=...
?X-Amz-...
#...
~~~

运行日志最多记录安全化 hostname、结果类别、bytes、耗时；若 hostname 也没有诊断价值，可以只记录错误类别。

错误映射：

- URL 解析/协议错误：`400`；
- remote 非 2xx、响应超限、内容非允许图片：`422`；
- timeout：`504`；
- 本地 staging/encode 失败：`500`。

内部 DNS/IP 拒绝细节不返回浏览器。

---

# 8. 文件附件

## 8.1 V1 allowlist

第一版收紧为：

### 图片文件

- JPEG；
- PNG；
- GIF；
- WebP。

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
- JPEG/PNG/GIF/WebP：真实 image decode；
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

上传和 staging 使用第 3/4 节相同并发、quota 和 TTL。

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

## 8.5 下载 URL

当前不做授权下载。

新用户图片和附件的 canonical URL 都统一由 `media_url + canonical key` 形成：

~~~text
<media_url>/u/i/<sharded-sha256>.jpg
<media_url>/u/f/<sharded-sha256>.xlsx
~~~

生产环境 `media_url` 指向独立 media origin，由 nginx 直接从 `media_path` serve。开发环境可以把
`media_url` 配到 ffweb 的 Gin local-media route，例如 `http://localhost:8080/file`；Gin 只是 dev fallback，
不是 production serving path。

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

production nginx 对 `/u/f/` 必须按路径强制 attachment，而不是根据扩展名 inline：

~~~nginx
location ^~ /u/f/ {
    add_header X-Content-Type-Options nosniff always;
    default_type application/octet-stream;
    # implementation must emit Content-Disposition: attachment
    try_files $uri =404;
}
~~~

开发环境 Gin local-media handler 对 `u/f/` 提供同样的 attachment/octet-stream/nosniff 语义，并允许
`upload-staging/` 临时媒体访问。当前 production serving source 是 nginx + local `media_path`；R2 是异步
replica/恢复副本，不要求浏览器在 V1 直接从 R2 object endpoint 下载。

---

# 9. R2 后台 mirror

对 user-upload canonical object，R2 不同步写。

新增/复用 Task 类型：

~~~text
media.mirror_r2
~~~

payload：

~~~text
canonical_key
verified_mime
kind: image|file
~~~

不得包含文件内容、asset token、session、sourceUrl 或 credential。

idempotency key：

~~~text
media-r2:<canonical-key>
~~~

## 9.1 ffdb 如何识别需要 mirror 的对象

不新增 `PostEntryWithMedia`，保持现有：

~~~protobuf
rpc PostEntry(Entry) returns (Entry)
~~~

ffdb 在 Entry 成功 canonical persistence 后，从最终 Entry 提取 refs：

1. 遍历 `Entry.files[].url`，只接受当前 `media_url` 下 `/u/f/` canonical path；
2. 用标准 HTML parser 解析 `Entry.body`，提取 `img[src]`，只接受当前 `media_url` 下
   `/u/i/` canonical path；
3. 不解析 Plate `rawBody`；
4. 不 mirror `upload-staging/`；
5. 不处理外部 URL；
6. key 去重。

这个提取只用于 replica scheduling，不改变 Entry 的业务语义。

## 9.2 enqueue 时序

~~~text
ffweb
  -> promote + rewrite
  -> PostEntry(Entry)

ffdb PostEntry
  -> authorize
  -> persist canonical Entry
  -> derive canonical user-upload refs
  -> if R2 enabled: enqueue media.mirror_r2
  -> return Entry

ffdb background worker
  -> LocalStorage
  -> R2
~~~

R2 是 replica，因此 V1 不为了 enqueue 去重构当前 `PutEntry` Pebble batch，也不要求 Entry + mirror task
同事务。

如果 Entry 已经持久化，但 enqueue 失败：

- 记录 `mirror_enqueue_failed`；
- `PostEntry` 仍成功；
- local canonical object 继续正常 serving；
- 后续可通过 retry/reconcile 补 R2。

这和现有“派生状态允许异步收敛”的项目原则一致，也避免为了 replica 增加新的 RPC 或 transaction staging
primitive。

R2 未配置时不 enqueue；完整配置时 enqueue；partial config 继续 fail loud。

## 9.3 worker

handler：

1. 从 canonical LocalStorage 打开对象；
2. image 使用 verified image MIME；
3. file 在 R2 serving metadata 上使用 `application/octet-stream` / attachment 语义；
4. PUT 到 R2 同 key；
5. retry 走现有 Task lease/backoff；
6. 成功后完成 task。

R2 失败不回滚 Entry，也不删除 local object。

现有 Archive/Service `mirrorMedia` 的同步兼容路径不在本项改造范围。

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

单图片 multipart `file`。

成功：

~~~json
{
  "assetToken": "...",
  "url": "https://m.friendfeed.me/upload-staging/<upload-id>.jpg",
  "thumbUrl": "https://m.friendfeed.me/upload-staging/<thumb-upload-id>.jpg",
  "width": 1600,
  "height": 900,
  "mimeType": "image/jpeg",
  "size": 123456
}
~~~

返回 staging URL，上传成功后即可直接在 Plate 中显示。canonical URL 只在用户发布时由 ffweb promote 后
生成，并在调用 `PostEntry` 前写入最终 `rawBody/body`。

## `POST /a/upload/mirror`

请求一个 `sourceUrl`，返回与 `/a/upload` 相同结构。

## `POST /a/upload_file`

返回 `assetToken/name/mimeType/size`。

## 通用状态码

- `400`：请求/格式错误；
- `401`：未登录；
- `413`：request/file 超限；
- `422`：remote/type 内容不符合约束；
- `429`：并发或 staging quota；
- `500`：本地处理/持久化错误；
- `504`：remote mirror timeout。

R2 不再是同步 upload response 的 `502/503` 来源，因为用户上传请求不等待 R2。

---

# 12. 实施顺序

1. **锁定残留行为**
   - `/a/upload` 登录边界；
   - 当前 20 MiB 行为；
   - 非图片先写后失败的缺陷测试。

2. **staging primitive**
   - `<media_path>/upload-staging`；
   - production nginx / dev Gin 均可 serve staging URL；
   - server-derived extension；
   - asset token；
   - TTL cleanup；
   - per-user 256 MiB staged quota；
   - concurrent limits。

3. **图片 validation**
   - bytes sniff；
   - decode-config；
   - 16,384 side；
   - 50MP limit；
   - JPEG/PNG/GIF/WebP；
   - thumbnail staging。

4. **canonical promotion**
   - digest key + extension；
   - atomic stream/copy publish；
   - staging re-hash；
   - `u/i` / `u/f` namespaces。

5. **编辑器恢复**
   - file picker；
   - paste binary；
   - data URI/blob；
   - remote mirror；
   - pending/error；
   - canonical URL serialization；
   - sourceUrl 丢弃。

6. **附件**
   - V1 allowlist；
   - asset token；
   - `Entry.files`；
   - SSR/React 文件列表；
   - public forced-download route。

7. **R2 task**
   - 保持现有 `PostEntry(Entry)` RPC；
   - ffdb 从最终 `Entry.files` + `Entry.body img[src]` 提取本站 canonical refs；
   - idempotent `media.mirror_r2`；
   - local -> R2；
   - image/file headers；
   - failure/retry tests。

8. **部署**
   - production media 由 `nginx_media.conf` 类配置直接 serve canonical `media_path`，ffweb 不代理；
   - nginx 允许 `/upload-staging/` 临时访问，禁止 directory listing；
   - nginx `/u/i/` 按 server-derived extension 返回正确图片 MIME；
   - nginx `/u/f/` 强制 attachment/octet-stream/nosniff；
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
- user staged bytes > 256 MiB 被限制；
- 24h staging 可自动清理；
- 取消编辑不产生 canonical object。

### 图片

- multipart MIME 伪造无效；
- 非图片不进入 canonical storage；
- corrupt image 拒绝；
- >50MP 或 side >16384 在完整 decode 前拒绝；
- JPEG/PNG/GIF/WebP 正常；
- 同 bytes 得到相同 canonical key；
- sourceUrl/query/token 不持久化、不出日志；
- data/blob/staging URL 不进入最终 Entry；
- historical external image URL 仍能读；
- 新上传图片在 Entry 发布成功时 canonical local URL 已可读取；
- R2 尚未完成时页面仍正常。

### 附件

- allowlist 外格式拒绝；
- DOC/DOCX/XLS/XLSX/MP3/HTML 正常；
- token 不能跨 user、不能篡改、会过期；
- client 不能用 arbitrary URL 冒充新的 upload token；
- max 10 files / 100 MiB per Entry；
- display name 不影响 filesystem path；
- `Entry.files` 正确持久化和渲染；
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
- 将历史 media key 全部改成带扩展名的新格式。

这些项只有出现真实产品需求后再单独设计。