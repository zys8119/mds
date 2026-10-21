package main

import (
	"regexp"
	"strings"
	"testing"
)

var fenceBT = strings.Repeat("`", 3)

var sampleDoc = "# 标题一\n" +
	"\n" +
	"正文\n" +
	"\n" +
	fenceBT + "bash\n" +
	"# 这是注释不是标题\n" +
	"echo hi\n" +
	fenceBT + "\n" +
	"\n" +
	"## 子标题\n" +
	"\n" +
	"~~~\n" +
	"# 也是注释\n" +
	"~~~\n" +
	"\n" +
	"# 标题二\n"

func TestExtractTitlesSkipsCodeBlock(t *testing.T) {
	got := extractTitles(sampleDoc)
	want := []string{"# 标题一", "## 子标题", "# 标题二"}

	if len(got) != len(want) {
		t.Fatalf("标题数量不符: got=%d %v, want=%d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 个标题不符: got=%q, want=%q", i, got[i], want[i])
		}
	}
}

func TestHighlightMatches(t *testing.T) {
	re := regexp.MustCompile("foo")
	got := highlightMatches("a foo b foo", re)
	want := "a \x1b[1;31mfoo\x1b[0m b \x1b[1;31mfoo\x1b[0m"
	if got != want {
		t.Errorf("高亮结果不符:\n got=%q\nwant=%q", got, want)
	}

	if got := highlightMatches("no match", re); got != "no match" {
		t.Errorf("无命中时应原样返回，实际 %q", got)
	}
}

func TestExtractContentUnderTitleSkipsFakeHeading(t *testing.T) {
	got := extractContentUnderTitle(sampleDoc, "# 标题一", 1)

	if !strings.Contains(got, "# 这是注释不是标题") {
		t.Errorf("代码块内容应完整保留，实际:\n%s", got)
	}
	if strings.Contains(got, "# 标题二") {
		t.Errorf("应在一级标题处截断，不应包含 标题二:\n%s", got)
	}
}
