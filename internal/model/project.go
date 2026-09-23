package model

import "time"

type ProjectStatus string

const (
	// StatusScouting 入口 B 的盘点阶段：先摸清他手上现成有什么
	StatusScouting ProjectStatus = "scouting"
	// StatusChoosing 已给出 3 条候选路径，等用户选一条
	StatusChoosing ProjectStatus = "choosing"
	// StatusInterviewing 主干提问阶段，还没出方案
	StatusInterviewing ProjectStatus = "interviewing"
	// StatusPlanned 方案已生成，用户在验证推进
	StatusPlanned ProjectStatus = "planned"
	// StatusEnded 用户主动点了「项目结束」
	StatusEnded ProjectStatus = "ended"
)

type Entry string

const (
	// EntryHasIdea 入口 A：已经有想法，直接进主干
	EntryHasIdea Entry = "A"
	// EntryNoIdea 入口 B：不知道能做什么，先盘点再选路
	EntryNoIdea Entry = "B"
)

type Verdict string

const (
	VerdictUnset Verdict = ""
	// VerdictGo 可以起步
	VerdictGo Verdict = "go"
	// VerdictStop 劝退：以当前情况不该创业
	VerdictStop Verdict = "stop"
)

// Project 一个项目 = 用户从模糊想法走到起步的一整段过程。
// 没有账号体系，Token 就是访问凭证，前端存在链接里。
type Project struct {
	Base

	Token  string        `gorm:"type:char(32);uniqueIndex;not null" json:"token"`
	Status ProjectStatus `gorm:"type:varchar(20);not null;default:'interviewing'" json:"status"`
	Entry  Entry         `gorm:"type:varchar(2);not null;default:'A'" json:"entry"`

	// 提问阶段沉淀下来的结构化事实
	Idea        string `gorm:"type:varchar(500)" json:"idea"`
	City        string `gorm:"type:varchar(200)" json:"city"`
	RiskBudget  int    `gorm:"not null;default:-1" json:"risk_budget"`  // 打算投入的预算（元），-1 表示还没问到
	WeeklyHours int    `gorm:"not null;default:-1" json:"weekly_hours"` // 每周能投入几小时，-1 表示还没问到

	// 入口 B 的盘点结果。创业的起点不是「我会什么」，是「我手上现成有什么」。
	Assets     string `gorm:"type:text" json:"assets"`     // 车、场地、设备、能借到的人和东西
	Experience string `gorm:"type:text" json:"experience"` // 做过什么、别人常找他帮什么忙

	SelectedPathID string `gorm:"type:char(36);not null;default:''" json:"selected_path_id"`

	// 产品底线：方案可以是「先别做」
	Verdict       Verdict `gorm:"type:varchar(10);not null;default:''" json:"verdict"`
	VerdictReason string  `gorm:"type:text" json:"verdict_reason"`

	AskedCount int        `gorm:"not null;default:0" json:"asked_count"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
}

func (Project) TableName() string { return "projects" }

// Answering 是否处在需要用户继续回答问题的阶段。
func (p *Project) Answering() bool {
	return p.Status == StatusScouting || p.Status == StatusInterviewing
}
