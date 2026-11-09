package im

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// im 包必须零业务依赖：不得 import douyinlive/*；不得出现批量抓取入口关键字。
func TestImHasNoBusinessImports(t *testing.T) {
	fset := token.NewFileSet()
	files, _ := filepath.Glob("*.go")
	for _, fn := range files {
		if strings.HasSuffix(fn, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, fn, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		src, _ := os.ReadFile(fn)
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(p, "douyinlive/") {
				t.Errorf("%s imports business pkg %q", fn, p)
			}
		}
		// 采集策略红线：im 只提供单次能力，不含轮询/批量抓取入口
		for _, bad := range []string{"for range contacts", "PollLoop", "BulkScrape"} {
			if strings.Contains(string(src), bad) {
				t.Errorf("%s 含采集策略标识符 %q(应留上层)", fn, bad)
			}
		}
	}
}
