package model

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	Base

	ProjectID string `gorm:"type:char(36);index:idx_messages_project_id;not null" json:"project_id"`
	Role      Role   `gorm:"type:varchar(10);not null" json:"role"`
	Content   string `gorm:"type:text;not null" json:"content"`
}

func (Message) TableName() string { return "messages" }
