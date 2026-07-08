# FindSiteIcons

Go 重写版，功能等价于 [Rust 版 site_icons](https://github.com/xmsss814/site_icons)。高效的网站图标抓取工具，支持库调用和命令行使用。

## 特性

- 从多个来源发现图标（HTML `<head>`、Web App Manifest、默认 favicon 位置、站点 Logo）
- 通过解析图像二进制头部获取尺寸（PNG / JPEG / GIF / ICO / SVG），无需下载完整文件
- **保留完整的 Base64 编码图像数据**（`data` 字段）
- 使用加权评分系统提取站点 Logo
- 支持内联 data URI（`<svg>` 自动转换）
- 按 SVG > PNG > GIF > JPEG > ICO 优先级排序，同类型按分辨率降序
- 批量模式：`-l` 从文件读取多 URL，worker pool 并发处理

## 安装

```bash
go install FindSiteIcons/cmd/site-icons@latest
```

或从源码编译：

```bash
git clone <repo-url> site_icons
cd site_icons
go build -o FindSiteIcons.exe ./cmd/site-icons/
```

## 命令行用法

### 单个 URL

```bash
# 文本输出
FindSiteIcons https://github.com

# JSON 输出
FindSiteIcons --json https://github.com

# 快速模式（尽早返回最佳匹配）
FindSiteIcons --fast --json https://github.com
```

文本输出示例：

```
https://github.githubassets.com/favicons/favicon.svg site_favicon svg
https://github.githubassets.com/app-icon-512.png app_icon png 512x512
https://github.githubassets.com/apple-touch-icon-180x180.png app_icon png 180x180
```

JSON 输出示例：

```json
[
  {
    "url": "https://github.githubassets.com/favicons/favicon.svg",
    "headers": {},
    "kind": "site_favicon",
    "type": "svg",
    "data": "PHN2ZyB4bWxucz0i..."
  },
  {
    "url": "https://github.githubassets.com/app-icon-512.png",
    "headers": {},
    "kind": "app_icon",
    "type": "png",
    "size": "512x512",
    "data": "iVBORw0KGgoAAAANSUhEUgAAAgAAAAIACAYAAAD0eNT6..."
  }
]
```

### 批量模式 `-l`

从文件读取 URL 列表（每行一个），并发处理：

```bash
# 准备 URL 文件
cat > urls.txt << EOF
https://github.com
https://www.example.com
# 以 # 开头的行为注释
https://httpbin.org
EOF

# 批量处理
FindSiteIcons -l urls.txt --json

# 自定义并发数（默认 10）
FindSiteIcons -l urls.txt --json -j 5
```

JSON 模式下每行输出一个 JSON 对象（JSONL 格式）：

```json
{"url":"https://www.example.com","icons":[...]}
{"url":"https://github.com","icons":[...]}
```

文本模式下每个 URL 以 `# <url>` 作为标题：

```
# https://www.example.com
data:, site_favicon svg

# https://github.com
https://github.githubassets.com/favicons/favicon.svg site_favicon svg
```

### 完整参数

```
FindSiteIcons [--fast] [--json] [--debug] <url>
FindSiteIcons -l <file> [--fast] [--json] [--debug] [-j N]

  --fast     尽早返回最佳匹配
  --json     JSON 格式输出
  --debug    打印调试信息到 stderr
  -l <file>  从文件读取 URL 列表
  -j N       并发 worker 数量（默认 10）
```

## Go 库用法

```go
import "FindSiteIcons"

func main() {
    icons := site_icons.NewSiteIcons()

    // 抓取网站图标
    entries, err := icons.LoadWebsite("https://github.com", false)
    if err != nil {
        panic(err)
    }

    // 图标按分辨率从高到低排序
    for _, icon := range entries {
        fmt.Printf("%s %s %s\n", icon.URL, icon.Kind, icon.Info.Type)
        // icon.Info.Data 包含 Base64 编码的图像数据
    }
}
```

### 黑名单过滤

```go
icons := site_icons.NewSiteIconsWithBlacklist(func(u *url.URL) bool {
    return strings.Contains(u.Host, "doubleclick.net")
})
```

## Icon 结构

| 字段 | 类型 | 说明 |
|------|------|------|
| `url` | `string` | 图标 URL 或 data: URI |
| `headers` | `map[string]string` | HTTP 请求头 |
| `kind` | `"app_icon"` / `"site_favicon"` / `"site_logo"` | 图标类型 |
| `type` | `"png"` / `"jpeg"` / `"gif"` / `"ico"` / `"svg"` | 图像格式 |
| `size` | `"WxH"` (可选) | 单一尺寸（PNG/JPEG/GIF/SVG） |
| `sizes` | `"WxH WxH ..."` (可选) | 多尺寸（ICO） |
| `data` | `string` | Base64 编码的图像数据 |

## 图片来源

1. **HTML `<head>`** — `<link rel="icon">`、`<link rel="apple-touch-icon">` 等
2. **Web App Manifest** — `manifest.json` 中的 `icons` 字段
3. **默认位置** — `/favicon.svg` 和 `/favicon.ico`
4. **站点 Logo** — `<header>` 内或 class/id/alt/src 中包含 "logo" 的 `<img>` 标签，通过加权评分选出最佳候选

## 与 Rust 版的差异

| 项目 | Rust | Go |
|------|------|-----|
| 异步运行时 | tokio | goroutines |
| HTML 流式解析 | `flo_stream` pub/sub | channel + `io.Pipe` |
| 缓存 | `cached` crate（manifest 缓存） | 无 |
| TLD 解析 | `tldextract`（正确处理 `.co.uk`） | 简单字符串分割 |
| ICO 内嵌 PNG 尺寸 | 解析真实 PNG 尺寸 | 回退到 256×256 |
| WASM 支持 | 有（`cdylib`） | 无 |

## License

GPL-3.0
