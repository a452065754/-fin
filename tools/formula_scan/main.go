// Command formula_scan measures how well the formula engine covers the
// templates that ship with FineReport.
//
// It walks every .cpt under the reportlets tree, extracts the formulas from
// the cells, renders each template and attributes every formula failure to a
// cause. The result is a measured coverage number instead of a guess.
//
// Usage:
//
//	go run ./tools/formula_scan                       # summary + attribution
//	go run ./tools/formula_scan -detail               # per-error breakdown
//
// The FineReport installation is taken from FR_TEST_WEBROOT, defaulting to
// F:/tomcat-win64/webapps/webroot.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"qdake/reportviewer/internal/fr"
)

var (
	webrootFlag = flag.String("webroot", "", "FineReport webroot (default: FR_TEST_WEBROOT or F:/tomcat-win64/webapps/webroot)")
	detailFlag  = flag.Bool("detail", false, "print every distinct evaluator error")
)

var (
	// formulaPat extracts the formula text of a cell that carries one.
	formulaPat = regexp.MustCompile(`class="com\.fr\.base\.Formula"[^>]*>\s*<Attributes>\s*<!\[CDATA\[(.*?)\]\]>`)
	// warnPat extracts the formula and the reason out of a render warning.
	warnPat = regexp.MustCompile(`公式 (.*?) 计算失败: (.*)$`)
	// callPat finds function calls so their frequency can be counted.
	callPat = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
)

func main() {
	flag.Parse()
	webroot := *webrootFlag
	if webroot == "" {
		webroot = os.Getenv("FR_TEST_WEBROOT")
	}
	if webroot == "" {
		webroot = `F:/tomcat-win64/webapps/webroot`
	}
	reportlets := filepath.Join(webroot, "WEB-INF", "reportlets")
	if st, err := os.Stat(reportlets); err != nil || !st.IsDir() {
		fmt.Fprintf(os.Stderr, "找不到 reportlets 目录: %s\n用 FR_TEST_WEBROOT 或 -webroot 指定\n", reportlets)
		os.Exit(1)
	}

	paths := collectTemplates(reportlets)
	calls := map[string]int{}
	formulaCells := 0
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, m := range formulaPat.FindAllStringSubmatch(string(raw), -1) {
			formulaCells++
			for _, c := range callPat.FindAllStringSubmatch(m[1], -1) {
				calls[strings.ToUpper(c[1])]++
			}
		}
	}

	dr := fr.NewDataResolver(webroot)
	defer dr.Close()
	causes := map[string]int{}
	details := map[string]int{}
	total := 0
	for _, path := range paths {
		tpl, err := fr.ParseCPTFile(path)
		if err != nil {
			continue
		}
		p := tpl.Render(dr, nil)
		for _, w := range p.Warnings {
			m := warnPat.FindStringSubmatch(w)
			if m == nil {
				continue
			}
			total++
			causes[classify(m[2])]++
			details[m[2]+"   <<< "+m[1]]++
		}
	}

	fmt.Printf("模板数: %d\n", len(paths))
	fmt.Printf("模板中的公式单元格: %d\n", formulaCells)
	fmt.Printf("公式求值失败: %d\n\n", total)

	fmt.Println("按原因归类:")
	for _, e := range rank(causes) {
		fmt.Printf("  %6d (%4.1f%%)  %s\n", e.v, pct(e.v, total), e.k)
	}

	if *detailFlag {
		fmt.Println("\n逐条错误:")
		for i, e := range rank(details) {
			if i >= 30 {
				fmt.Printf("  ... 另有 %d 种\n", len(details)-30)
				break
			}
			fmt.Printf("  %5d  %s\n", e.v, e.k)
		}
	}

	fmt.Println("\n模板中出现的函数（按次数）:")
	for i, e := range rank(calls) {
		if i >= 30 {
			fmt.Printf("  ... 另有 %d 个函数\n", len(calls)-30)
			break
		}
		fmt.Printf("  %6d  %s\n", e.v, e.k)
	}
}

// collectTemplates returns every .cpt file under root, sorted.
func collectTemplates(root string) []string {
	var paths []string
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(p, ".cpt") {
			paths = append(paths, p)
		}
		return nil
	})
	sort.Strings(paths)
	return paths
}

// classify maps an evaluator error onto a coarse cause so the remaining gap
// can be split between the formula engine and the environment.
func classify(msg string) string {
	switch {
	case strings.Contains(msg, "无效单元格") || strings.Contains(msg, "无法解析单元格"):
		return "引用了未渲染/不存在的单元格（渲染范围缺口）"
	case strings.Contains(msg, "不是数值"):
		return "引用的单元格不是数值（多为上游失败连带）"
	case strings.Contains(msg, "暂不支持的函数"):
		return "函数尚未实现"
	case strings.Contains(msg, "报表") && strings.Contains(msg, "找不到"):
		return "跨报表引用的报表文件不存在"
	case strings.Contains(msg, "工作表"):
		return "跨表引用的工作表找不到"
	case strings.Contains(msg, "无法解析日期"):
		return "日期参数格式无法识别"
	case strings.Contains(msg, "多余内容") || strings.Contains(msg, "语法错误") ||
		strings.Contains(msg, "无法识别") || strings.Contains(msg, "缺少右括号"):
		return "语法未覆盖（单元格切片 / 数据集方法等）"
	case strings.Contains(msg, "除数不能为 0"):
		return "除零"
	default:
		return "其他: " + msg
	}
}

type kv struct {
	k string
	v int
}

func rank(m map[string]int) []kv {
	var list []kv
	for k, v := range m {
		list = append(list, kv{k, v})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].v != list[j].v {
			return list[i].v > list[j].v
		}
		return list[i].k < list[j].k
	})
	return list
}

func pct(v, total int) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(v) / float64(total)
}
