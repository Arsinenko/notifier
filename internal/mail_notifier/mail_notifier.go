package mail_notifier

import (
	"fmt"
	"log"
	"net/smtp"
)

// EmailAccount содержит настройки SMTP-сервера и учетные данные
type EmailAccount struct {
	Host     string
	Port     int
	Email    string
	Password string
}

func SendEmail(acc EmailAccount, target string, subject string, content string) error {
	auth := smtp.PlainAuth("", acc.Email, acc.Password, acc.Host)
	addr := fmt.Sprintf("%s:%d", acc.Host, acc.Port)

	message := []byte(fmt.Sprintf("To: %s\r\n"+
		"Subject: %s\r\n"+
		"Content-Type: text/plain; charset=\"utf-8\"\r\n"+
		"\r\n"+
		"%s\r\n", target, subject, content))
	log.Println(string(message))

	err := smtp.SendMail(addr, auth, acc.Email, []string{target}, message)
	if err != nil {
		return fmt.Errorf("ошибка при отправке email: %w", err)
	}

	return nil
}
