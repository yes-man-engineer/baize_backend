package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

type ProjectStatus string

const (
	// StatusChatting 还在聊
	StatusChatting ProjectStatus = "chatting"
	// StatusReady 该问的都问到了，可以出方案。但用户想接着聊也随他，
	// 这是"可以了"不是"到此为止"。
	StatusReady ProjectStatus = "ready"
	// StatusPlanned 方案已生成
	StatusPlanned ProjectStatus = "planned"
	// StatusEnded 用户主动结束
	StatusEnded ProjectStatus = "ended"
)

// Chatting 还能不能继续聊。聊够了不等于不让聊了。
func (s ProjectStatus) Chatting() bool {
	return s == StatusChatting || s == StatusReady
}

// Facts 模型从对话里抽出来的已确认信息。
//
// 键不固定：卖烧烤记「出摊时段」，做皮具记「货源哪来的」，这些列不完，
// 也不该由表结构提前框死。模型觉得哪件事会影响方案就记哪件。
//
// 值一律存原话（"5000 元" 而不是 5000）。存成数字，代码就会忍不住拿去算、
// 去比较、去校验，然后又长出一个按关键词猜语义的解析函数。
type Facts map[string]string

func (f Facts) Value() (driver.Value, error) {
	if f == nil {
		return "{}", nil
	}
	b, err := json.Marshal(f)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

func (f *Facts) Scan(src any) error {
	var b []byte
	switch v := src.(type) {
	case nil:
		*f = Facts{}
		return nil
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return fmt.Errorf("facts 读出来的类型不认识: %T", src)
	}
	if len(b) == 0 {
		*f = Facts{}
		return nil
	}
	return json.Unmarshal(b, f)
}

// Project 没有账号体系，ID 就是访问凭证：拿到这串 uuid 就能看这个项目。
// uuid v4 有 122 位随机，猜不出来，不必再单设一个凭证列——
// 这个项目里 token 一词已经被大模型的 prompt_tokens / completion_tokens 占了。
type Project struct {
	Base

	// UserID 现在恒为空。留着是为了将来接上账号时，老项目还能认领回去——
	// 那时候再加列，已有的行就没法回填归属了。
	UserID string `gorm:"type:char(36);not null;default:''" json:"user_id"`

	Status ProjectStatus `gorm:"type:varchar(20);not null;default:'chatting'" json:"status"`

	// Title 建项目时从用户第一条消息截出来，立刻就有，不用等模型。
	Title string `gorm:"type:varchar(100);not null;default:''" json:"title"`

	Facts Facts `gorm:"type:json" json:"facts"`
}

func (Project) TableName() string { return "projects" }
