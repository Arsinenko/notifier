package main

import (
	"context"
	"fmt"
	"notifier/config"
	"notifier/internal/folder_watcher"
	"notifier/internal/mail_notifier"
	"notifier/internal/models"
	"notifier/internal/repository"
	"notifier/internal/telegram_bot"
	"notifier/internal/user_api"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/redis/go-redis/v9"
)

func NotesManager(ctx context.Context, cfg *config.Config, bot *tgbotapi.BotAPI, notsChan chan models.Event, eventStore *repository.RedisEventRepository, userStore repository.UserStore) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	cleanupTicker := time.NewTicker(time.Hour)
	defer cleanupTicker.Stop()

	for {
		select {
		case note, ok := <-notsChan:
			if !ok {
				return
			}

			// Сохраняем событие в Redis
			if err := eventStore.AddEvent(ctx, note); err != nil {
				fmt.Printf("Failed to save event: %v\n", err)
				continue
			}

			// Получаем всех пользователей для мгновенных уведомлений
			users, err := userStore.GetAll(ctx)
			if err != nil {
				fmt.Printf("Failed to get users: %v\n", err)
				continue
			}

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

		case <-ticker.C:
			now := time.Now()
			timeStr := now.Format("15:04")

			users, err := userStore.GetAll(ctx)
			if err != nil {
				fmt.Printf("Failed to get users: %v\n", err)
				continue
			}

			for _, user := range users {
				times := strings.Fields(user.Frequency)
				isScheduledTime := false
				for _, t := range times {
					if t == timeStr {
						isScheduledTime = true
						break
					}
				}

				if isScheduledTime {
					// Получаем события с учетом прав пользователя
					events, err := eventStore.GetEventsForUser(ctx, *user, user.LastNotifiedAt, now)
					if err != nil {
						fmt.Printf("Failed to get events for user %d: %v\n", user.ID, err)
						continue
					}

					if len(events) > 0 {
						elems := make([]string, len(events))
						for i, event := range events {
							elems[i] = fmt.Sprintf("%s %s", event.CreatedAt.Format("2006.01.02 15:04"), event.Content)
						}
						responseStr := strings.Join(elems, "\n")
						NotifyUser(*user, cfg, responseStr, bot)

						// Обновляем время последнего уведомления
						if err := userStore.UpdateLastNotified(ctx, user.ID, now); err != nil {
							fmt.Printf("Failed to update last notified for user %d: %v\n", user.ID, err)
						}
					}
				}
			}

		case <-cleanupTicker.C:
			// Удаляем события старше 24 часов
			cleanupTime := time.Now().Add(-24 * time.Hour)
			if err := eventStore.CleanUpOldEvents(ctx, cleanupTime); err != nil {
				fmt.Printf("Failed to cleanup old events: %v\n", err)
			}
		}
	}
}

func NotifyUser(user models.User, cfg *config.Config, note string, bot *tgbotapi.BotAPI) {
	if user.Notifier == models.MailNotifier {
		err := mail_notifier.SendEmail(mail_notifier.EmailAccount{
			Host:     cfg.Mail.Host,
			Port:     cfg.Mail.Port,
			Email:    cfg.Mail.Email,
			Password: cfg.Mail.Password,
		}, user.Email, "Новое уведомление", note)
		if err != nil {
			fmt.Printf("Failed to send email to user %d: %v\n", user.ID, err)
		}
	}
	if user.Notifier == models.TelegramNotifier {
		_, err := bot.Send(tgbotapi.NewMessage(user.ID, note))
		if err != nil {
			fmt.Printf("Failed to send telegram to user %d: %v\n", user.ID, err)
		}
	}
}

func main() {
	cfg, err := config.LoadConfig("./config/config.yaml")
	if err != nil {
		panic(err)
	}

	// Подключение к Redis
	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})

	ctx := context.Background()

	// Проверяем соединение с Redis
	if err := redisClient.Ping(ctx).Err(); err != nil {
		panic(fmt.Sprintf("Failed to connect to Redis: %v", err))
	}

	// Инициализация репозиториев
	eventStore := repository.NewRedisEventRepository(redisClient, 24*time.Hour)
	userStore := repository.NewUserRepo(redisClient)

	bot, err := tgbotapi.NewBotAPI(cfg.Token)
	if err != nil {
		panic(err)
	}

	// Загружаем пользователей из JSON в Redis при первом запуске
	var initialUsers []*models.User
	if cfg.UsersFilepath != "" {
		initialUsers, err = models.LoadFromJson(cfg.UsersFilepath)
		if err == nil && len(initialUsers) > 0 {
			for _, user := range initialUsers {
				if err := userStore.Save(ctx, user); err != nil {
					fmt.Printf("Failed to migrate user %d: %v\n", user.ID, err)
				}
			}
			fmt.Printf("Migrated %d users from JSON to Redis\n", len(initialUsers))
		}
	}

	notsChan := make(chan models.Event)

	go user_api.RunUserAPI("8080", &userStore)
	go telegram_bot.StartBot(cfg, bot, &userStore, eventStore)
	go folder_watcher.Watcher(notsChan, cfg.TargetFolder)
	go NotesManager(ctx, cfg, bot, notsChan, eventStore, &userStore)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	// Сохраняем пользователей из Redis обратно в JSON при завершении
	if cfg.UsersFilepath != "" {
		users, err := userStore.GetAll(ctx)
		if err == nil && len(users) > 0 {
			if err := models.SaveToJson(users, cfg.UsersFilepath); err != nil {
				fmt.Printf("Failed to save users to JSON: %v\n", err)
			} else {
				fmt.Printf("Saved %d users to JSON\n", len(users))
			}
		}
	}

	fmt.Println("Shutting down...")
	close(notsChan)

	if err := redisClient.Close(); err != nil {
		fmt.Printf("Failed to close Redis connection: %v\n", err)
	}

	time.Sleep(time.Second * 2)
}
