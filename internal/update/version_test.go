package update

import "testing"

// Compare 按发布源与运行构建必须遵守的规则排序版本。首个用例就是当年坏掉的那个：
// 构建把自己的 release 读回来，必须被判为同一版本，而不是更新。
func TestCompare(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		// 同一版本，无论两边各自怎么拼写。
		{"0.1.1", "0.1.1", 0},
		{"v0.1.1", "0.1.1", 0},
		{"0.1.1", "v0.1.1", 0},
		{"0.1.1+build.7", "0.1.1", 0},
		{"0.1.0-beta.5", "0.1.0-beta.5", 0},

		// 更新的 release。
		{"0.1.2", "0.1.1", 1},
		{"0.2.0", "0.1.9", 1},
		{"1.0.0", "0.9.9", 1},
		{"0.10.0", "0.9.0", 1},
		{"0.1.1", "0.1.0-beta.5", 1},

		// 更旧的 release，包括因是被运行版本的预发布而更旧的。
		{"0.1.0", "0.1.1", -1},
		{"0.1.0-beta.5", "0.1.0", -1},
		{"0.1.0-beta.4", "0.1.0-beta.5", -1},
		{"0.1.0-alpha", "0.1.0-beta", -1},

		// 提前结束的版本小于继续它的版本。
		{"0.1", "0.1.0", -1},
		{"0.1.0", "0.1", 1},

		// 不得 panic 或给出荒谬结果的边界。
		{"", "", 0},
		{"", "0.0.1", -1},
		{"0.0.1", "", 1},
		// "dev" 是单词，单词排在数字之后：开发构建得到发布源的 release，
		// 而不是被判为更新。
		{"dev", "0.1.1", 1},
		{"0.1.1", "dev", -1},
	} {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
		// 该比较在正反两个方向上都是全序。
		if got := Compare(c.b, c.a); got != -c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.b, c.a, got, -c.want)
		}
	}
}

// 运行版本高于发布源的构建（本地构建，或被撤回的 release）视为已是最新，而不提供降级。
func TestCompareDoesNotOfferADowngrade(t *testing.T) {
	if got := Compare("0.1.1", "0.1.2"); got >= 0 {
		t.Errorf("Compare(0.1.1, 0.1.2) = %d, want the feed to be older", got)
	}
}
