# gamis

纯渲染引擎：把嵌入 Go 模板表达式的 JSON 模板渲染为 AMIS 页面 JSON，供前端 SPA 通过接口消费。

与 [amisgo](https://github.com/zrcoder/amisgo) / amigo 不同：那些方案用 Go 类型 + 链式调用构建页面并自带 web 框架；**gamis 只做「JSON 模板 → JSON」渲染，不碰 HTTP 层**。页面配置以 JSON 文件维护（编辑器/格式化工具兼容），Go 后端负责渲染和回填数据。

## 特性

- **JSON 模板 + Go 模板表达式**：模板文件基本保持合法 JSON，值里可嵌入 `{{ .X }}`
- **整值表达式输出原始 JSON**：布尔、数字、对象、数组保留类型，不被强转成字符串
- **空值省略**：整值表达式求值为空（缺失键 / 空串 / 空切片 / 空 map / nil）时，自动省略该字段
- **片段复用**：`{{ include "frag.json" }}`，路径相对当前模板目录，自动循环检测
- **内置函数**：`include`、`default`、`json`，支持注入自定义函数（可覆盖内置）
- **灵活加载**：支持 `os.DirFS` 和 `go:embed`（统一走 `fs.FS`）
- **并发安全**：模板解析一次后缓存，Engine 可被多个 goroutine 并发使用
- **输出友好**：默认缩进 JSON，可切换紧凑模式
- 零第三方依赖，Go 1.22+

## 安装

```sh
go get github.com/khs1001/gamis
```

## 快速开始

```go
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/khs1001/gamis"
)

func main() {
	eng := gamis.New(gamis.WithFS(os.DirFS("templates")))

	http.HandleFunc("/api/page", func(w http.ResponseWriter, r *http.Request) {
		out, err := eng.Render("index.json", map[string]any{
			"Title": "欢迎使用 gamis",
			"Name":  "world",
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(out)
	})

	log.Fatal(http.ListenAndServe(":8080", nil))
}
```

`templates/index.json`：

```json
{
  "type": "page",
  "title": "{{ .Title }}",
  "body": "{{ include \"header.json\" }}"
}
```

`templates/header.json`：

```json
{
  "type": "tpl",
  "tpl": "hello {{ .Name }}"
}
```

接口返回：

```json
{
  "body": {
    "tpl": "hello world",
    "type": "tpl"
  },
  "title": "欢迎使用 gamis",
  "type": "page"
}
```

## API

```go
func New(opts ...Option) *Engine
func WithFS(fsys fs.FS) Option          // os.DirFS / embed.FS
func WithFuncs(f template.FuncMap) Option
func WithCompact() Option               // 默认缩进输出，此开关切换为紧凑 JSON

func (e *Engine) Render(name string, data any) ([]byte, error)
func (e *Engine) Parse() error          // 预解析全部 .json 模板并校验（可选）
```

- `Render`：渲染指定模板，返回校验过的合法 JSON。模板懒加载并缓存。
- `Parse`：遍历 `fs.FS` 下所有 `.json` 模板做预解析与校验，可用于启动期预热。

## 模板语法

### 值替换

数据上下文根路径即 `{{ .X }}`。数据建议使用 `map[string]any` 或含导出字段的结构体。

```json
{ "title": "Hello, {{ .Name }}!" }
```

缺失的 map 键是宽松的：整值表达式求值为空时该字段被省略，不会报错。

### 整值表达式 → 原始 JSON

当字符串值**恰好**是单个 `{{...}}` 表达式时，结果按原始 JSON 注入，类型得以保留：

```json
{
  "visible": "{{ .Show }}",     // true / false
  "count":   "{{ .Count }}",    // 数字
  "meta":    "{{ json .Meta }}" // 对象/数组
}
```

- 布尔、数字直接注入；`{{ json .Meta }}` 用于把 map/slice/struct 输出为对象/数组
- 结果不是合法 JSON 时回退为字符串（如 `{{ .Name }}` → `"John"`）
- 若数据里的字符串恰好长得像 JSON 字面量（如 `"2026"`、`"true"`、`"null"`），整值表达式会按对应类型转换；需要强制字符串时用 `{{ json .Name }}`

### 空值省略

整值表达式求值为以下值时，该字段从输出中**移除**：

| 情况 | 示例 |
| --- | --- |
| 缺失键 / nil | `{{ .Missing }}` |
| 空字符串 | `{{ .Empty }}` |
| 空切片 / 空数组 | `{{ json .EmptySlice }}` |
| 空 map / 空对象 | `{{ json .EmptyMap }}` |
| 渲染为 null | `{{ .Null }}` |

```json
// 模板
{ "badge": "{{ .Badge }}", "title": "固定" }

// .Badge 为空 → 输出
{ "title": "固定" }
```

### include（片段复用）

`{{ include "path.json" }}` 用**相同数据**渲染另一个模板，整值注入。路径相对当前模板所在目录，`../` 可回溯（不能越出 `fs.FS` 根）；循环引用会报错。

```json
// layout/page.json
{ "header": "{{ include \"partials/header.json\" }}", "body": "..." }

// layout/partials/header.json
{ "type": "tpl", "tpl": "{{ .SiteName }}" }
```

### default

Helm 语义：值为空时返回默认值。

```json
{ "title": "{{ default \"未命名\" .Title }}" }
```

### json

把任意值序列化为 JSON 文本，用于整值表达式输出对象/数组，或强制字符串输出。

```json
{ "tags": "{{ json .Tags }}", "year": "{{ json .Year }}" }
```

### 自定义函数

```go
eng := gamis.New(
	gamis.WithFS(fsys),
	gamis.WithFuncs(template.FuncMap{
		"upper": strings.ToUpper,
	}),
)
```

```json
{ "title": "{{ upper .Title }}" }
```

自定义函数可覆盖内置函数（`include` / `default` / `json`）。

## 加载方式

`os.DirFS`（开发/部署）与 `go:embed`（编译期嵌入）均可：

```go
// 磁盘目录
gamis.New(gamis.WithFS(os.DirFS("templates")))

// 编译期嵌入
//go:embed templates
var templatesFS embed.FS
gamis.New(gamis.WithFS(templatesFS)) // Render("templates/index.json", ...)
```

## 限制

- **模板 key 不支持模板化**，仅字符串值可含表达式
- **v1 不提供 `if` / `range` 等控制结构**；需要用条件时借助 `default` 或数据预加工
- 字符串字面量若以 `{{` 开头并以 `}}` 结尾，会被当作表达式处理；需要输出字面量可用 `{{ "{{" }}` 转义
- 对不存在的**结构体**字段求值会报错（map 缺失键则宽松返回空）；嵌套访问空对象（如 `{{ .A.B }}` 且 A 为 nil）会报错，这是模板缺陷的信号

## 并发

`Engine` 并发安全：模板树解析后缓存只读，每次表达式求值创建独立的 `text/template`。可在多个请求 goroutine 中共享同一个 Engine。

## 示例

见 [examples](examples) 目录：`basic`（磁盘加载 + net/http 服务）与 `embed`（go:embed 加载）。

## License

[MIT](LICENSE)