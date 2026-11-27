package update

import (
	"strconv"
	"strings"
)

// Compare 按语义化版本比较两个版本字符串，返回 -1、0 或 1。
//
// 它的存在是为了让「release 是否比运行版本新」由一条规则回答而非字符串比较。
// 发布源可能给出正在询问的构建的版本——最新发布的就是当前安装所跑的——用 == 或
// 普通排序比较，要么提供重装同版本，要么把 "v0.1.1" 与 "0.1.1" 判成不同版本。
//
// 规则即语义化版本：前导 "v" 与 "+build" 后缀不属于版本号；两边都是数字时按数值
// 比较；预发布早于其正式版；提前结束的版本小于继续的版本（"0.1" < "0.1.0"）。
//
// 它也是 MyGo 自身 updater 在提供更新前所做的比较，main.go 的适配器因此使用它：
// 本包判决的「更新」与框架安装的必须一致。
func Compare(a, b string) int {
	aRelease, aPrerelease := splitVersion(a)
	bRelease, bPrerelease := splitVersion(b)
	if c := compareFields(aRelease, bRelease); c != 0 {
		return c
	}
	return comparePrerelease(aPrerelease, bPrerelease)
}

// splitVersion 把版本拆成排序用的两部分：发布号与预发布标签。前导 "v" 是标签
// 的写法，"+build" 后缀说明构建方式；二者都不改变是哪个版本。
func splitVersion(version string) (release, prerelease string) {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	version, _, _ = strings.Cut(version, "+")
	release, prerelease, _ = strings.Cut(version, "-")
	return release, prerelease
}

// comparePrerelease 排序两个预发布标签。缺席的排在所有存在的之后，因为正式版
// 新于它自己的预发布。
func comparePrerelease(a, b string) int {
	switch {
	case a == b:
		return 0
	case a == "":
		return 1
	case b == "":
		return -1
	}
	return compareFields(a, b)
}

// compareFields 逐字段比较两个点分隔的版本部分。数字按数值比；数字字段排在
// 单词字段前；两个单词按字符串比；一方缺的字段使该方更小。
func compareFields(a, b string) int {
	af, bf := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(af) || i < len(bf); i++ {
		var x, y string
		if i < len(af) {
			x = af[i]
		}
		if i < len(bf) {
			y = bf[i]
		}
		xn, xerr := strconv.Atoi(x)
		yn, yerr := strconv.Atoi(y)
		switch {
		case x == y:
			continue
		case x == "":
			return -1
		case y == "":
			return 1
		case xerr == nil && yerr == nil:
			if xn < yn {
				return -1
			}
			if xn > yn {
				return 1
			}
		case xerr == nil:
			return -1 // 数字排在单词之前
		case yerr == nil:
			return 1
		default:
			return strings.Compare(x, y)
		}
	}
	return 0
}
