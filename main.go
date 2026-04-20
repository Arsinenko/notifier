package main

import (
	"fmt"
	"notifier/config"
	"notifier/internal/folder_watcher"
	"notifier/internal/mail_notifier"
	models2 "notifier/internal/models"
	"notifier/internal/telegram_bot"
	"notifier/internal/user_api"
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

	// Таймер для очистки старых ивентов (например, каждый час)
	cleanupTicker := time.NewTicker(time.Hour)
	defer cleanupTicker.Stop()

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
			respNote := fmt.Sprintf("%s %s", note.CreatedAt.Format("2006.01.02 15:04"), note.Content)
			for _, user := range users {
				if user.Frequency == "Immediate" {

					for _, perm := range user.Permissions {
						if string(perm) == strings.ToUpper(note.Op.String()) {
							NotifyUser(*user, cfg, respNote, bot)
						}
					}
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

		case <-cleanupTicker.C:
			cleanupOldEvents()
		}
	}
}

// cleanupOldEvents удаляет ивенты, которые старше чем любой из user.LastNotifiedAt
func cleanupOldEvents() {
	usersMu.RLock()
	defer usersMu.RUnlock()

	// Находим минимальную (самую старую) дату LastNotifiedAt среди всех пользователей
	var minLastNotifiedAt time.Time
	hasUsers := false

	for _, user := range users {
		if !user.LastNotifiedAt.IsZero() {
			if !hasUsers || user.LastNotifiedAt.Before(minLastNotifiedAt) {
				minLastNotifiedAt = user.LastNotifiedAt
				hasUsers = true
			}
		}
	}

	// Если нет пользователей или ни у одного нет LastNotifiedAt, ничего не удаляем
	if !hasUsers {
		return
	}

	eventsMu.Lock()
	defer eventsMu.Unlock()

	// Фильтруем ивенты, оставляем только те, которые новее minLastNotifiedAt
	newEvents := make([]models2.Event, 0, len(events))
	removedCount := 0

	for _, event := range events {
		if event.CreatedAt.After(minLastNotifiedAt) {
			newEvents = append(newEvents, event)
		} else {
			removedCount++
		}
	}

	if removedCount > 0 {
		events = newEvents
		fmt.Printf("Cleaned up %d old events (older than %s)\n",
			removedCount, minLastNotifiedAt.Format("2006-01-02 15:04:05"))
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
	go user_api.RunUserAPI("8080", &users, &usersMu)

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
