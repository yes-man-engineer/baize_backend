package service

import (
	"testing"

	"github.com/yes-man-engineer/baize_backend/internal/model"
)

// 规则 4：涉及具体街道的人流、价格、竞争，一律不许标 green。
// 这条是产品的立身之本——标错一次，本地用户看穿一次，信任就归零。
func TestNormalizeItemNeverGreensLocalFacts(t *testing.T) {
	cases := []struct {
		name    string
		in      model.PlanItem
		want    model.Confidence
		wantAsm string
	}{
		{
			name: "本地人流不许标green",
			in: model.PlanItem{
				Title: "晚上人流", Content: "这条街晚上人流大概每小时 300 人",
				Confidence: model.ConfGreen,
			},
			want: model.ConfRed,
		},
		{
			name: "摊位费不许标green",
			in: model.PlanItem{
				Title: "摊位费", Content: "一个月 800 元", Confidence: model.ConfGreen,
			},
			want: model.ConfRed,
		},
		{
			name: "通用事实可以标green",
			in: model.PlanItem{
				Title: "办证材料", Content: "健康证需要身份证和体检报告",
				Confidence: model.ConfGreen,
			},
			want: model.ConfGreen,
		},
		{
			name: "黄色没假设就降红",
			in: model.PlanItem{
				Title: "定价", Content: "烤脑花卖多少",
				Confidence: model.ConfYellow, Assumption: "",
			},
			want: model.ConfRed,
		},
		{
			name: "黄色带假设保持黄",
			in: model.PlanItem{
				Title: "定价", Content: "烤脑花卖多少",
				Confidence: model.ConfYellow, Assumption: "我估计能卖 8 块一个",
			},
			want:    model.ConfYellow,
			wantAsm: "我估计能卖 8 块一个",
		},
		{
			name: "不认识的值当成待验证",
			in: model.PlanItem{
				Title: "选品", Confidence: "maybe", Assumption: "假设卖烤串",
			},
			want:    model.ConfYellow,
			wantAsm: "假设卖烤串",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			it := c.in
			normalizeItem(&it)

			if it.Confidence != c.want {
				t.Errorf("confidence = %q, 期望 %q", it.Confidence, c.want)
			}
			if it.Assumption != c.wantAsm {
				t.Errorf("assumption = %q, 期望 %q", it.Assumption, c.wantAsm)
			}
			// 黄红一定要有可执行的验证动作，否则任务板上是空白任务
			if it.Confidence != model.ConfGreen && it.VerifyAction == "" {
				t.Error("黄/红条目没有 verify_action，任务板上会出现空白任务")
			}
			// 绿色不该带待办，否则文档视图和任务板对不上
			if it.Confidence == model.ConfGreen && (it.VerifyAction != "" || it.Assumption != "") {
				t.Error("绿色条目不该带假设或待办")
			}
		})
	}
}

// 规则 2：3 条路径的角度必须互不相同，否则等于只给了一条。
func TestSpreadAnglesAlwaysDistinct(t *testing.T) {
	cases := [][]string{
		{"steady", "steady", "steady"},          // 模型全标一个
		{"", "", ""},                            // 全空
		{"随便", "fast_cash", "随便"},               // 混合非法值
		{"high_ceiling", "steady", "fast_cash"}, // 本来就对
		{"fast_cash", "fast_cash", "steady"},    // 部分重复
	}

	for _, in := range cases {
		list := make([]model.PathOption, len(in))
		for i, a := range in {
			list[i] = model.PathOption{Angle: model.PathAngle(a)}
		}

		spreadAngles(list)

		seen := map[model.PathAngle]bool{}
		for _, p := range list {
			if !p.Angle.Valid() {
				t.Errorf("输入 %v：出现非法角度 %q", in, p.Angle)
			}
			if seen[p.Angle] {
				t.Errorf("输入 %v：角度 %q 重复，3 条没拉开", in, p.Angle)
			}
			seen[p.Angle] = true
		}
	}
}

// 规则 3：verdict=stop 时不落创业执行方案，只留帮他止损算账的。
func TestStopSectionsKeepOnlyLossControl(t *testing.T) {
	dropped := []string{"选址", "选品", "定价", "采购设备", "第一周计划", "合规手续"}
	for _, s := range dropped {
		if stopSections[s] {
			t.Errorf("劝退时不该保留执行类 section: %q", s)
		}
	}
	for _, s := range []string{"风险", "启动资金", "成本毛利"} {
		if !stopSections[s] {
			t.Errorf("劝退时应保留止损类 section: %q", s)
		}
	}
}
