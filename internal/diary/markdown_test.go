package diary

import "testing"

func TestSummaryStripsMarkdownCodeAndStyleTags(t *testing.T) {
	md := "# 今天的记录\n\n> 看见了 <c blue>**蓝色的海**</c> 和 <s large>晚霞</s>\n\n```go\nfmt.Println(\"hidden\")\n```\n\n- 去了 [西湖](https://example.com) 散步 ![夕阳](https://example.com/a.jpg)\n"
	got := Summary(md, 180)
	want := "今天的记录 看见了 蓝色的海 和 晚霞 去了 西湖 散步 夕阳"
	if got != want {
		t.Fatalf("Summary mismatch:\ngot:  %q\nwant: %q", got, want)
	}

	short := Summary("这是一段很长很长的中文日记摘要用来测试截断", 6)
	if short != "这是一段很长…" {
		t.Fatalf("truncated summary mismatch: %q", short)
	}

	if got := Summary("第一行\r\n第二行\r第三行", 180); got != "第一行 第二行 第三行" {
		t.Fatalf("Summary CRLF/CR mismatch: %q", got)
	}
}

func TestStyleTagsParsingAndLiteralFallback(t *testing.T) {
	if !HasStyleTags("前 <c red>重点</c> 后") {
		t.Fatal("expected HasStyleTags true")
	}
	if HasStyleTags("未闭合 <c red>文字") {
		t.Fatal("expected HasStyleTags false for unclosed tag")
	}
	if HasStyleTags("未知 <c neon>颜色</c>") {
		t.Fatal("expected HasStyleTags false for unknown color tag")
	}

	runs := ParseInline("普通 <c red><s large>**强调**</s ></c> 结束")
	// 注意 `</s >` 中间有空格不匹配，所以外层 <c red> 合法但内层 <s large> 保留原文
	if len(runs) == 0 {
		t.Fatal("expected non-empty runs")
	}

	nested := ParseInline("普通 <c blue><s large>**强调**</s></c> [链接](https://example.com)")
	foundStyled := false
	foundLink := false
	for _, r := range nested {
		if r.Text == "强调" && r.Color == "blue" && r.Size == "large" && r.Bold {
			foundStyled = true
		}
		if r.Text == "链接" && r.LinkURL == "https://example.com" {
			foundLink = true
		}
	}
	if !foundStyled {
		t.Fatalf("expected nested styled bold run, got %+v", nested)
	}
	if !foundLink {
		t.Fatalf("expected link run, got %+v", nested)
	}

	// 非法协议链接保留原文，不生成 LinkURL
	unsafe := ParseInline("[点我](javascript:alert(1))")
	for _, r := range unsafe {
		if r.LinkURL != "" {
			t.Fatalf("javascript link must not produce LinkURL: %+v", unsafe)
		}
	}
}

func TestApplyFormat(t *testing.T) {
	out, s, e := ApplyFormat(FormatBold, "hello world", 6, 11)
	if out != "hello **world**" || s != 8 || e != 13 {
		t.Fatalf("bold: got %q [%d,%d]", out, s, e)
	}
	out, s, e = ApplyFormat(FormatColor("orange"), "你好", 0, 2)
	if out != "<c orange>你好</c>" || s != 10 || e != 12 {
		t.Fatalf("color: got %q [%d,%d]", out, s, e)
	}
	out, s, e = ApplyFormat(FormatSize("large"), "", 0, 0)
	if out != "<s large>文字</s>" || s != 9 || e != 11 {
		t.Fatalf("size placeholder: got %q [%d,%d]", out, s, e)
	}
	out, _, _ = ApplyFormat(FormatHeading, "第一行\n第二行", 5, 5)
	if out != "第一行\n## 第二行" {
		t.Fatalf("heading prefix: got %q", out)
	}
}
