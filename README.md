# Claude Endpoint Probe (Go)

一个用 Go 编写的 Claude 端点测试基础框架，面向以下目标：

- 参数覆盖检查（Messages API 常见参数）
- Prompt Caching 写入/读取验证
- 复杂 Tool Calling 多轮链路
- Tool Choice 语义一致性探测（none/any/tool/disable_parallel）
- SSE Streaming 事件级协议验证
- Error taxonomy 与 envelope 契约验证
- 推理能力与 thinking 完整性校验（含 tamper probe）
- 极限长文本 needle-in-haystack 回归测试
- Payload/block size 深度边界探测（含二分逼近）
- 跨时间/跨模型 baseline 回归漂移告警
- 伪装模型 / 假模型协议指纹识别
- Prompt Injection 基线探测（直攻 + 间接注入）

> 设计基准以 Anthropic 官方 Messages API 为准，适合对“Anthropic 兼容端点”做真实性和能力体检。

## 目录结构

```text
cmd/claude-probe/main.go        # CLI 入口
cmd/probe-api/main.go           # HTTP API 服务（管理后台 + 用户测试）
internal/anthropic/             # 官方协议请求/响应封装
internal/probe/                 # 各测试套件与执行器
internal/server/                # API、OIDC、预算、审计、可观测
web/                            # React 前端（admin + user）
deploy/otel/                    # OTel Collector + Jaeger 本地编排
```

## 快速开始

```bash
go run ./cmd/claude-probe \
  -base-url https://api.anthropic.com \
  -api-key "$CLAUDE_API_KEY" \
  -model "claude-sonnet-4-5-20250929" \
  -suite all
```

或使用环境变量：

```bash
export CLAUDE_BASE_URL="https://api.anthropic.com"
export CLAUDE_API_KEY="your_key"
export CLAUDE_MODEL="claude-sonnet-4-5-20250929"
go run ./cmd/claude-probe -suite all
```

## Web 控制台（管理后台 + 免登录用户测试）

### 启动 API 服务

```bash
cp ./configs/server.example.yaml ./configs/server.local.yaml
# 编辑 server.local.yaml：填入 OIDC 与 test key 池

go run ./cmd/probe-api -config ./configs/server.local.yaml
```

### 启动前端

```bash
cd web
npm install
npm run dev
```

- 管理后台：`http://localhost:5173/admin`（OIDC 登录后可用）
- 用户测试页：`http://localhost:5173/user`（免登录，模板化场景）
- API：`http://localhost:8080`

### 关键接口（首版）

- `POST /api/v1/admin/runs`：后台创建测试任务（需 OIDC session）
- `GET /api/v1/admin/runs/{id}`：查看完整结果
- `GET /api/v1/admin/runs/{id}/events`：SSE 事件流
- `GET /api/v1/admin/metrics/overview`：后台概览指标
- `POST /api/v1/user/quick-test`：免登录快测（场景白名单）
- `GET /api/v1/user/quick-test/{id}`：免登录结果查询（脱敏）

`POST /api/v1/admin/runs` 支持 `dry_run=true`，用于不触发上游模型调用的联调演练（UI / SSE / 审计 / 指标可全链路验证）。

### 限额 key 与审计

- 所有模型调用都在服务端执行，前端不接触模型 key。
- key 池在 `keys.test_key_pool` 配置，支持 `daily_limit_usd / rpm / tpm`。
- 每次 run 写入 `key_usage`、审计事件、风险快照（hard-gate 命中、trust score）。

### OpenTelemetry 本地观测

```bash
cd deploy/otel
docker compose up -d
```

- OTLP 接收端：`localhost:4317`
- Jaeger UI：`http://localhost:16686`
- 在 `server.local.yaml` 中设置 `observability.otlp_endpoint: "localhost:4317"`

### API 冒烟脚本

```bash
# 启动 probe-api 后执行
./scripts/smoke_api.sh

# 若要覆盖 admin 创建任务路径，提供 admin token
ADMIN_TOKEN="your-admin-token" ./scripts/smoke_api.sh
```

## Suite 列表

- `params`：参数覆盖（temperature/top_p/top_k/metadata/stop_sequences/thinking/service_tier）
- `cache`：`cache_control` 写读 + 前缀变异 miss 验证 + 可选 `ttl=1h`
- `tools`：多工具、多轮 `tool_use`/`tool_result` 链路
- `toolchoice`：`tool_choice` 语义验证（none/any/tool/disable_parallel_tool_use）
- `stream`：`stream=true` SSE 事件序列与事件结构验证
- `error`：认证、header、malformed JSON、字段类型错误的错误语义验证
- `authenticity`：协议指纹防伪 + 官方风格ID校验 + no-tools 隐藏工具注入探针
- `reasoning`：多专业领域推理题库评分（semantic 等价判分）+ 题库分布防伪检查 + thinking block/signature + tamper 拒绝验证
- `injection`：提示注入/工具注入/工具内部信息泄漏探针（直接覆盖 + 间接 tool-result + allowlist 逃逸）
- `needle`：长文本捞针（多位置 + 多轮回归 + 尺寸倍增）
- `block`：payload 体积倍增 + 二分边界探测
- `regression`：由 `-baseline-in` 触发，比较关键指标漂移并告警
- `timeline`：由 `-history-glob` 触发，输出 P95/slope/change-point 趋势
- `trust_score`：自动附加的多维度加权信任分（含 hard-gate 优先级、decision trace）

示例：

```bash
# 仅跑难伪装协议细节
go run ./cmd/claude-probe -suite stream,error,toolchoice,authenticity,reasoning

# 输出 JSON 报告到文件
go run ./cmd/claude-probe -suite all -format json -out report.json

# 基线对比（跨时间或跨模型）
go run ./cmd/claude-probe \
  -suite reasoning,needle,authenticity,block \
  -baseline-in ./baseline.json \
  -format json \
  -out current.json

# 趋势时间线（P95 / slope / change-point）
go run ./cmd/claude-probe \
  -suite reasoning,needle,authenticity,block \
  -history-glob "./runs/*.json" \
  -history-max 500 \
  -timeline-out ./timeline.json \
  -format json \
  -out ./runs/current.json

# 生成/更新 baseline
go run ./cmd/claude-probe \
  -suite reasoning,needle,authenticity,block \
  -baseline-out ./baseline.json \
  -format json

# 多维度加权信任分（重点看是否伪装/注入）
go run ./cmd/claude-probe \
  -suite authenticity,injection,tools,toolchoice,stream,error \
  -forensics-level forensic \
  -hard-gate true \
  -score-weight-authenticity 0.35 \
  -score-weight-injection 0.30 \
  -score-weight-tools 0.15 \
  -score-weight-toolchoice 0.08 \
  -score-weight-stream 0.06 \
  -score-weight-error 0.06 \
  -score-warn-threshold 80 \
  -score-fail-threshold 65 \
  -strict

# 极限长文本捞针回归
go run ./cmd/claude-probe \
  -suite needle \
  -needle-start-bytes 524288 \
  -needle-max-bytes 33554432 \
  -needle-runs-per-pos 5

# 极限 block 探测（高成本）
go run ./cmd/claude-probe -suite block -block-start-bytes 1048576 -block-max-bytes 67108864
```

## 参数说明（重点）

- `-anthropic-version` 默认 `2023-06-01`
- `-anthropic-beta` 可选，传递 beta header
- `-deep-probe` 默认 `true`，开启更难伪装的协议细节探针
- `-forensics-level` 默认 `balanced`，可选 `fast|balanced|forensic`
- `-consistency-runs` 默认 `0`（自动按 forensics-level），用于协议一致性重复探针
- `-consistency-drift-warn` / `-consistency-drift-fail` 一致性漂移阈值（百分比，`0`=自动）
- `-reasoning-bank` 可选，自定义题库 JSON 路径（支持新 schema 与 legacy array）
- `-reasoning-repeat` 默认 `2`，推理题一致性重复轮数
- `-reasoning-domains` 默认 `all`，按领域过滤题库（逗号分隔）
- `-reasoning-max-cases` 默认 `32`，从题库采样的最大题量
- `-reasoning-domain-warn` 默认 `0.8`，单领域准确率低于此值触发 warn
- `-reasoning-domain-fail` 默认 `0.6`，单领域准确率低于此值触发 fail
- `-reasoning-weighted-warn` 默认 `0.8`，difficulty 加权得分低于此值触发 warn
- `-reasoning-weighted-fail` 默认 `0.65`，difficulty 加权得分低于此值触发 fail
- 推理判分支持：`numeric/date/yes-no/choice/alternatives("||")/unit-conversion/text-synonym` 等价判分，降低格式伪装干扰
- `-reasoning-import-in` 导入外部 benchmark 文件（import 模式）
- `-reasoning-import-out` import 模式输出题库路径（必填）
- `-reasoning-import-format` import 格式：`auto|gsm8k_jsonl|bbh_jsonl|arc_jsonl|mmlu_csv|gpqa_csv`
- `auto` 模式按文件扩展名+文件名关键词推断（如 `bbh*.jsonl`、`arc*.jsonl`、`gpqa*.csv`）
- `-reasoning-import-domain` import 时写入题目 domain（默认自动推断）
- `-reasoning-import-name` import 时题库 metadata.name（可选）
- `-reasoning-import-source` import 时题库 metadata.source（可选）
- `-trust-score` 默认 `true`，追加多维度加权评分结果
- `-hard-gate` 默认 `true`，关键信号命中直接 fail（优先于加权分）
- `-hard-gate-spoof-risk` 默认 `70`，`authenticity.spoof_risk_score` 的 hard-gate 阈值
- `-hard-gate-stream-fail` 默认 `false`，启用后 `stream.failures>0` 直接触发 hard-gate
- `-hard-gate-error-fail` 默认 `false`，启用后 `error.failures>0` 直接触发 hard-gate
- hard-gate 默认命中项：`injection.leak_count`、`injection.hidden_tool_signal_count`、`tools.unknown_tool_calls`、`authenticity.no_tools_probe_tool_calls`、`authenticity.spoof_risk_score`、`authenticity.consistency_drift_score`
- `-score-weight-authenticity` 默认 `0.30`
- `-score-weight-injection` 默认 `0.25`
- `-score-weight-tools` 默认 `0.15`
- `-score-weight-toolchoice` 默认 `0.10`
- `-score-weight-stream` 默认 `0.10`
- `-score-weight-error` 默认 `0.10`
- `-score-warn-threshold` 默认 `75`
- `-score-fail-threshold` 默认 `60`
- `-block-start-bytes` 默认 `65536`
- `-block-max-bytes` 默认 `41943040`
- `-needle-start-bytes` 默认 `262144`
- `-needle-max-bytes` 默认 `16777216`
- `-needle-runs-per-pos` 默认 `3`
- `-baseline-in` 读取历史报告并执行 regression 漂移分析
- `-baseline-out` 将当前报告写成下一轮 baseline
- `-history-glob` 读取历史报告集合并生成 timeline 趋势分析
- `-history-max` 历史报告读取上限，默认 `200`
- `-timeline-out` 输出 timeline 快照 JSON（含每指标序列与统计）
- `-strict` 若存在 warn/fail 则退出码非 0

## 官方基线（用于判定）

以下判定逻辑使用官方文档语义：

- Messages API 需要 `x-api-key` + `anthropic-version`
- Prompt caching 使用 `cache_control`，usage 包含 `cache_creation_input_tokens` / `cache_read_input_tokens`
- Tool calling 基于 content blocks（`tool_use` / `tool_result`）
- 标准端点请求体上限为 32MB（batch API 更高）

参考文档：

- https://docs.anthropic.com/en/api/messages
- https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching
- https://docs.anthropic.com/en/docs/agents-and-tools/tool-use/overview
- https://docs.anthropic.com/en/api/overview

## 注意事项

- `block` suite 默认就是高强度探测，可能产生大量 token 成本与较长运行时间。
- `needle` suite 会构造超长文档，建议单独跑并拉高 `-timeout`。
- `baseline` 对比会生成额外 `regression` 结果项，纳入 `-strict` 判定。
- `timeline` 分析会生成额外 `timeline` 结果项，纳入 `-strict` 判定。
- `injection` suite 使用随机 sentinel，不会输出真实密钥。
- `authenticity` suite 是“协议行为指纹”，不是数学意义上的绝对真伪证明。
- `trust_score` 结果会输出 `trust_score_raw`、`trust_score_final`、`hard_gate_hits`、`decision_trace`，便于法证追溯。

## 推理题库

- 题库文件：`internal/probe/reasoning_bank.json`
- 已覆盖领域：`medicine`、`law`、`finance`、`cybersecurity`、`software_architecture`、`data_engineering`、`cloud_sre`、`operations`
- schema 支持两种格式：
  - 新格式（推荐）：`{version,name,source,created_at,cases:[...]}`  
  - 兼容格式：直接 JSON array（legacy）
- 每题包含：`id`、`domain`、`difficulty`、`question`、`expected`
- `expected` 支持多答案：`a||b||c`（任一匹配即算正确）

示例（只测法律+金融+安全）：

```bash
go run ./cmd/claude-probe \
  -suite reasoning \
  -reasoning-domains law,finance,cybersecurity \
  -reasoning-max-cases 24 \
  -reasoning-domain-warn 0.82 \
  -reasoning-domain-fail 0.65 \
  -reasoning-weighted-warn 0.84 \
  -reasoning-weighted-fail 0.7 \
  -reasoning-repeat 3
```

外部 benchmark 导入示例：

```bash
# import 模式不需要 -api-key / -model

# GSM8K JSONL -> 题库
go run ./cmd/claude-probe \
  -reasoning-import-in ./datasets/gsm8k_test.jsonl \
  -reasoning-import-format gsm8k_jsonl \
  -reasoning-import-domain math_reasoning \
  -reasoning-import-out ./banks/gsm8k_bank.json

# MMLU CSV -> 题库
go run ./cmd/claude-probe \
  -reasoning-import-in ./datasets/mmlu_high_school_mathematics.csv \
  -reasoning-import-format mmlu_csv \
  -reasoning-import-domain mmlu_math \
  -reasoning-import-out ./banks/mmlu_math_bank.json

# BBH JSONL -> 题库
go run ./cmd/claude-probe \
  -reasoning-import-in ./datasets/bbh_boolean_expressions.jsonl \
  -reasoning-import-format bbh_jsonl \
  -reasoning-import-domain benchmark_reasoning \
  -reasoning-import-out ./banks/bbh_bank.json

# ARC JSON/JSONL -> 题库
go run ./cmd/claude-probe \
  -reasoning-import-in ./datasets/arc_challenge_test.jsonl \
  -reasoning-import-format arc_jsonl \
  -reasoning-import-domain science_qa \
  -reasoning-import-out ./banks/arc_bank.json

# GPQA CSV -> 题库
go run ./cmd/claude-probe \
  -reasoning-import-in ./datasets/gpqa_diamond.csv \
  -reasoning-import-format gpqa_csv \
  -reasoning-import-domain graduate_science \
  -reasoning-import-out ./banks/gpqa_bank.json

# 使用外部题库执行推理测试
go run ./cmd/claude-probe \
  -suite reasoning \
  -reasoning-bank ./banks/mmlu_math_bank.json \
  -reasoning-repeat 3 \
  -reasoning-max-cases 64
```
