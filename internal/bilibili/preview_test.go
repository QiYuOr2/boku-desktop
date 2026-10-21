package bilibili

import (
	"strings"
	"testing"
)

// feedItem wraps one item's modules in the answer the endpoint returns, so a
// test can state only the part it is about.
func feedItem(body string) string {
	return `{"code":0,"message":"0","data":{"items":[{"id_str":"1","type":"DYNAMIC_TYPE_WORD","modules":{` +
		`"module_author":{"mid":1,"name":"UP","face":"","pub_ts":1700000000},` + body + `}}]}}`
}

// Every kind of post the feed carries ends up with a line to preview. The
// fixtures are the shapes captured from the real endpoint.
func TestEveryKindHasAPreview(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		want    string
		wantNot string
	}{
		{
			name: "a video carries only a title",
			body: `"module_dynamic":{"desc":null,"major":{"archive":{"bvid":"BV1x","title":"视频标题","jump_url":"//www.bilibili.com/video/BV1x"}}}`,
			want: "视频标题",
		},
		{
			name: "a picture post keeps its words in opus.summary",
			body: `"module_dynamic":{"desc":null,"major":{"opus":{"jump_url":"//www.bilibili.com/opus/1","title":"","summary":{"text":"今天天气很好"}}}}`,
			want: "今天天气很好",
		},
		{
			name: "a post's own words win over what it points at",
			body: `"module_dynamic":{"desc":{"text":"说点什么"},"major":{"archive":{"bvid":"BV1x","title":"视频标题","jump_url":"//www.bilibili.com/video/BV1x"}}}`,
			want: "说点什么",
		},
		{
			name: "a picture post that is only emotes says what it is",
			body: `"module_dynamic":{"desc":null,"major":{"opus":{"jump_url":"//www.bilibili.com/opus/1","summary":{"text":"[表情包_捏捏][表情包_捏捏]"}}}}`,
			want: "发布了图片动态",
		},
		{
			name: "a live card uses its title",
			body: `"module_dynamic":{"desc":null,"major":{"live":{"title":"正在直播：一起玩游戏","jump_url":"//live.bilibili.com/1"}}}`,
			want: "正在直播：一起玩游戏",
		},
		{
			name: "a column uses its title",
			body: `"module_dynamic":{"desc":null,"major":{"article":{"title":"专栏标题","jump_url":"//www.bilibili.com/read/cv1"}}}`,
			want: "专栏标题",
		},
		{
			name:    "emote placeholders never reach the row",
			body:    `"module_dynamic":{"desc":{"text":"[tv_微笑]今天真开心[tv_点赞]"},"major":null}`,
			want:    "今天真开心",
			wantNot: "[tv_微笑]",
		},
		{
			name: "a forwarded post shows its own words",
			body: `"module_dynamic":{"desc":{"text":"转发一下"},"major":null}`,
			want: "转发一下",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items, err := decodeDynamics(1, []byte(feedItem(tc.body)), 1)
			if err != nil {
				t.Fatalf("decode = %v", err)
			}
			if len(items) != 1 {
				t.Fatalf("items = %d, want 1", len(items))
			}
			if got := items[0].Summary; got != tc.want {
				t.Errorf("preview = %q, want %q", got, tc.want)
			}
			if tc.wantNot != "" && strings.Contains(items[0].Summary, tc.wantNot) {
				t.Errorf("preview = %q, want it free of %q", items[0].Summary, tc.wantNot)
			}
		})
	}
}

// A row opens what the post points at, and the timeline entry only when it
// points nowhere.
func TestPreviewLinksPointAtTheEntry(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"a video", `"module_dynamic":{"major":{"archive":{"bvid":"BV1x","title":"t","jump_url":"//www.bilibili.com/video/BV1x"}}}`, "https://www.bilibili.com/video/BV1x"},
		{"a picture post", `"module_dynamic":{"major":{"opus":{"jump_url":"//www.bilibili.com/opus/9"}}}`, "https://www.bilibili.com/opus/9"},
		{"a column", `"module_dynamic":{"major":{"article":{"jump_url":"//www.bilibili.com/read/cv9"}}}`, "https://www.bilibili.com/read/cv9"},
		{"a live room", `"module_dynamic":{"major":{"live":{"jump_url":"//live.bilibili.com/9"}}}`, "https://live.bilibili.com/9"},
		{"nothing in particular", `"module_dynamic":{}`, "https://t.bilibili.com/1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items, err := decodeDynamics(1, []byte(feedItem(tc.body)), 1)
			if err != nil {
				t.Fatal(err)
			}
			if got := items[0].URL; got != tc.want {
				t.Errorf("url = %q, want %q", got, tc.want)
			}
		})
	}
}

// The label beside the creator's name says what kind of post it is.
func TestDynamicKindLabelsThePost(t *testing.T) {
	cases := []struct{ name, rawType, body, want string }{
		{"a video", "DYNAMIC_TYPE_AV", `"module_dynamic":{"major":{"archive":{"bvid":"BV1x"}}}`, "视频"},
		{"a picture post", "DYNAMIC_TYPE_DRAW", `"module_dynamic":{"major":{"opus":{"jump_url":"x"}}}`, "图文"},
		{"a column", "DYNAMIC_TYPE_ARTICLE", `"module_dynamic":{"major":{"article":{"jump_url":"x"}}}`, "专栏"},
		{"a forward", "DYNAMIC_TYPE_FORWARD", `"module_dynamic":{"desc":{"text":"t"}}`, "转发"},
		{"a plain post", "DYNAMIC_TYPE_WORD", `"module_dynamic":{"desc":{"text":"t"}}`, "动态"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := `{"code":0,"data":{"items":[{"id_str":"1","type":"` + tc.rawType + `","modules":{"module_author":{"mid":1,"name":"UP","pub_ts":1700000000},` + tc.body + `}}]}}`
			items, err := decodeDynamics(1, []byte(payload), 1)
			if err != nil {
				t.Fatal(err)
			}
			if got := items[0].Kind; got != tc.want {
				t.Errorf("kind = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStripEmotes(t *testing.T) {
	cases := []struct{ in, want string }{
		{"[tv_微笑]你好", "你好"},
		{"你好[tv_微笑]", "你好"},
		{"a[b]c[d]e", "ace"},
		{"没有表情", "没有表情"},
		{"[未闭合", "[未闭合"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := stripEmotes(tc.in); got != tc.want {
			t.Errorf("stripEmotes(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
