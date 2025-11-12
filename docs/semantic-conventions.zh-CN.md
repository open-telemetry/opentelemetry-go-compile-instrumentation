# 语义约定管理

本文档描述了在编译时插桩项目中管理 [OpenTelemetry 语义约定](https://opentelemetry.io/docs/concepts/semantic-conventions/)的工具和工作流程。

## 概述

语义约定定义了 OpenTelemetry 项目中使用的一组通用属性名称和值，以确保一致性和互操作性。本项目使用 [OTel Weaver](https://github.com/open-telemetry/weaver) 来验证和跟踪语义约定的变更。

## 前置条件

语义约定工具需要 OTel Weaver。当你运行相关的 make 目标时，它会自动安装：

```bash
make weaver-install
```

这会将 weaver CLI 工具安装到 `$GOPATH/bin`。请确保 `$GOPATH/bin` 在你的 `PATH` 中。

## 可用的 MakeFile

### 验证语义约定

验证你的语义约定定义是否遵循正确的模式和约定：

```bash
make lint/semantic-conventions
```

此命令会：

- 对照官方模式检查语义约定注册表
- 验证属性名称、类型和定义
- 报告任何模式违规或已弃用的模式
- 使用 `--future` 标志启用更严格的验证规则

**何时使用**：在提交对 `pkg/inst-api-semconv/` 中语义约定定义的更改之前运行此命令。

### 生成注册表差异

将当前的语义约定注册表与基线版本进行比较，查看发生了哪些变更：

```bash
make registry-diff
```

默认情况下，这会与 `v1.29.0` 进行比较。要使用不同的基线版本：

```bash
BASELINE_VERSION=v1.28.0 make registry-diff
```

此命令会：

- 生成一个 markdown 报告，显示版本之间的差异
- 突出显示添加、修改和删除的属性
- 将报告保存到 `tmp/registry-diff.md`
- 在终端中显示报告

**何时使用**：在更新语义约定依赖项或添加新属性时使用此命令来了解变更。

### 解析注册表模式

生成整个语义约定注册表的解析、扁平化视图：

```bash
make semantic-conventions/resolve
```

此命令会：

- 获取完整的语义约定注册表
- 解析所有引用和继承关系
- 输出包含所有定义的单个 YAML 文件
- 将输出保存到 `tmp/resolved-schema.yaml`

**何时使用**：当你需要检查完整模式或调试属性定义时使用此命令。

## 工作流程：添加新属性

在向此项目添加新的语义约定属性时，请遵循以下工作流程：

### 1. 检查上游语义约定

在定义新属性之前，检查它是否已存在于 [OpenTelemetry 语义约定](https://github.com/open-telemetry/semantic-conventions)中：

```bash
make semantic-conventions/resolve
# 在解析的模式中搜索你的属性
grep "your.attribute.name" tmp/resolved-schema.yaml
```

### 2. 定义属性

如果属性在上游不存在（或你需要项目特定的属性）：

1. 将属性定义添加到 `pkg/inst-api-semconv/instrumenter/` 中的适当文件
2. 遵循 [OpenTelemetry 属性命名约定](https://opentelemetry.io/docs/specs/semconv/general/attribute-naming/)
3. 包含适当的文档和示例

示例结构：

```go
// pkg/inst-api-semconv/instrumenter/http/http.go
package http

const (
    // HTTPRequestMethod 表示 HTTP 请求方法
    // 类型: string
    // 示例: "GET", "POST", "DELETE"
    HTTPRequestMethod = "http.request.method"

    // HTTPResponseStatusCode 表示 HTTP 响应状态码
    // 类型: int
    // 示例: 200, 404, 500
    HTTPResponseStatusCode = "http.response.status_code"
)
```

### 3. 验证你的更改

运行验证工具以确保你的定义正确：

```bash
make lint/semantic-conventions
```

修复验证器报告的任何错误或警告。

### 4. 生成差异报告

生成差异报告以记录你的更改：

```bash
make registry-diff
```

检查差异以确保只存在预期的更改。

### 5. 运行测试

确保你的更改不会破坏现有功能：

```bash
make test
```

### 6. 提交审查

提交包含语义约定更改的 PR 时：

1. CI 会自动运行 `lint/semantic-conventions`
2. 会生成注册表差异报告并作为 PR 评论发布
3. 仔细检查差异报告以确保所有更改都是有意的
4. 在合并之前解决任何 CI 失败

## 模式定义位置

本项目中的语义约定定义位于：

```
pkg/inst-api-semconv/
├── instrumenter/
│   ├── http/           # HTTP 语义约定
│   │   ├── http.go
│   │   └── ...
│   ├── net/            # 网络语义约定
│   │   ├── net.go
│   │   └── ...
│   └── utils/          # 工具函数
```

这些定义扩展或实现了官方 [OpenTelemetry 语义约定](https://github.com/open-telemetry/semantic-conventions)，用于编译时插桩。

## 持续集成

项目包含语义约定的自动检查：

### Pull Request 阶段

当你修改 `pkg/inst-api-semconv/` 中的文件时：

1. **注册表验证**：验证定义是否遵循正确的模式
2. **差异报告**：生成与上游注册表的比较
3. **PR 评论**：将更改摘要作为 PR 评论发布
4. **阻塞检查**：如果发现验证错误，CI 将**失败**，在解决问题之前阻止合并

### 主分支

当更改合并到 `main` 时：

1. **注册表验证**：重新验证当前状态
2. **基线更新**：为未来比较建立新的基线

## 最佳实践

### 1. 优先使用标准属性

始终优先使用官方注册表中的现有语义约定。仅在必要时才创建自定义属性。

### 2. 遵循命名约定

- 使用点符号：`namespace.concept.attribute`
- 对多词属性使用 snake_case：`http.request.method`
- 具体明确，避免缩写：使用 `client.address` 而不是 `cli.addr`

### 3. 充分记录

包括：

- 属性用途的清晰描述
- 预期类型（string、int、boolean 等）
- 示例值
- 任何约束或有效范围

### 4. 版本兼容性

更新语义约定时：

- 检查差异报告中的破坏性更改
- 相应地更新依赖代码
- 更新文档以反映更改

### 5. 测试影响

修改语义约定后：

- 运行所有测试：`make test`
- 使用演示应用测试：`make build-demo`
- 验证插桩仍然正常工作

## 故障排除

### Weaver 安装失败

如果自动安装失败：

1. **检查你的平台**：Weaver 支持 macOS（Intel/ARM）和 Linux（x86_64）
2. **手动安装**：从 [weaver releases](https://github.com/open-telemetry/weaver/releases) 下载
3. **验证安装**：运行 `weaver --version`

### 注册表验证错误

常见验证错误及解决方案：

- **无效的属性名称**：确保遵循点符号和命名约定
- **缺少必需字段**：添加所有必需字段（名称、类型、描述）
- **类型不匹配**：确保属性类型与预期的模式类型匹配
- **已弃用的模式**：更新为使用当前的语义约定模式

### 差异报告显示意外更改

如果差异报告显示你没有做的更改：

1. **检查基线版本**：确保你正在与正确的基线进行比较
2. **更新本地注册表**：从语义约定仓库拉取最新更改
3. **查看上游更改**：检查 [语义约定更新日志](https://github.com/open-telemetry/semantic-conventions/releases)

## 附加资源

- [OpenTelemetry 语义约定](https://opentelemetry.io/docs/concepts/semantic-conventions/)
- [语义约定仓库](https://github.com/open-telemetry/semantic-conventions)
- [OTel Weaver 文档](https://github.com/open-telemetry/weaver)
- [属性命名指南](https://opentelemetry.io/docs/specs/semconv/general/attribute-naming/)

## 有疑问或问题？

如果你遇到语义约定工具的问题：

1. 查看 [GitHub Issues](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/issues)
2. 在 [#otel-go-compt-instr-sig](https://cloud-native.slack.com/archives/C088D8GSSSF) Slack 频道询问
3. 提交包含问题详情的新 issue
