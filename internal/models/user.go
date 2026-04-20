package models

import (
	"encoding/json"
	"notifier/internal/permissions"
	"os"
	"time"
)

type NotifierType string

const (
	MailNotifier     NotifierType = "mail"
	TelegramNotifier NotifierType = "telegram"
)

type User struct {
	ID               int64                    `json:"id"`
	UserLabel        string                   `json:"user_label"`
	Frequency        string                   `json:"frequency"`
	Notifier         NotifierType             `json:"notifier"`
	Email            string                   `json:"email"`
	VerificationCode string                   `json:"verification_code"` // Храним код для проверки
	IsVerified       bool                     `json:"is_verified"`
	LastNotifiedAt   time.Time                `json:"last_notified_at"`
	Permissions      []permissions.Permission `json:"permissions"`
}

func SaveToJson(users []*User, filepath string) error {
	marshal, err := json.Marshal(users)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath, marshal, 0644)
}

func LoadFromJson(filepath string) ([]*User, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var users []*User
	err = json.NewDecoder(file).Decode(&users)
	if err != nil {
		return nil, err
	}
	return users, nil
}
