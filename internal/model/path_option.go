package model

type PathAngle string

const (
	// AngleSteady 最稳：投入小、风险低，但天花板也低
	AngleSteady PathAngle = "steady"
	// AngleFastCash 最快回钱：优先解决现金流
	AngleFastCash PathAngle = "fast_cash"
	// AngleHighCeiling 天花板最高：起步慢，但能做大
	AngleHighCeiling PathAngle = "high_ceiling"
)

func (a PathAngle) Valid() bool {
	return a == AngleSteady || a == AngleFastCash || a == AngleHighCeiling
}

// PathOption 入口 B 的候选路径。
// 固定给 3 条：给 1 条等于替用户做决定，给 10 条等于没给。
// 3 条之间必须差异明显，所以用 Angle 把它们拉开。
type PathOption struct {
	Base

	ProjectID string    `gorm:"type:char(36);index;not null" json:"project_id"`
	Title     string    `gorm:"type:varchar(200);not null" json:"title"`
	Angle     PathAngle `gorm:"type:varchar(20);not null" json:"angle"`
	Summary   string    `gorm:"type:text" json:"summary"`
	// WhyYou 为什么以他现成的东西能启动这条，而不是泛泛而谈
	WhyYou string `gorm:"type:text" json:"why_you"`
	// StartupCost 预估启动投入（元），必须在用户的亏损上限之内
	StartupCost int    `gorm:"not null;default:0" json:"startup_cost"`
	FirstStep   string `gorm:"type:text" json:"first_step"`

	SortOrder int `gorm:"not null;default:0" json:"sort_order"`
}

func (PathOption) TableName() string { return "path_options" }
