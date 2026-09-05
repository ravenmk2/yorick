# Yorick

Yorick 是一个备份采集工具：按一份作业定义，把散落在系统各处的应用配置——文件、目录、注册表项、hosts、命令输出——采集到一个输出目录，再交给下游工具（restic / 7z / rclone…）做压缩、存储与版本管理。

作业支持两种定义格式：

- **YAML（推荐）**：声明式工作流，带表达式、条件与加载期校验
- **JavaScript（旧格式）**：基于 otto 引擎的脚本，保留兼容

## 构建

开发构建（保留调试信息）：

```sh
go build -o yorick.exe .
```

发布构建（更小、不泄露本机路径）：

```sh
go build -trimpath -ldflags "-s -w" -o yorick.exe .
```

交叉编译（以 Linux 为例）：

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o yorick .
```

参数说明：

- `-trimpath`：二进制中不嵌入本机源码路径，构建可复现
- `-ldflags "-s -w"`：裁掉符号表与 DWARF 调试信息，显著减小体积（需要调试时不要加）
- `CGO_ENABLED=0`：纯 Go 构建，无 libc 依赖（交叉编译时必须）

## 用法

```sh
yorick run <file>          # 按扩展名识别：.yaml/.yml → YAML，.js → JavaScript
yorick run -f backup.yaml  # 与位置参数等价
yorick run                 # 省略时自动探测 .yorick.yaml → .yorick.yml → .yorick.js
```

| 参数 | 说明 |
|---|---|
| `-o, --output` | 输出目录，默认 `.backup` |
| `-f, --file` | 作业定义文件路径（同位置参数） |
| `-s, --script` | 旧参数名，已废弃（仍可用，打印警告） |
| `--debug` | 调试日志 |

## YAML 作业格式

完整且始终最新的格式说明见 [examples/sample.yaml](examples/sample.yaml) 的头部注释（它就是规范），覆盖全部功能的导览见 [examples/tour.yaml](examples/tour.yaml)，此处是要点速览。

```yaml
version: 1
name: my-backup
vars:
  PROGRAMS: C:/Programs
tasks:
  - name: Git
    dest: app-data/git
    steps:
      - func: copy
        args: { src: ~/.gitconfig, dest: .gitconfig }
```

### 结构

- 顶层：`version`（必填，目前为 1）、`name`（可选）、`vars`（可选，静态常量）、`tasks`（必填，按声明顺序执行）
- 任务：`name`、`dest`（输出目录下的子目录）、`if`（可选条件）、`steps`（顺序执行）
- 步骤：`func` + `args`；可选 `id`（仅被 `steps.<id>.output` 引用时才需要）、`if`

### 表达式

动态值一律写 `${{ }}`（[expr-lang](https://expr-lang.org/) 语法，加载期编译检查）：

- 作用域：`vars.*`、`env.*`（环境变量，缺失为 `''`）、`os`（windows / linux / darwin）、`steps.<id>.output`
- 纯函数：`isDir`、`isFile`、`fileExt`、`absPath`、`isAbsPath`、`format`
- 整个值恰好是一个 `${{ }}` 时保留原始类型（列表、布尔等）；嵌在字符串中则求值后拼接
- 注意：含 `${{ }}` 的值不要用行内 flow 写法 `{ src: ${{ ... }} }`——YAML 会把 `{ }` 当 flow 边界截断，必须写成块状多行

### 步骤函数（func）

| func | args | 输出 |
|---|---|---|
| `copy` | `src`、`dest`、`include`、`exclude`（后两者可选） | — |
| `read-ini` | `file`、`expr`（如 `[0].Default`） | 字符串 |
| `latest-file` | `dir`、`depth`（默认 1） | `{name, path, rel, ext}` |
| `reg-export` | `key`、`dest`（Windows 专属，其它平台跳过） | — |
| `hosts-file` | `dest`（默认 `hosts`） | — |
| `log` | `msg` | — |
| `exec` | `cmd`；可选 `args`（逐项传参，不经 shell）、`cwd`（工作目录）、`stdout`（命令输出落到该文件） | — |

`read-ini` 的 `expr` 是路径表达式：`.` 分段，`[n]` 取第 n 个 section（0 起，按文件出现顺序），最后一段是键名。

`copy` 的 `include` 非空时 `src` 视为容器：枚举其子项，逐项拷到 `dest/<原名>`；否则直接拷贝 `src`。

### include / exclude 规则

- 每条规则两种写法（可混写）：`'plugins*'` 简写（等价 `type: any`），或 `{ type: dir, pattern: 'plugins*' }`（`type`：`dir` / `file` / `any`，默认 `any`）
- pattern 默认 **glob**（`*` `?` `**` `{a,b}` `[0-9]`）；`re:` 前缀则为正则（按原样匹配，`^$` 需自己写）
- 匹配对象是 `/` 分隔的相对路径；glob 的 `*` 不跨 `/`
- `include` 是选择级（挑选 `src` 的子项）；`exclude` 是内容级：文件命中 file/any 规则被排除，任一上级目录命中 dir/any 规则则整棵子树被剪掉
- 规则可带可选 depth（仅 include，默认 1，候选项层级 ≤ depth 才命中）
- 列表内 OR（include 非空才进入枚举模式，见上）

## JavaScript 作业格式（旧）

见 [examples/sample.js](examples/sample.js)。全局函数包括 `task`、`destDir`、`copyFile`、`copyDir`、`copyDirEx`、`exportReg`、`putHostsFile`、`getEnv`、`readIni`、`listDirs`、`listFiles`、`findLatestFile` 等（实现见 `core/func.go`、`core/script.go`）。该路径保留兼容；`if`、`exec` 等新能力只在 YAML 格式提供。

## License

见 [LICENSE](LICENSE)。
