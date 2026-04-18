package telegram_bot

import (
	"fmt"
	"math/rand"
	"notifier/config"
	"notifier/internal/mail_notifier"
	"notifier/internal/models"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func StartBot(cfg *config.Config, bot *tgbotapi.BotAPI, users *[]*models.User, mu *sync.RWMutex) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message != nil {
			handleMessage(cfg, bot, update.Message, users, mu)
		} else if update.CallbackQuery != nil {
			handleCallback(cfg, bot, update.CallbackQuery, users, mu)
		}
	}
}

func handleMessage(cfg *config.Config, bot *tgbotapi.BotAPI, msg *tgbotapi.Message, users *[]*models.User, mu *sync.RWMutex) {
	if msg.Text == "/start" || msg.Text == "/edit" {
		showFrequencyMenu(bot, msg.Chat.ID)
		return
	}

	mu.Lock()
	user := findUserByID(*users, msg.From.ID)

	// Проверяем, является ли ввод временем (формат HH:MM или список через пробел)
	if user != nil && (strings.Contains(msg.Text, ":") || msg.Text == "Immediate") {
		user.Frequency = msg.Text
		mu.Unlock()

		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Telegram", "notif_tg"),
				tgbotapi.NewInlineKeyboardButtonData("Email", "notif_mail"),
			),
		)
		resp := tgbotapi.NewMessage(msg.Chat.ID, "Настройки обновлены ("+msg.Text+"). Куда присылать уведомления?")
		resp.ReplyMarkup = keyboard
		bot.Send(resp)
		return
	}
	mu.Unlock()

	mu.RLock()
	user = findUserByID(*users, msg.From.ID)
	mu.RUnlock()

	if user == nil {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Пожалуйста, начните с /start"))
		return
	}

	if user.Notifier == models.MailNotifier && !user.IsVerified {
		if user.Email == "" {
			user.Email = msg.Text
			code := fmt.Sprintf("%04d", rand.Intn(10000))
			user.VerificationCode = code

			go mail_notifier.SendEmail(mail_notifier.EmailAccount{
				Host:     cfg.Mail.Host,
				Port:     cfg.Mail.Port,
				Email:    cfg.Mail.Email,
				Password: cfg.Mail.Password,
			}, user.Email, "Код подтверждения", "Ваш код: "+code)

			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("Я отправил код на %s. Введите его здесь:", msg.Text)))
		} else {
			if msg.Text == user.VerificationCode {
				user.IsVerified = true
				user.LastNotifiedAt = time.Now()
				bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "✅ Почта подтверждена! Настройки сохранены."))
			} else {
				bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ Неверный код. Попробуйте еще раз:"))
			}
		}
	}
}

func handleCallback(cfg *config.Config, bot *tgbotapi.BotAPI, cb *tgbotapi.CallbackQuery, users *[]*models.User, mu *sync.RWMutex) {
	mu.Lock()
	user := findUserByID(*users, cb.From.ID)
	if user == nil {
		user = &models.User{
			ID: cb.From.ID,
		}
		*users = append(*users, user)
	}
	mu.Unlock()

	if strings.HasPrefix(cb.Data, "freq_") {
		freq := cb.Data[5:]

		if freq == "custom" {
			bot.Send(tgbotapi.NewMessage(cb.Message.Chat.ID, "Введите время через пробел (например: 09:00 14:30 21:00):"))
			bot.Request(tgbotapi.NewCallback(cb.ID, ""))
			return
		}

		if freq == "immediate" {
			user.Frequency = "Immediate"
		} else {
			user.Frequency = freq
		}

		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Telegram", "notif_tg"),
				tgbotapi.NewInlineKeyboardButtonData("Email", "notif_mail"),
			),
		)
		editMsg := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, "Выбрано: "+user.Frequency+". Куда присылать уведомления?")
		editMsg.ReplyMarkup = &keyboard
		bot.Send(editMsg)
	}

	if cb.Data == "notif_tg" {
		user.Notifier = models.TelegramNotifier
		user.IsVerified = true
		user.LastNotifiedAt = time.Now()
		bot.Send(tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, "Готово! Буду писать в Telegram.\nРасписание: "+user.Frequency+"\nИзменить: /edit"))
	}

	if cb.Data == "notif_mail" {
		user.Notifier = models.MailNotifier
		user.Email = ""
		bot.Send(tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, "Введите ваш Email:"))
	}

	bot.Request(tgbotapi.NewCallback(cb.ID, ""))
}

func showFrequencyMenu(bot *tgbotapi.BotAPI, chatID int64) {
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("Немедленно", "freq_immediate")),
		tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("09:00", "freq_09:00")),
		tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("09:00 18:00", "freq_09:00 18:00")),
		tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("Свой вариант", "freq_custom")),
	)
	msg := tgbotapi.NewMessage(chatID, "Выберите или введите новое расписание уведомлений:")
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

func findUserByID(users []*models.User, id int64) *models.User {
	for _, u := range users {
		if u.ID == id {
			return u
		}
	}
	return nil
}
