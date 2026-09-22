package model

import "time"

type Confidence string

const (
	// ConfGreen 确定：不依赖本地的通用事实，直接给
	ConfGreen Confidence = "green"
	// ConfYellow 我猜的：和本地强相关，先给假设再让用户核实
	ConfYellow Confidence = "yellow"
	// ConfRed 只有用户能填：AI 无从得知，不给假设
	ConfRed Confidence = "red"
)

func (c Confidence) Valid() bool {
	return c == ConfGreen || c == ConfYellow || c == ConfRed
}

// PlanItem 方案里的一条。文档视图渲染全部条目，
// 任务板只取 yellow / red —— 同一份数据的两个视图。
type PlanItem struct {
	Base

	ProjectID string `gorm:"type:char(36);index;not null" json:"project_id"`
	Section   string `gorm:"type:varchar(50);not null" json:"section"`
	Title     string `gorm:"type:varchar(200);not null" json:"title"`
	Content   string `gorm:"type:text" json:"content"`

	Confidence Confidence `gorm:"type:varchar(10);not null" json:"confidence"`
	// Assumption 黄色项的假设值，用来让用户去反驳
	Assumption string `gorm:"type:text" json:"assumption"`
	// VerifyAction 今晚两小时内能做完的具体动作
	VerifyAction string `gorm:"type:text" json:"verify_action"`

	// Answer 用户回填的真实数据，填了就变绿
	Answer     string     `gorm:"type:text" json:"answer"`
	VerifiedAt *time.Time `json:"verified_at,omitempty"`

	SortOrder int `gorm:"not null;default:0" json:"sort_order"`
}

func (PlanItem) TableName() string { return "plan_items" }

// IsTask 是否出现在任务板上。
func (p *PlanItem) IsTask() bool {
	return p.Confidence == ConfYellow || p.Confidence == ConfRed
}
