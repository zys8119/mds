package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const configFileName = ".mdsearch_config.json"

// URLConfig URL 配置
type URLConfig struct {
	Alias   string `json:"alias"`
	URL     string `json:"url"`
	Current bool   `json:"current"` // 当前选中的资源
}

// Config 配置结构
type Config struct {
	URLs []URLConfig `json:"urls"`
}

func main() {
	// 内部子命令：为 fzf 的 change:reload 提供正则过滤结果
	// 用法: mdsearch --regex-filter <正则> <内容文件> <预览目录>
	if len(os.Args) > 1 && os.Args[1] == "--regex-filter" {
		handleRegexFilter()
		return
	}

	// 解析命令行参数
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--config", "-c":
			if len(os.Args) < 3 {
				printConfigHelp()
				return
			}
			switch os.Args[2] {
			case "add", "-a":
				handleConfigAdd()
				return
			case "list", "-l":
				handleConfigList()
				return
			case "remove", "-r":
				handleConfigRemove()
				return
			case "use", "-u":
				handleConfigUse()
				return
			case "modify", "-m":
				handleConfigModify()
				return
			default:
				printConfigHelp()
				return
			}
		case "--help", "-h":
			printHelp()
			return
		}
	}

	// 正常启动：先选择别名，然后搜索对应资源
	config := loadConfig()
	if len(config.URLs) == 0 {
		fmt.Println("错误：没有配置任何资源 URL")
		fmt.Println("请使用以下命令添加：")
		fmt.Println("  mdsearch --config add <别名> <URL>")
		fmt.Println("  mdsearch --config list")
		os.Exit(1)
	}

	// 交互循环：在标题列表中按 ESC 会回到资源选择，Ctrl+C 直接退出
	for {
		selectedAlias, key, err := selectAlias(config.URLs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "错误：%v\n", err)
			os.Exit(1)
		}

		if key == keyInterrupt {
			exitInterrupt()
		}

		if selectedAlias == "" {
			fmt.Println("未选择任何资源")
			return
		}

		if searchResource(config, selectedAlias) {
			exitInterrupt()
		}
	}
}

// exitInterrupt 处理 Ctrl+C：直接退出程序，不返回上一级
func exitInterrupt() {
	fmt.Println("已退出")
	os.Exit(0)
}

// fzf 快捷键标记
const (
	keyInterrupt = "ctrl-c" // Ctrl+C：直接退出程序
	keyBack      = "esc"    // ESC：返回上一级
	keyRegex     = "ctrl-r" // Ctrl+R：进入正则检索
)

// parseFzfOutput 解析带 --expect esc,ctrl-c 的 fzf 输出
// 第一行为触发的按键（未触发则为空），第二行为选中项
func parseFzfOutput(output string) (selected string, key string) {
	lines := strings.SplitN(strings.TrimRight(output, "\n"), "\n", 2)
	if len(lines) > 0 {
		key = strings.TrimSpace(lines[0])
	}
	if len(lines) > 1 {
		selected = strings.TrimSpace(lines[1])
	}
	return selected, key
}

// fzfExitKey 把 fzf 异常退出码映射为快捷键语义
// 130 表示被中断（Ctrl+C），其余（如 1 无匹配）按 ESC 处理
func fzfExitKey(err error) (string, bool) {
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		return "", false
	}
	switch exitErr.ExitCode() {
	case 130:
		return keyInterrupt, true
	case 1:
		return keyBack, true
	}
	return "", false
}

// searchResource 搜索指定资源下的标题并复制内容
// 在标题列表按 ESC 返回资源选择（返回 false），按 Ctrl+C 请求退出程序（返回 true）
func searchResource(config Config, selectedAlias string) bool {
	var selectedURL string
	for _, u := range config.URLs {
		if u.Alias == selectedAlias {
			selectedURL = u.URL
			break
		}
	}

	if selectedURL == "" {
		fmt.Printf("错误：未找到别名 '%s' 对应的 URL\n", selectedAlias)
		return false
	}

	fmt.Printf("正在搜索资源: %s\n", selectedAlias)

	// 获取内容并搜索
	content, err := fetchContent(selectedURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误：无法获取 %s: %v\n", selectedURL, err)
		return false
	}

	if content == "" {
		fmt.Fprintln(os.Stderr, "错误：获取的内容为空")
		return false
	}

	// 提取所有标题
	titles := extractTitles(content)
	if len(titles) == 0 {
		fmt.Fprintln(os.Stderr, "错误：未找到任何 Markdown 标题")
		return false
	}

	// 创建临时文件存储完整内容
	tmpFile, err := os.CreateTemp("", "readme_*.md")
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误：创建临时文件失败: %v\n", err)
		return false
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(content); err != nil {
		fmt.Fprintf(os.Stderr, "错误：写入临时文件失败: %v\n", err)
		return false
	}
	tmpFile.Close()

	// 创建预览脚本
	previewScript := createPreviewScript(tmpFile.Name())
	defer os.Remove(previewScript)

	for {
		// 使用 fzf 选择标题；Ctrl-R 进入正则检索
		selectedTitle, key, err := runFzf(titles, previewScript)
		if err != nil {
			fmt.Fprintf(os.Stderr, "错误：%v\n", err)
			return false
		}

		if key == keyInterrupt {
			return true
		}

		if key == keyRegex {
			hitTitle, exit, err := regexSearchPanel(content, titles)
			if err != nil {
				fmt.Fprintf(os.Stderr, "错误：正则检索失败: %v\n", err)
				continue
			}
			if exit {
				return true
			}
			if hitTitle != "" {
				if copyTitleFlow(content, hitTitle) {
					return true
				}
			}
			continue
		}

		if key == keyBack || selectedTitle == "" {
			// ESC：返回上一级（资源选择）
			return false
		}

		if copyTitleFlow(content, selectedTitle) {
			return true
		}
	}
}

// copyTitleFlow 展示标题下的内容，并让用户选择复制整体内容或其中某一个代码块
// 返回 true 表示用户按了 Ctrl+C，需要直接退出程序
func copyTitleFlow(content, selectedTitle string) bool {
	level := getTitleLevel(selectedTitle)
	contentUnderTitle := extractContentUnderTitle(content, selectedTitle, level)

	fmt.Printf("\n=== %s ===\n\n", selectedTitle)
	fmt.Println(contentUnderTitle)

	copyText, copyLabel, key, err := selectCopyContent(contentUnderTitle)
	if err != nil {
		fmt.Fprintf(os.Stderr, "警告：选择复制内容失败: %v\n", err)
		return false
	}

	if key == keyInterrupt {
		return true
	}

	if copyText == "" {
		// ESC：返回上一级（标题列表）
		fmt.Println("未选择复制内容，返回标题列表")
		return false
	}

	if err := copyToClipboard(copyText); err != nil {
		fmt.Fprintf(os.Stderr, "警告：复制到剪贴板失败: %v\n", err)
	} else {
		fmt.Printf("\n✅ 已复制到剪贴板：%s\n", copyLabel)
	}

	fmt.Println("继续选择要复制的标题，按 ESC 返回资源选择")
	return false
}

func printHelp() {
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║  mdsearch - 使用 fzf 交互式搜索 Markdown 文档标题          ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("描述:")
	fmt.Println("  从远程 Markdown 文件（如 README.md）中提取所有标题，")
	fmt.Println("  通过 fzf 进行模糊搜索和实时预览，快速定位并查看内容。")
	fmt.Println()
	fmt.Println("用法:")
	fmt.Println("  mdsearch [命令] [参数...]")
	fmt.Println()
	fmt.Println("命令 (长参数 / 短参数):")
	fmt.Println("  (无参数)                          启动交互式搜索")
	fmt.Println("                                    先选择资源别名，再搜索标题")
	fmt.Println()
	fmt.Println("  --config add / -c -a <别名> <URL>   添加一个资源")
	fmt.Println("                                    别名用于快速识别和切换")
	fmt.Println()
	fmt.Println("  --config modify / -c -m <别名> <新URL>  修改资源的 URL")
	fmt.Println()
	fmt.Println("  --config list / -c -l              列出所有已配置的资源")
	fmt.Println("                                    带 * 标记的是当前选中的资源")
	fmt.Println()
	fmt.Println("  --config use / -c -u <别名>        切换到指定的资源")
	fmt.Println("                                    后续搜索将使用该资源")
	fmt.Println()
	fmt.Println("  --config remove / -c -r <别名>     删除指定的资源")
	fmt.Println()
	fmt.Println("  --help / -h                        显示此帮助信息")
	fmt.Println()
	fmt.Println("示例 (长参数):")
	fmt.Println("  1. 添加资源:")
	fmt.Println("     mdsearch --config add blog https://raw.githubusercontent.com/zys8119/Blog/refs/heads/master/README.md")
	fmt.Println()
	fmt.Println("  2. 修改资源 URL:")
	fmt.Println("     mdsearch --config modify blog https://new-url.com/README.md")
	fmt.Println()
	fmt.Println("  3. 列出资源:")
	fmt.Println("     mdsearch --config list")
	fmt.Println()
	fmt.Println("  4. 切换资源:")
	fmt.Println("     mdsearch --config use blog")
	fmt.Println()
	fmt.Println("  5. 删除资源:")
	fmt.Println("     mdsearch --config remove docs")
	fmt.Println()
	fmt.Println("  6. 启动搜索:")
	fmt.Println("     mdsearch")
	fmt.Println()
	fmt.Println("示例 (短参数):")
	fmt.Println("  1. 添加资源:")
	fmt.Println("     mdsearch -c -a blog https://raw.githubusercontent.com/zys8119/Blog/refs/heads/master/README.md")
	fmt.Println()
	fmt.Println("  2. 修改资源 URL:")
	fmt.Println("     mdsearch -c -m blog https://new-url.com/README.md")
	fmt.Println()
	fmt.Println("  3. 列出资源:")
	fmt.Println("     mdsearch -c -l")
	fmt.Println()
	fmt.Println("  4. 切换资源:")
	fmt.Println("     mdsearch -c -u blog")
	fmt.Println()
	fmt.Println("  5. 删除资源:")
	fmt.Println("     mdsearch -c -r docs")
	fmt.Println()
	fmt.Println("  6. 查看帮助:")
	fmt.Println("     mdsearch -h")
	fmt.Println()
	fmt.Println("交互操作:")
	fmt.Println("  选择资源时:  上下键切换，Enter 确认")
	fmt.Println("  搜索标题时:  输入关键词模糊搜索，右侧实时预览内容")
	fmt.Println("  按 Enter 查看选中标题的完整内容")
	fmt.Println("  之后选择复制「整体内容」或其中某一个代码块")
	fmt.Println("  （标题内容中没有代码块时，直接复制整体内容）")
	fmt.Println("  Ctrl-R: 直接进入 fzf 正则检索，输入框内输入正则即实时过滤")
	fmt.Println("          结果按标题分组列出命中行，预览保留全文并高亮命中")
	fmt.Println("          选中后回到该标题继续复制")
	fmt.Println("  复制完成后可继续选择其他标题复制")
	fmt.Println("  ESC 逐级返回：复制选择 -> 标题列表 -> 资源选择")
	fmt.Println("  Ctrl+C 任意界面直接退出程序（不返回上一级）")
	fmt.Println()
	fmt.Println("配置文件:")
	fmt.Println("  位置: ~/.mdsearch_config.json")
	fmt.Println("  可手动编辑，但建议使用命令管理")
	fmt.Println()
	fmt.Println("环境要求:")
	fmt.Println("  - fzf: 需要安装 fzf (brew install fzf)")
	fmt.Println("  - awk, grep, bash: 系统自带")
}

func printConfigHelp() {
	fmt.Println("配置管理命令:")
	fmt.Println("  mdsearch --config add <别名> <URL>   添加资源")
	fmt.Println("  mdsearch --config list               列出所有资源（显示当前选中）")
	fmt.Println("  mdsearch --config remove <别名>      删除资源")
	fmt.Println("  mdsearch --config use <别名>         切换当前选中的资源")
	fmt.Println("  mdsearch --config modify <别名> <新URL>  修改资源的 URL")
}

// handleConfigAdd 处理添加配置
func handleConfigAdd() {
	if len(os.Args) < 5 {
		fmt.Println("错误：请提供别名和 URL")
		fmt.Println("用法: mdsearch --config add <别名> <URL>")
		fmt.Println("示例: mdsearch --config add blog https://example.com/README.md")
		os.Exit(1)
	}

	alias := os.Args[3]
	url := os.Args[4]
	config := loadConfig()

	// 检查别名是否已存在
	for i, u := range config.URLs {
		if u.Alias == alias {
			config.URLs[i].URL = url
			// 如果当前没有选中任何资源，将更新的设为当前
			if !hasCurrent(config.URLs) {
				config.URLs[i].Current = true
			}
			if err := saveConfig(config); err != nil {
				fmt.Fprintf(os.Stderr, "错误：保存配置失败: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("已更新别名 '%s' 的 URL: %s\n", alias, url)
			return
		}
	}

	// 新增资源，如果是第一个则设为当前
	newURL := URLConfig{Alias: alias, URL: url, Current: false}
	if len(config.URLs) == 0 {
		newURL.Current = true
	}
	config.URLs = append(config.URLs, newURL)
	if err := saveConfig(config); err != nil {
		fmt.Fprintf(os.Stderr, "错误：保存配置失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("成功添加资源: %s -> %s\n", alias, url)
	if len(config.URLs) == 1 {
		fmt.Println("提示：该资源已设为当前选中")
	}
}

// handleConfigList 列出所有配置
func handleConfigList() {
	config := loadConfig()
	if len(config.URLs) == 0 {
		fmt.Println("当前没有配置任何资源")
		return
	}

	fmt.Println("已配置的资源:")
	fmt.Println("---")
	for i, u := range config.URLs {
		marker := "  "
		if u.Current {
			marker = "* "
		}
		fmt.Printf("%s%d. %s -> %s\n", marker, i+1, u.Alias, u.URL)
	}
	fmt.Println("---")
	fmt.Println("* 表示当前选中的资源")
}

// handleConfigRemove 删除配置
func handleConfigRemove() {
	if len(os.Args) < 4 {
		fmt.Println("错误：请提供要删除的别名")
		fmt.Println("用法: mdsearch --config remove <别名>")
		os.Exit(1)
	}

	alias := os.Args[3]
	config := loadConfig()

	found := false
	newURLs := []URLConfig{}
	for _, u := range config.URLs {
		if u.Alias == alias {
			found = true
			continue
		}
		newURLs = append(newURLs, u)
	}

	if !found {
		fmt.Printf("未找到别名 '%s'\n", alias)
		return
	}

	config.URLs = newURLs
	// 如果删除的是当前选中的，重新设置一个当前选中
	if len(config.URLs) > 0 && !hasCurrent(config.URLs) {
		config.URLs[0].Current = true
	}
	if err := saveConfig(config); err != nil {
		fmt.Fprintf(os.Stderr, "错误：保存配置失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("成功删除资源: %s\n", alias)
}

// handleConfigUse 切换当前选中的资源
func handleConfigUse() {
	if len(os.Args) < 4 {
		fmt.Println("错误：请提供要切换的别名")
		fmt.Println("用法: mdsearch --config use <别名>")
		os.Exit(1)
	}

	alias := os.Args[3]
	config := loadConfig()

	found := false
	for i := range config.URLs {
		if config.URLs[i].Alias == alias {
			config.URLs[i].Current = true
			found = true
		} else {
			config.URLs[i].Current = false
		}
	}

	if !found {
		fmt.Printf("未找到别名 '%s'\n", alias)
		os.Exit(1)
	}

	if err := saveConfig(config); err != nil {
		fmt.Fprintf(os.Stderr, "错误：保存配置失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("已切换到资源: %s\n", alias)
}

// handleConfigModify 修改资源的 URL
func handleConfigModify() {
	if len(os.Args) < 5 {
		fmt.Println("错误：请提供别名和新 URL")
		fmt.Println("用法: mdsearch --config modify <别名> <新URL>")
		fmt.Println("示例: mdsearch --config modify blog https://new-url.com/README.md")
		os.Exit(1)
	}

	alias := os.Args[3]
	newURL := os.Args[4]
	config := loadConfig()

	found := false
	for i := range config.URLs {
		if config.URLs[i].Alias == alias {
			config.URLs[i].URL = newURL
			found = true
			break
		}
	}

	if !found {
		fmt.Printf("未找到别名 '%s'\n", alias)
		os.Exit(1)
	}

	if err := saveConfig(config); err != nil {
		fmt.Fprintf(os.Stderr, "错误：保存配置失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("已更新资源 '%s' 的 URL: %s\n", alias, newURL)
}

// hasCurrent 检查是否有当前选中的资源
func hasCurrent(urls []URLConfig) bool {
	for _, u := range urls {
		if u.Current {
			return true
		}
	}
	return false
}

// getConfigPath 获取配置文件路径
func getConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return configFileName
	}
	return filepath.Join(home, configFileName)
}

// loadConfig 加载配置
func loadConfig() Config {
	configPath := getConfigPath()
	data, err := os.ReadFile(configPath)
	if err != nil {
		return Config{URLs: []URLConfig{}}
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{URLs: []URLConfig{}}
	}
	return config
}

// saveConfig 保存配置
func saveConfig(config Config) error {
	configPath := getConfigPath()
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}

// selectAlias 让用户选择别名，返回选中的别名和触发的快捷键
func selectAlias(urls []URLConfig) (string, string, error) {
	if len(urls) == 1 {
		// 只有一个资源，直接返回
		return urls[0].Alias, "", nil
	}

	// 构建选项列表：别名 + 标记当前选中
	var options []string
	for _, u := range urls {
		marker := "  "
		if u.Current {
			marker = "* "
		}
		options = append(options, fmt.Sprintf("%s%s -> %s", marker, u.Alias, u.URL))
	}

	// 使用 fzf 选择
	cmd := exec.Command("fzf",
		"--prompt", "选择资源 > ",
		"--height", "40%",
		"--header", "ESC: 退出  |  Ctrl+C: 直接退出",
		"--expect", "esc,ctrl-c",
	)
	cmd.Stdin = strings.NewReader(strings.Join(options, "\n"))
	cmd.Stderr = os.Stderr

	output, err := cmd.Output()
	if err != nil {
		if key, ok := fzfExitKey(err); ok {
			return "", key, nil
		}
		return "", "", err
	}

	selected, key := parseFzfOutput(string(output))
	if key != "" || selected == "" {
		return "", key, nil
	}

	// 从选中行提取别名（取箭头前面的部分）
	parts := strings.Split(selected, " -> ")
	if len(parts) == 2 {
		aliasPart := strings.TrimSpace(parts[0])
		// 移除可能的 * 标记
		aliasPart = strings.TrimPrefix(aliasPart, "* ")
		return aliasPart, "", nil
	}
	return selected, "", nil
}

func fetchContent(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

// isFence 判断是否为代码块围栏行（``` 或 ~~~）
func isFence(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

func extractTitles(content string) []string {
	lines := strings.Split(content, "\n")
	var titles []string
	titleRegex := regexp.MustCompile(`^#+\s+`)
	inCodeBlock := false

	for _, line := range lines {
		if isFence(line) {
			inCodeBlock = !inCodeBlock
			continue
		}
		// 代码块内的 # 是注释，不作为标题
		if inCodeBlock {
			continue
		}
		if titleRegex.MatchString(line) {
			titles = append(titles, line)
		}
	}
	return titles
}

func getTitleLevel(title string) int {
	count := 0
	for _, ch := range title {
		if ch == '#' {
			count++
		} else {
			break
		}
	}
	return count
}

func extractContentUnderTitle(content, title string, level int) string {
	lines := strings.Split(content, "\n")
	var result []string
	found := false
	titleRegex := regexp.MustCompile(`^#+`)

	// 规范化标题用于比较
	normalizeTitle := func(s string) string {
		return strings.TrimSpace(s)
	}

	normalizedTarget := normalizeTitle(title)
	inCodeBlock := false

	for _, line := range lines {
		if isFence(line) {
			inCodeBlock = !inCodeBlock
			if found {
				result = append(result, line)
			}
			continue
		}
		if !inCodeBlock && titleRegex.MatchString(line) {
			currentLevel := getTitleLevel(line)
			if found && currentLevel <= level {
				break
			}
			// 使用规范化后的标题进行比较
			if normalizeTitle(line) == normalizedTarget {
				found = true
				result = append(result, line)
				continue
			}
		}
		if found {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

// codeBlock 表示一个围栏代码块
type codeBlock struct {
	StartLine int    // 围栏起始行（0 基）
	EndLine   int    // 围栏结束行（0 基），未闭合时为最后一行
	Lang      string // 围栏后声明的语言，可能为空
	Body      string // 围栏内部的代码内容（不含围栏行）
}

// extractCodeBlocks 提取内容中的所有围栏代码块
func extractCodeBlocks(content string) []codeBlock {
	lines := strings.Split(content, "\n")
	var blocks []codeBlock
	start := -1
	lang := ""

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !isFence(line) {
			continue
		}
		if start < 0 {
			start = i
			lang = strings.TrimSpace(strings.TrimLeft(trimmed, "`~"))
			continue
		}
		blocks = append(blocks, codeBlock{
			StartLine: start,
			EndLine:   i,
			Lang:      lang,
			Body:      strings.Join(lines[start+1:i], "\n"),
		})
		start = -1
		lang = ""
	}

	// 未闭合的代码块，取到结尾
	if start >= 0 {
		blocks = append(blocks, codeBlock{
			StartLine: start,
			EndLine:   len(lines) - 1,
			Lang:      lang,
			Body:      strings.Join(lines[start+1:], "\n"),
		})
	}

	return blocks
}

// selectCopyContent 让用户选择复制整体内容还是其中某一个代码块
// 返回要复制的文本、描述和触发的快捷键；用户按 ESC 取消时返回空文本
func selectCopyContent(content string) (string, string, string, error) {
	blocks := extractCodeBlocks(content)

	const wholeLabel = "整体内容"
	if len(blocks) == 0 {
		return content, wholeLabel, "", nil
	}

	// 把整体内容和每个代码块分别写入临时文件，供 fzf 预览使用
	tmpDir, err := os.MkdirTemp("", "mds_copy_*")
	if err != nil {
		return "", "", "", err
	}
	defer os.RemoveAll(tmpDir)

	writePreview := func(idx int, text string) error {
		return os.WriteFile(filepath.Join(tmpDir, fmt.Sprintf("%d.txt", idx)), []byte(text), 0644)
	}

	if err := writePreview(0, content); err != nil {
		return "", "", "", err
	}

	options := []string{wholeLabel}
	for i, b := range blocks {
		if err := writePreview(i+1, b.Body); err != nil {
			return "", "", "", err
		}
		lang := b.Lang
		if lang == "" {
			lang = "无语言"
		}
		lineCount := 0
		if b.Body != "" {
			lineCount = len(strings.Split(b.Body, "\n"))
		}
		options = append(options, fmt.Sprintf("代码块 %d (%s, %d 行)", i+1, lang, lineCount))
	}

	previewScript := createCopyPreviewScript(tmpDir)
	defer os.Remove(previewScript)

	cmd := exec.Command("fzf",
		"--prompt", "复制内容 > ",
		"--header", "ESC: 返回标题列表  |  Ctrl+C: 退出",
		"--preview-window", "right:60%:wrap",
		"--preview", previewScript+" {}",
		"--expect", "esc,ctrl-c",
		"--bind", "enter:accept",
	)
	cmd.Stdin = strings.NewReader(strings.Join(options, "\n"))
	cmd.Stderr = os.Stderr

	output, err := cmd.Output()
	if err != nil {
		if key, ok := fzfExitKey(err); ok {
			return "", "", key, nil
		}
		return "", "", "", err
	}

	selected, key := parseFzfOutput(string(output))
	if key != "" {
		return "", "", key, nil
	}

	if selected == "" || selected == wholeLabel {
		return content, wholeLabel, "", nil
	}

	for i, b := range blocks {
		if strings.HasPrefix(selected, fmt.Sprintf("代码块 %d ", i+1)) {
			return b.Body, selected, "", nil
		}
	}

	return content, wholeLabel, "", nil
}

// createCopyPreviewScript 生成用于预览"复制内容"候选项的脚本
func createCopyPreviewScript(dir string) string {
	script := fmt.Sprintf(`#!/bin/bash
line="$1"
case "$line" in
  整体内容)
    cat "%s/0.txt"
    ;;
  代码块*)
    n=$(echo "$line" | sed 's/^代码块 \([0-9]*\).*/\1/')
    cat "%s/$n.txt"
    ;;
esac
`, dir, dir)

	tmpFile, err := os.CreateTemp("", "copy_preview_*.sh")
	if err != nil {
		return ""
	}
	tmpFile.WriteString(script)
	tmpFile.Close()
	os.Chmod(tmpFile.Name(), 0755)
	return tmpFile.Name()
}

func copyToClipboard(text string) error {
	// macOS 使用 pbcopy
	cmd := exec.Command("pbcopy")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	if _, err := stdin.Write([]byte(text)); err != nil {
		return err
	}
	stdin.Close()

	return cmd.Wait()
}

func createPreviewScript(contentFile string) string {
	script := fmt.Sprintf(`#!/bin/bash
title="$1"
file="%s"

if [ -z "$title" ] || [ -z "$file" ]; then
    exit 1
fi

level=$(echo "$title" | awk '{print match($0, /^#+/); print RLENGTH}' | tail -1)

# 用八进制构造三个反引号，避免在 Go 原始字符串中直接出现反引号
fence=$(printf '\140\140\140')

awk -v t="$title" -v l="$level" -v fence="$fence" '
BEGIN { found=0; in_code=0 }
{
    # 代码块围栏（三个反引号或三个波浪号）开头，切换状态并原样输出
    line=$0
    sub(/^[[:space:]]+/, "", line)
    if (substr(line, 1, 3) == fence || substr(line, 1, 3) == "~~~") {
        in_code = !in_code;
        if (found) print;
        next;
    }
    # 仅在非代码块内识别标题
    if (!in_code && $0 ~ /^#/) {
        match($0, /^#+/);
        current_level = RLENGTH;
        if (found && current_level <= l) {
            exit;
        }
        if ($0 == t) {
            found=1;
            print;
            next;
        }
    }
    if (found) {
        print;
    }
}
' "$file"
`, contentFile)

	tmpFile, _ := os.CreateTemp("", "preview_*.sh")
	tmpFile.WriteString(script)
	tmpFile.Close()
	os.Chmod(tmpFile.Name(), 0755)
	return tmpFile.Name()
}

// runFzf 选择标题，返回选中的标题和触发的快捷键
// 快捷键 ctrl-r 表示请求进入正则检索
func runFzf(titles []string, previewScript string) (string, string, error) {
	titlesInput := strings.Join(titles, "\n")

	tmpTitles, err := os.CreateTemp("", "titles_*.txt")
	if err != nil {
		return "", "", err
	}
	defer os.Remove(tmpTitles.Name())
	if _, err := tmpTitles.WriteString(titlesInput); err != nil {
		return "", "", err
	}
	tmpTitles.Close()

	// 构建 fzf 命令
	cmd := exec.Command("fzf",
		"--prompt", "选择标题 > ",
		"--header", "Ctrl-R: 正则检索全文  |  ESC: 返回资源选择  |  Ctrl+C: 退出",
		"--preview-window", "right:60%:wrap",
		"--preview", previewScript+" {}",
		"--expect", "ctrl-r,esc,ctrl-c",
		"--bind", "enter:accept",
	)

	// 从临时文件读取标题列表作为 stdin
	f, err := os.Open(tmpTitles.Name())
	if err != nil {
		return "", "", err
	}
	defer f.Close()

	cmd.Stdin = f
	cmd.Stderr = os.Stderr

	// 捕获 stdout
	output, err := cmd.Output()
	if err != nil {
		if key, ok := fzfExitKey(err); ok {
			return "", key, nil
		}
		return "", "", err
	}

	selected, key := parseFzfOutput(string(output))
	return selected, key, nil
}

// highlightMatches 用 ANSI 颜色高亮一行中所有正则匹配
func highlightMatches(line string, re *regexp.Regexp) string {
	locs := re.FindAllStringIndex(line, -1)
	if len(locs) == 0 {
		return line
	}

	var b strings.Builder
	last := 0
	for _, loc := range locs {
		b.WriteString(line[last:loc[0]])
		b.WriteString("\x1b[1;31m")
		b.WriteString(line[loc[0]:loc[1]])
		b.WriteString("\x1b[0m")
		last = loc[1]
	}
	b.WriteString(line[last:])
	return b.String()
}

// regexHit 一次正则命中
type regexHit struct {
	seq      int    // 预览文件序号
	titleIdx int    // 所属标题在 titles 中的下标
	title    string // 所属标题
	line     int    // 在标题内容内的行号
	text     string // 命中行文本
}

// renderRegexHits 渲染为 fzf 输入行：<预览序号>\t<标题下标>\t<标题>:<行号>: <命中行>
func renderRegexHits(hits []regexHit) string {
	var b strings.Builder
	for i, h := range hits {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%d\t%d\t%s:%d: %s", h.seq, h.titleIdx, h.title, h.line, h.text)
	}
	return b.String()
}

// computeRegexHits 按标题划分全文，收集命中行并生成预览文件
// re 为 nil 时返回全部行（用于首次进入时的完整列表）
func computeRegexHits(content string, re *regexp.Regexp, previewDir string) ([]regexHit, error) {
	titles := extractTitles(content)
	var hits []regexHit
	seq := 0

	for ti, title := range titles {
		level := getTitleLevel(title)
		section := extractContentUnderTitle(content, title, level)
		if section == "" {
			continue
		}

		lines := strings.Split(section, "\n")
		var matched []int
		for i, line := range lines {
			if re == nil || re.MatchString(line) {
				matched = append(matched, i)
			}
		}
		if len(matched) == 0 {
			continue
		}

		// 预览内容：全文保留，命中行高亮
		var b strings.Builder
		if re == nil {
			b.WriteString(title + "\n")
			b.WriteString(strings.Repeat("─", 40) + "\n")
			b.WriteString(section + "\n")
		} else {
			b.WriteString(fmt.Sprintf("%s  (命中 %d 处)\n", title, len(matched)))
			b.WriteString(strings.Repeat("─", 40) + "\n")
			for _, line := range lines {
				b.WriteString(highlightMatches(line, re))
				b.WriteString("\n")
			}
		}
		preview := b.String()

		for _, i := range matched {
			seq++
			if err := os.WriteFile(filepath.Join(previewDir, fmt.Sprintf("%d.txt", seq)), []byte(preview), 0644); err != nil {
				return nil, err
			}
			hits = append(hits, regexHit{
				seq:      seq,
				titleIdx: ti,
				title:    title,
				line:     i + 1,
				text:     strings.TrimSpace(lines[i]),
			})
		}
	}

	return hits, nil
}

// shellQuote 用单引号包裹参数，供 fzf reload 命令使用
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// handleRegexFilter 内部子命令：按正则输出命中列表，供 fzf 的 change:reload 调用
// 用法: mdsearch --regex-filter <正则> <内容文件> <预览目录>
func handleRegexFilter() {
	if len(os.Args) < 5 {
		return
	}
	pattern := os.Args[2]
	contentFile := os.Args[3]
	previewDir := os.Args[4]

	data, err := os.ReadFile(contentFile)
	if err != nil {
		return
	}

	var re *regexp.Regexp
	if strings.TrimSpace(pattern) != "" {
		re, err = regexp.Compile(pattern)
		if err != nil {
			msg := strings.ReplaceAll(err.Error(), "\n", " ")
			fmt.Printf("0\t0\t正则无效: %s", msg)
			return
		}
	}

	hits, err := computeRegexHits(string(data), re, previewDir)
	if err != nil {
		return
	}
	if len(hits) == 0 {
		fmt.Print("0\t0\t没有匹配的内容")
		return
	}
	fmt.Print(renderRegexHits(hits))
}

// regexSearchPanel 直接在 fzf 中输入正则实时检索全文
// 输入框即正则表达式输入框，结果按标题分组列出命中行，预览保留全文并高亮命中
// 返回选中命中项所属的标题；第二个返回值表示用户按了 Ctrl+C 需要直接退出程序
func regexSearchPanel(content string, titles []string) (string, bool, error) {
	self, err := os.Executable()
	if err != nil {
		return "", false, err
	}

	tmpDir, err := os.MkdirTemp("", "mds_regex_*")
	if err != nil {
		return "", false, err
	}
	defer os.RemoveAll(tmpDir)

	previewDir := filepath.Join(tmpDir, "preview")
	if err := os.MkdirAll(previewDir, 0755); err != nil {
		return "", false, err
	}

	contentFile := filepath.Join(tmpDir, "content.md")
	if err := os.WriteFile(contentFile, []byte(content), 0644); err != nil {
		return "", false, err
	}

	// 首次进入展示全部行，之后由 change:reload 调用自身按正则刷新
	initial, err := computeRegexHits(content, nil, previewDir)
	if err != nil {
		return "", false, err
	}
	if len(initial) == 0 {
		fmt.Println("文档中没有可检索的内容")
		return "", false, nil
	}

	previewScript := fmt.Sprintf(`#!/bin/bash
cat "%s/$1.txt" 2>/dev/null
`, previewDir)
	scriptFile, err := os.CreateTemp("", "regex_preview_*.sh")
	if err != nil {
		return "", false, err
	}
	defer os.Remove(scriptFile.Name())
	scriptFile.WriteString(previewScript)
	scriptFile.Close()
	os.Chmod(scriptFile.Name(), 0755)

	reload := fmt.Sprintf("%s --regex-filter {q} %s %s",
		shellQuote(self), shellQuote(contentFile), shellQuote(previewDir))

	cmd := exec.Command("fzf",
		"--ansi",
		"--disabled",
		"--delimiter", "\t",
		"--with-nth", "3..",
		"--prompt", "正则 > ",
		"--header", "直接输入正则表达式实时检索  |  ESC: 返回标题列表  |  Ctrl+C: 退出",
		"--preview-window", "right:60%:wrap",
		"--preview", scriptFile.Name()+" {1}",
		"--bind", "change:reload:"+reload,
		"--expect", "esc,ctrl-c",
		"--bind", "enter:accept",
	)
	cmd.Stdin = strings.NewReader(renderRegexHits(initial))
	cmd.Stderr = os.Stderr

	output, err := cmd.Output()
	if err != nil {
		if key, ok := fzfExitKey(err); ok {
			return "", key == keyInterrupt, nil
		}
		return "", false, err
	}

	selected, key := parseFzfOutput(string(output))
	if key != "" {
		return "", key == keyInterrupt, nil
	}
	if selected == "" {
		return "", false, nil
	}

	fields := strings.SplitN(selected, "\t", 3)
	if len(fields) < 2 {
		return "", false, nil
	}
	ti, err := strconv.Atoi(strings.TrimSpace(fields[1]))
	if err != nil || ti < 0 || ti >= len(titles) {
		return "", false, nil
	}
	return titles[ti], false, nil
}
