package main

import (
	"fmt"
	"notifier/config"
	"notifier/internal/folder_watcher"
	"notifier/internal/mail_notifier"
	models2 "notifier/internal/models"
	"notifier/telegram_bot"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var (
	eventsMu sync.RWMutex
	usersMu  sync.RWMutex
	events   = make([]models2.Event, 0)
	users    = make([]*models2.User, 0)
)

func NotesManager(cfg *config.Config, bot *tgbotapi.BotAPI, notsChan chan models2.Event) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case note, ok := <-notsChan:
			if !ok {
				return
			}
			eventsMu.Lock()
			events = append(events, note)
			eventsMu.Unlock()

			usersMu.RLock()
			for _, user := range users {
				if user.Frequency == "Immediate" {
					NotifyUser(*user, cfg, note.Content, bot)
				}
			}
			usersMu.RUnlock()

		case <-ticker.C:
			now := time.Now()
			timeStr := now.Format("15:04")

			usersMu.RLock()
			for _, user := range users {
				// Изменено: проверяем наличие конкретного времени в строке через пробелы или границы
				times := strings.Fields(user.Frequency)
				isScheduledTime := false
				for _, t := range times {
					if t == timeStr {
						isScheduledTime = true
						break
					}
				}

				if isScheduledTime {
					eventsMu.RLock()
					elems := models2.GetEvents(events, user.LastNotifiedAt, now)
					eventsMu.RUnlock()

					if len(elems) > 0 {
						responseStr := strings.Join(elems, "\n")
						NotifyUser(*user, cfg, responseStr, bot)
						user.LastNotifiedAt = now
					}
				}
			}
			usersMu.RUnlock()
		}
	}
}

func NotifyUser(user models2.User, cfg *config.Config, note string, bot *tgbotapi.BotAPI) {
	if user.Notifier == models2.MailNotifier {
		err := mail_notifier.SendEmail(mail_notifier.EmailAccount{
			Host:     cfg.Mail.Host,
			Port:     cfg.Mail.Port,
			Email:    cfg.Mail.Email,
			Password: cfg.Mail.Password,
		}, user.Email, "Новое уведомление", note)
		if err != nil {
			fmt.Println(err)
			return
		}
	}
	if user.Notifier == models2.TelegramNotifier {
		_, err := bot.Send(tgbotapi.NewMessage(user.ID, note))
		if err != nil {
			fmt.Println(err)
			return
		}
	}
}

func main() {
	cfg, err := config.LoadConfig("./config/config.yaml")
	if err != nil {
		panic(err)
	}
	bot, err := tgbotapi.NewBotAPI(cfg.Token)
	if err != nil {
		panic(err)
	}
	users, err = models2.LoadFromJson(cfg.UsersFilepath)
	if err != nil {
		users = make([]*models2.User, 0)
	}

	notsChan := make(chan models2.Event)

	go telegram_bot.StartBot(cfg, bot, &users, &usersMu)
	go folder_watcher.Watcher(notsChan, cfg.TargetFolder)
	go NotesManager(cfg, bot, notsChan)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	models2.SaveToJson(users, cfg.UsersFilepath)

	fmt.Println("Shutting down...")
	close(notsChan)
	time.Sleep(time.Second * 2)
}
