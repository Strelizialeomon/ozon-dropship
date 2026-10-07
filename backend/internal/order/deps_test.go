package order

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// 验收：依赖方向符合包依赖 ADR（ADR-20261007-go-package-deps）——无反向 import、无环。
//
// 这条护栏直接解析源码的 import（只解析非测试文件），把允许的边写死在表里：
// 谁加了反方向的 import，`go test` 当场红，不用等编译器报环（有的反向边不成环，编译器兜不住）。
func TestBusinessPackageDependencyDirection(t *testing.T) {
	const modulePrefix = "github.com/Strelizialeomon/ozon-dropship/backend/internal/"

	allowed := map[string]map[string]bool{
		"catalog":  {}, // 最底层：只许 infra
		"order":    {"store": true},
		"purchase": {"order": true, "catalog": true, "store": true},
		// shipment → purchase 是本份新增的边（交接对照表要国内快递号，见 PR 说明）；
		// 方向单向、不成环：purchase 不 import shipment / order 不 import 这两者。
		"shipment": {"order": true, "purchase": true, "store": true},
	}

	for pkg, allow := range allowed {
		t.Run(pkg, func(t *testing.T) {
			// 测试的工作目录是本包目录（internal/order），业务包都是它的兄弟。
			dir, err := filepath.Abs(filepath.Join("..", pkg))
			if err != nil {
				t.Fatal(err)
			}
			fset := token.NewFileSet()
			var got []string
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("读目录 %s 失败: %v", dir, err)
			}
			for _, e := range entries {
				name := e.Name()
				if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
					continue
				}
				f, perr := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ImportsOnly)
				if perr != nil {
					t.Fatalf("解析 %s 失败: %v", name, perr)
				}
				for _, imp := range f.Imports {
					path := strings.Trim(imp.Path.Value, `"`)
					if !strings.HasPrefix(path, modulePrefix) {
						continue // 标准库与外部依赖不管
					}
					dep := strings.Split(strings.TrimPrefix(path, modulePrefix), "/")[0]
					if dep == pkg {
						continue // 包内部文件互相引用不算依赖
					}
					if dep == "infra" || dep == "middleware" {
						continue // 横切基础设施：谁都许用
					}
					got = append(got, dep)
				}
			}
			sort.Strings(got)
			seen := map[string]bool{}
			for _, dep := range got {
				if seen[dep] {
					continue
				}
				seen[dep] = true
				if !allow[dep] {
					t.Errorf("%s 不许 import %s（ADR 依赖链：%s）", pkg, dep, describeAllowed(pkg, allow))
				}
			}
		})
	}
}

func describeAllowed(pkg string, allow map[string]bool) string {
	names := make([]string, 0, len(allow))
	for k := range allow {
		names = append(names, k)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return pkg + " 只能依赖 infra"
	}
	return strings.Join(names, " / ")
}
