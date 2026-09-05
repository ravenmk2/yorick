# AGENTS.md

## 项目概览

Yorick 是 Go 1.25 编写的 CLI 备份采集工具，按一份作业定义（job file）把应用配置采集到输出目录。术语层级：作业（一份定义文件、一次运行）→ 任务（`tasks:` 条目）→ 步骤（`steps:`）。两种定义格式并存：

- **YAML**：声明式工作流。规范即 `examples/backup.yaml` 的头部注释——改动 schema 时必须同步更新它
- **JavaScript**：otto 引擎的脚本式工作流

## 目录结构

- `main.go` — 入口，仅初始化 logrus
- `app/cli.go` — CLI（urfave/cli v2）。`run`：位置参数 / `-f` / 自动探测，按扩展名分发。注意 `normalizeRunArgs`：cli/v2 的 flag 解析停在第一个位置参数，必须把 flag 提前，勿删
- `app/scripted.go` — 脚本式路径入口
- `app/declarative.go` — 声明式路径入口
- `scripted/` — 脚本式作业引擎（otto）：`script.go`、`script_init.go`、`func.go`、`task.go`
- `declarative/` — 声明式作业引擎（YAML）：
  - `spec.go` — YAML 加载器；所有加载期校验集中在此（未知 func、向前引用、表达式预编译、arg key 校验）
  - `expr.go` — `${{ }}` 表达式（expr-lang）。作用域为 `ExprScope`；纯函数必须是函数字段而非方法（expr-lang 不认方法上的 expr tag）
  - `pattern.go` — include/exclude 规则：`Rule{Type, Pattern}` 两种写法（字符串简写/映射）、选择级 MatchCandidate 与内容级 MatchContent（dir/any 规则命中上级目录即剪子树）；默认 glob（doublestar，全串匹配），`re:` 前缀为正则（MatchString）；匹配前 `filepath.ToSlash`；规则可带 depth（仅 include，默认 1，层级 ≤ depth 命中）；glob 的 `*` 不跨目录
  - `runner.go` — YAML 执行器
  - `steps.go` — 7 个 step func 的注册表与实现；args 严格解码（插值 → yaml round-trip + KnownFields）
- `utils/` — 文件/目录/ini 工具，两个引擎共用
- `examples/` — 示例；`backup.yaml` 同时是 schema 规范文档，`tour.yaml` 为功能全览
- `.github/workflows/` — CI：`test.yml`（push 到 master/develop，三平台 vet + test + build），`release.yml`（push `v*` tag，交叉编译四平台二进制并上传到 release 页）

## 构建与验证

```sh
go build ./... && go vet ./...
```

无测试套件。验证方式：用临时合成 fixture 写 smoke.yaml / smoke.js，跑二进制并检查输出树，跑完清理 fixture。**不要运行 `examples/` 下的示例**——它们引用真实用户目录，会产生真实的文件拷贝与注册表导出。

## 约定

- 日志用 logrus；注释从简，只写非显而易见的决策
- 表达式作用域只允许纯函数（无 IO 副作用）；会产生可引用输出的探测一律做成 step func
- `reg-export` 是 Windows 专属，其它平台跳过；`hosts-file` 的源路径按 OS 定义（`utils/const_*.go`，新增平台要补对应文件）。主要运行环境是 Windows + Git Bash
