package main

import (
	"os"
	"testing"
)

func TestRealReadme(t *testing.T) {
	b, err := os.ReadFile("/tmp/readme.md")
	if err != nil {
		t.Skip(err)
	}
	titles := extractTitles(string(b))
	t.Logf("共 %d 个标题", len(titles))
	for _, ti := range titles {
		t.Log(ti)
	}
}
