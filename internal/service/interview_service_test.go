package service

import "testing"

// 亏损上限是整份方案的尺子，解析错了方案就废了。
// 这里锁住两件事：该抠出来的抠得出来，不该抠的一个都不许抠进来。
func TestParseMoney(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		allowBare bool
		want      int
		wantOK    bool
	}{
		// 第一轮：问的就是钱，裸数字也认
		{"裸数字", "5000", true, 5000, true},
		{"带块", "5000块", true, 5000, true},
		{"万", "1万左右", true, 10000, true},
		{"小写w", "2w", true, 20000, true},
		{"中文两万", "两万", true, 20000, true},
		{"中文三五千取小头", "三五千", true, 3000, true},
		{"一句话里有干扰数字", "我今年35岁，最多亏5000块", true, 5000, true},
		{"多个金额取最小", "我有2万存款，最多亏5千", true, 5000, true},
		{"不知道", "不知道", true, 0, false},
		{"没想过", "没想过这个", true, 0, false},

		// 后续轮次：没带金额单位的一律不认，否则会污染成 3 元、20 元
		{"城市和年限不算钱", "我在杭州，做了3年销售", false, 0, false},
		{"工时不算钱", "每周大概能干20个小时", false, 0, false},
		{"年龄不算钱", "我今年35岁", false, 0, false},
		{"后续轮次补充金额仍然认", "哦对了，我最多能亏5000块", false, 5000, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parseMoney(c.input, c.allowBare)
			if ok != c.wantOK || got != c.want {
				t.Errorf("parseMoney(%q, %v) = (%d, %v), 期望 (%d, %v)",
					c.input, c.allowBare, got, ok, c.want, c.wantOK)
			}
		})
	}
}
