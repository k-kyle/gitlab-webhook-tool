# GitLab Webhook Tool

一个轻量的 Go 服务：接收 GitLab Webhook 事件，并将消息转发为飞书机器人卡片通知。

## 功能说明

- 监听 HTTP 地址：`/web-hook`
- 支持事件类型：
  - `push`
  - `merge_request`
- 飞书通知内容包含：
  - 项目、用户、分支信息
  - push 的 commit 摘要
  - push 的 `modified` 文件列表（最多展示 10 个）
- 运行日志包含：
  - 服务启动信息
  - webhook 请求接收和分发信息
  - 飞书发送结果

## 默认访问地址

- `http://<your-ip>:6710/web-hook`

## 启动参数

```bash
./gitlab-webhook-tool -port 6710 -config ./config.yaml -feishuWebhook https://open.feishu.cn/open-apis/bot/v2/hook/fallback
```

- `-port`：HTTP 监听端口，默认 `6710`
- `-config`：路由配置文件路径，支持 `.json/.yaml/.yml`
- `-feishuWebhook`：兜底飞书机器人 Webhook（可选）

## 本地运行（开发调试）

```bash
go run ./cmd/main.go -port 6710 -config ./config.yaml
```

## 按项目路由到不同机器人

支持通过配置文件将不同项目路由到不同飞书机器人。

路由优先级如下：

1. `project.path_with_namespace`（例如 `group/projectA`）
2. `project.name`（例如 `projectA`）
3. `*`（默认路由）
4. `-feishuWebhook`（命令行兜底）

### YAML 示例

```yaml
routes:
  "*":
    webhook: "https://open.feishu.cn/open-apis/bot/v2/hook/defaultA"
    note: "默认路由"
    group: "研发公共群"
    owner: "ops"
  "projectA":
    webhook: "https://open.feishu.cn/open-apis/bot/v2/hook/botB"
    note: "projectA 专用群"
    group: "projectA-group"
    owner: "alice"
  "group/projectB":
    webhook: "https://open.feishu.cn/open-apis/bot/v2/hook/botC"
    note: "projectB 专用群"
    group: "projectB-group"
    owner: "bob"
```

### JSON 示例

```json
{
  "routes": {
    "*": {
      "webhook": "https://open.feishu.cn/open-apis/bot/v2/hook/defaultA",
      "note": "默认路由",
      "group": "研发公共群",
      "owner": "ops"
    },
    "projectA": {
      "webhook": "https://open.feishu.cn/open-apis/bot/v2/hook/botB",
      "note": "projectA 专用群",
      "group": "projectA-group",
      "owner": "alice"
    },
    "group/projectB": {
      "webhook": "https://open.feishu.cn/open-apis/bot/v2/hook/botC",
      "note": "projectB 专用群",
      "group": "projectB-group",
      "owner": "bob"
    }
  }
}
```

### 启动示例

仅使用配置文件：

```bash
./gitlab-webhook-tool -port 6710 -config ./config.yaml
```

配置文件 + 命令行兜底：

```bash
./gitlab-webhook-tool -port 6710 -config ./config.yaml -feishuWebhook https://open.feishu.cn/open-apis/bot/v2/hook/fallback
```

## 编译

产物目录：`./dist/linux/`

### 在 bash（Git Bash / WSL / Linux / macOS）下交叉编译 Linux 可执行文件

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ./dist/linux/gitlab-webhook-tool ./cmd/main.go
```

或：

```bash
export CGO_ENABLED=0
export GOOS=linux
export GOARCH=amd64
go build -o ./dist/linux/gitlab-webhook-tool ./cmd/main.go
```

### 在 Windows PowerShell 下交叉编译 Linux 可执行文件

```powershell
$env:CGO_ENABLED="0"
$env:GOOS="linux"
$env:GOARCH="amd64"
go build -o .\dist\linux\gitlab-webhook-tool .\cmd\main.go
```

## GitLab Webhook 配置建议

在 GitLab 项目中配置 Webhook：

- URL：`http://<service-ip>:6710/web-hook`
- Method：`POST`
- Content type：`application/json`
- 事件勾选：
  - Push events
  - Merge request events

## 当前已知限制

- 当前未内置 webhook token 鉴权（如需可后续扩展）
- 仅处理 `push` 与 `merge_request` 两类事件
