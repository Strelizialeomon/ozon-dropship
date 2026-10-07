package audit

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// 回归：审计字段超列宽曾被 MySQL 1406 整条抹掉（长用户名登录失败 = 无痕）。
// 截断后必须：① 总长（字符）不超 n；② 不劈多字节字符。
func TestTruncateRunes(t *testing.T) {
	long := strings.Repeat("a", 300)
	got := truncateRunes(long, 128)
	if utf8.RuneCountInString(got) != 128 {
		t.Fatalf("ASCII 截断后应恰为 128 字符，实际 %d", utf8.RuneCountInString(got))
	}

	cn := strings.Repeat("测", 300) // 每个 3 字节
	got = truncateRunes(cn, 128)
	if utf8.RuneCountInString(got) != 128 {
		t.Fatalf("中文截断后应恰为 128 字符（列宽按字符算），实际 %d", utf8.RuneCountInString(got))
	}
	if !utf8.ValidString(got) {
		t.Fatal("不能把多字节字符劈一半")
	}

	if truncateRunes("短", 128) != "短" {
		t.Fatal("不超限的不该动")
	}
	if got := truncateRunes(strings.Repeat("x", 500), 1); utf8.RuneCountInString(got) != 1 {
		t.Fatalf("n=1 边界：应恰 1 字符，实际 %q", got)
	}
}
