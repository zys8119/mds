# mds — Markdown 标题检索与复制工具

从远程 Markdown（如 GitHub 上的 README）中提取标题，用 [fzf](https://github.com/junegunn/fzf) 做交互式模糊搜索与实时预览，选中后把整段内容或其中某个代码块直接复制到剪贴板。

适用于「我知道某篇文档里有这个配置/命令，但不想在浏览器里翻半天」的场景。

## 特性

- **标题索引**：解析全文所有 Markdown 标题，代码块内的 `#` 注释不会被误认成标题。
- **实时预览**：标题列表右侧同步预览该标题下的完整内容。
- **按标题分段**：复制范围严格限定在所选标题内，遇到同级或更高级标题自动截断。
- **整体 / 代码块二选一**：复制前可选择「整体内容」或某一个围栏代码块，预览即最终复制内容。
- **正则全文检索**：`Ctrl-R` 直接进入 fzf 正则检索，输入框内输入正则即实时过滤，结果按标题分组列出命中行，预览全文并红色高亮命中。
- **多资源管理**：可配置多个别名指向不同文档，随时切换。
- **不会复制完就退出**：复制后仍停留在标题列表，可继续复制；`ESC` 逐级返回上一级，`Ctrl-C` 直接退出。

## 环境要求

- Go 1.16+（仅构建时需要）
- `fzf`
- `bash`、`awk`、`sed`（系统自带）
- macOS（复制使用 `pbcopy`）

```bash
brew install fzf
```

## 构建

项目无外部 Go 依赖，也没有 `go.mod`，直接编译单文件即可：

```bash
cd mds
go build -o mds main.go
```

> 下文命令均以 `mds` 为例；`mds --help` 中的示例文案写作 `mdsearch`，两者等价，按实际二进制名调用即可。

## 快速开始

```bash
# 1. 添加一个资源（别名 + Markdown 原文 URL）
mds --config add blog https://raw.githubusercontent.com/zys8119/Blog/refs/heads/master/README.md

# 2. 启动交互式搜索
mds
```

启动后：选择资源 → 搜索标题 → 查看内容 → 选择复制范围 → 内容进剪贴板。

## 交互与快捷键

### 资源选择

| 按键             | 行为     |
|------------------|----------|
| 上下键           | 切换资源 |
| Enter            | 确认选择 |
| `ESC` / `Ctrl-C` | 退出     |

只有配置了一个资源时会跳过此步。

### 标题列表

| 按键     | 行为                         |
|----------|------------------------------|
| 输入文字 | 模糊匹配标题                 |
| Enter    | 查看该标题内容并进入复制流程 |
| `Ctrl-R` | 进入正则全文检索             |
| `ESC`    | 返回资源选择                 |
| `Ctrl-C` | 直接退出程序                 |

### 正则检索（`Ctrl-R`）

| 按键     | 行为                                             |
|----------|--------------------------------------------------|
| 输入正则 | 实时重新扫描全文，刷新命中列表；清空则显示全部行 |
| Enter    | 回到该命中所属标题，继续走复制流程               |
| `ESC`    | 返回标题列表                                     |
| `Ctrl-C` | 直接退出程序                                     |

列表行格式为 `标题:行号: 命中行内容`，按标题分组排列；正则写错时列表会显示「正则无效: 原因」，不会中断程序。

### 复制内容选择

| 按键     | 行为                         |
|----------|------------------------------|
| Enter    | 复制「整体内容」或选中代码块 |
| `ESC`    | 返回标题列表                 |
| `Ctrl-C` | 直接退出程序                 |

标题下没有围栏代码块时，跳过此步直接复制整体内容。复制的是代码块内部内容，不含围栏行（三个反引号或波浪号那一行）。

## 配置管理

```bash
mds --config add    <别名> <URL>   # 添加资源；别名已存在则更新其 URL
mds --config modify <别名> <新URL> # 修改已有资源的 URL
mds --config list                  # 列出所有资源，* 标记当前选中
mds --config use    <别名>         # 切换当前选中资源
mds --config remove <别名>         # 删除资源
mds --help                         # 查看完整帮助
```

短参数：`-c -a` / `-c -m` / `-c -l` / `-c -u` / `-c -r` / `-h`。

配置文件位于 `~/.mdsearch_config.json`，可手动编辑：

```json
{
  "urls": [
    {
      "alias": "blog",
      "url": "https://raw.githubusercontent.com/zys8119/Blog/refs/heads/master/README.md",
      "current": true
    }
  ]
}
```

## Shell 补全

内置 bash / zsh 补全脚本，支持补全子命令、`--config` 的子操作、shell 名，以及**已配置的资源别名**。

### 一键安装（推荐）

```bash
mds --install-completion        # 按 $SHELL 自动识别 bash / zsh
mds --install-completion zsh    # 也可显式指定
```

安装内容：

| shell | 补全脚本                             | 启动配置                                  |
|-------|--------------------------------------|-------------------------------------------|
| bash  | `~/.local/share/mds/completion.bash` | 在 `~/.bashrc` 中追加 source 语句         |
| zsh   | `~/.local/share/mds/_mds`            | 在 `~/.zshrc` 中追加 `fpath` 与 `compdef` |

写入 rc 文件的内容由 `# >>> mds completion >>>` / `# <<< mds completion <<<` 包裹，重复执行只会替换这一段，不会重复追加。装完执行 `source ~/.zshrc`（或 `~/.bashrc`）或重开终端即可生效。

### 手动安装

```bash
# bash：临时生效
source <(mds --completion bash)

# zsh：写入 fpath 中的目录，文件名需以 _ 开头
mds --completion zsh > "${fpath[1]}/_mds"
```

补全效果：

```text
$ mds <TAB>
--config  --help  -h  --completion  --list-aliases

$ mds --config <TAB>
add  modify  list  use  remove

$ mds --config add <TAB>
blog  skills          # 来自配置文件中的别名

$ mds --config use b<TAB>
blog

$ mds --completion <TAB>
bash  zsh
```

`--list-aliases` 是补全脚本内部调用的辅助命令，按行输出所有别名；正常使用无需手动执行。

补全脚本同时注册了 `mds` 和 `mdsearch` 两个命令名，二进制改成哪个名字都能用。

## 工作原理

1. 按别名取出 URL，`GET` 下载 Markdown 原文。
2. 逐行扫描提取标题，维护代码围栏状态以跳过代码块内的 `#`。
3. 以 `fzf --preview` 调用一个临时 shell 脚本，用 `awk` 抽取所选标题到下一个同级/更高级标题之间的内容作为预览。
4. 选中标题后解析围栏代码块，把「整体内容」和各代码块分别写入临时文件，作为复制选择的候选项与预览源。
5. 正则检索使用 fzf `--disabled` 关闭内置过滤，通过 `change:reload:` 在每次输入变化时调用内部子命令重新计算命中行：

   ```bash
   mds --regex-filter <正则> <内容文件> <预览目录>
   ```

   该子命令按标题分段匹配、生成带 ANSI 高亮的预览文件，并输出 fzf 列表行；属于内部实现，正常使用无需手动调用。

6. 最终内容通过 `pbcopy` 写入剪贴板。

所有临时文件（预览脚本、标题列表、检索缓存）都会在对应流程结束时清理。

## 测试

```bash
go test main.go main_test.go
```

覆盖标题提取（跳过代码块内伪标题）、标题内容截断、正则命中高亮。

`real_test.go` 会在 `/tmp/readme.md` 存在时打印其标题列表，可作为真实文档的冒烟检查：

```bash
curl -sL <某个 README 的 raw 地址> -o /tmp/readme.md
go test -run TestRealReadme -v main.go real_test.go
```

## 已知限制

- 仅支持 macOS 剪贴板（`pbcopy`）。
- 标题识别为行首 `#` 加空白的 ATX 风格，不支持 Setext（`===` / `---`）标题。
- 内容一次性读入内存，超大文件（数十 MB 级）体验会下降。
