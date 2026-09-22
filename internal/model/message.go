package model

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message 提问阶段的一问一答。
type Message struct {
	Base

	ProjectID string `gorm:"type:char(36);index;not null" json:"project_id"`
	Role      Role   `gorm:"type:varchar(16);not null" json:"role"`
	Content   string `gorm:"type:text;not null" json:"content"`
}

func (Message) TableName() string { return "messages" }
