package telegram_bot

import (
	"context"
	"fmt"
	"math/rand"
	"notifier/config"
	"notifier/internal/mail_notifier"
	"notifier/internal/models"
	"notifier/internal/repository"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type BotHandler struct {
	cfg        *config.Config
	bot        *tgbotapi.BotAPI
	userStore  repository.UserStore
	eventStore repository.EventStore
	ctx        context.Context
}

func StartBot(cfg *config.Config, bot *tgbotapi.BotAPI, userStore repository.UserStore, eventStore repository.EventStore) {
	handler := &BotHandler{
		cfg:        cfg,
		bot:        bot,
		userStore:  userStore,
		eventStore: eventStore,
		ctx:        context.Background(),
	}

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message != nil {
			handler.handleMessage(update.Message)
		} else if update.CallbackQuery != nil {
			handler.handleCallback(update.CallbackQuery)
		}
	}
}

func (h *BotHandler) handleMessage(msg *tgbotapi.Message) {
	// Обработка команд помощи
	if msg.Text == "/help" {
		h.showHelp(msg.Chat.ID)
		return
	}

	if msg.Text == "/help events" {
		h.showEventsHelp(msg.Chat.ID)
		return
	}

	if msg.Text == "/start" || msg.Text == "/edit" {
		showFrequencyMenu(h.bot, msg.Chat.ID)
		return
	}

	// Команда для получения событий
	if strings.HasPrefix(msg.Text, "/events") {
		h.handleEventsCommand(msg)
		return
	}

	// Получаем пользователя из Redis
	user, err := h.userStore.GetByID(h.ctx, msg.From.ID)
	if err != nil {
		// Пользователь не найден, создаем нового
		user = &models.User{
			ID: msg.From.ID,
		}
		if err := h.userStore.Save(h.ctx, user); err != nil {
			h.bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Ошибка при создании пользователя"))
			return
		}
	}

	// 1. Обработка ввода времени (Frequency)
	if strings.Contains(msg.Text, ":") || msg.Text == "Immediate" {
		user.Frequency = msg.Text
		if err := h.userStore.Save(h.ctx, user); err != nil {
			h.bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Ошибка при сохранении настроек"))
			return
		}

		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Telegram", "notif_tg"),
				tgbotapi.NewInlineKeyboardButtonData("Email", "notif_mail"),
			),
		)
		resp := tgbotapi.NewMessage(msg.Chat.ID, "Настройки сохранены. Куда присылать уведомления?")
		resp.ReplyMarkup = keyboard
		h.bot.Send(resp)
		return
	}

	// 2. Обработка ввода Email
	if user.Notifier == models.MailNotifier && !user.IsVerified {
		if user.Email == "" {
			user.Email = msg.Text
			code := fmt.Sprintf("%04d", rand.Intn(10000))
			user.VerificationCode = code

			// Сохраняем пользователя с кодом
			if err := h.userStore.Save(h.ctx, user); err != nil {
				h.bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Ошибка при сохранении"))
				return
			}

			// Отправляем код на email
			go mail_notifier.SendEmail(mail_notifier.EmailAccount{
				Host:     h.cfg.Mail.Host,
				Port:     h.cfg.Mail.Port,
				Email:    h.cfg.Mail.Email,
				Password: h.cfg.Mail.Password,
			}, user.Email, "Код подтверждения", "Ваш код: "+code)

			h.bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Код отправлен на "+msg.Text+". Введите его:"))
		} else {
			if msg.Text == user.VerificationCode {
				user.IsVerified = true
				if err := h.userStore.Save(h.ctx, user); err != nil {
					h.bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Ошибка при подтверждении"))
					return
				}
				h.bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "✅ Почта подтверждена! Теперь введите ваше имя (Label) для системы:"))
			} else {
				h.bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ Неверный код. Попробуйте еще раз:"))
			}
		}
		return
	}

	// 3. Обработка ввода UserLabel
	if user.IsVerified && user.UserLabel == "" {
		user.UserLabel = msg.Text
		user.LastNotifiedAt = time.Now()

		if err := h.userStore.Save(h.ctx, user); err != nil {
			h.bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Ошибка при сохранении имени"))
			return
		}

		h.bot.Send(tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("Принято, %s! Все настройки завершены.\nРасписание: %s\nИзменить: /edit\n\nДля получения справки используйте /help",
			user.UserLabel, user.Frequency)))
		return
	}
}

func (h *BotHandler) showHelp(chatID int64) {
	helpText := `🤖 *Доступные команды бота:*

*Основные команды:*
/start - Начать настройку бота
/edit - Изменить настройки уведомлений
/help - Показать эту справку

*Команды для работы с событиями:*
/events - Показать события за сегодня
/help events - Подробная справка по командам событий

*Описание возможностей:*
📅 *Уведомления*
Бот может присылать уведомления в Telegram или на Email
Вы можете настроить расписание: немедленно, в определенное время или свое расписание

🔔 *Получение событий*
С помощью команды /events вы можете просматривать историю событий за разные периоды

*Примеры использования:*
1. Настройка: /start
2. Изменение настроек: /edit
3. Просмотр событий: /events week

❓ Для получения подробной информации о команде /events введите: /help events`

	msg := tgbotapi.NewMessage(chatID, helpText)
	msg.ParseMode = "Markdown"
	h.bot.Send(msg)
}

func (h *BotHandler) showEventsHelp(chatID int64) {
	helpText := `📅 *Команда /events - получение событий*

*Синтаксис:*
/events [период]

*Доступные периоды:*
• (без параметра) или today - события за сегодня
• yesterday - события за вчера
• week - события за текущую неделю (пн-вс)
• month - события за текущий месяц
• YYYY-MM-DD - события за конкретную дату

*Примеры:*
/events - события за сегодня
/events yesterday - события за вчера
/events week - события за неделю
/events month - события за месяц
/events 2024-01-15 - события за 15 января 2024

*Формат вывода:*
События группируются по дням и показываются с указанием времени и источника (если доступно)

*Примечания:*
• Если вы указали Label при настройке, будут показаны только ваши события
• Без Label показываются все события системы
• Сообщения длиннее 4096 символов автоматически разбиваются на части

*Пример вывода:*
📊 События за период 15.01.2024 - 15.01.2024:

*15.01.2024:*
  ⏰ 09:30 - Системное уведомление (from: system)
  ⏰ 14:15 - Важное событие

❓ Основная справка: /help`

	msg := tgbotapi.NewMessage(chatID, helpText)
	msg.ParseMode = "Markdown"
	h.bot.Send(msg)
}

func (h *BotHandler) handleEventsCommand(msg *tgbotapi.Message) {
	// Получаем пользователя
	user, err := h.userStore.GetByID(h.ctx, msg.From.ID)
	if err != nil {
		h.bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ Пользователь не найден. Используйте /start для настройки."))
		return
	}

	// Разбираем аргументы команды
	args := strings.Fields(msg.Text)
	var period string
	if len(args) > 1 {
		period = strings.ToLower(args[1])
	} else {
		period = "today"
	}

	// Вычисляем временной диапазон
	var from, to time.Time
	now := time.Now()

	switch period {
	case "today", "сегодня":
		from = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		to = from.Add(24 * time.Hour)
	case "yesterday", "вчера":
		from = time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, now.Location())
		to = from.Add(24 * time.Hour)
	case "week", "неделя":
		weekday := now.Weekday()
		if weekday == time.Sunday {
			weekday = 7
		}
		from = now.AddDate(0, 0, -int(weekday)+1)
		from = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location())
		to = from.Add(7 * 24 * time.Hour)
	case "month", "месяц":
		from = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		to = from.AddDate(0, 1, 0)
	default:
		// Пробуем распарсить пользовательскую дату
		parsedDate, err := time.Parse("2006-01-02", period)
		if err == nil {
			from = time.Date(parsedDate.Year(), parsedDate.Month(), parsedDate.Day(), 0, 0, 0, 0, parsedDate.Location())
			to = from.Add(24 * time.Hour)
		} else {
			h.bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ Неверный формат. Используйте: /events [today|yesterday|week|month|YYYY-MM-DD]\nДля справки: /help events"))
			return
		}
	}

	// Получаем события
	var events []models.Event
	if user.UserLabel != "" {
		events, err = h.eventStore.GetEventsForUser(h.ctx, *user, from, to)
	} else {
		events, err = h.eventStore.GetEvents(h.ctx, from, to)
	}

	if err != nil {
		h.bot.Send(tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("❌ Ошибка при получении событий: %v", err)))
		return
	}

	// Форматируем ответ
	if len(events) == 0 {
		var periodText string
		switch period {
		case "today", "сегодня":
			periodText = "сегодня"
		case "yesterday", "вчера":
			periodText = "вчера"
		case "week", "неделя":
			periodText = "эту неделю"
		case "month", "месяц":
			periodText = "этот месяц"
		default:
			periodText = fmt.Sprintf("%s", from.Format("02.01.2006"))
		}

		response := fmt.Sprintf("📭 Нет событий за %s", periodText)
		h.bot.Send(tgbotapi.NewMessage(msg.Chat.ID, response))
		return
	}

	var response strings.Builder
	response.WriteString(fmt.Sprintf("📊 *События за период %s - %s:*\n\n",
		from.Format("02.01.2006"),
		to.Add(-time.Second).Format("02.01.2006")))

	// Группируем события по дням
	eventsByDay := make(map[string][]models.Event)
	for _, event := range events {
		dayKey := event.CreatedAt.Format("2006-01-02")
		eventsByDay[dayKey] = append(eventsByDay[dayKey], event)
	}

	// Выводим события по дням
	for day, dayEvents := range eventsByDay {
		parsedDay, _ := time.Parse("2006-01-02", day)
		response.WriteString(fmt.Sprintf("*%s:*\n", parsedDay.Format("02.01.2006")))

		for _, event := range dayEvents {
			response.WriteString(fmt.Sprintf("  ⏰ %s - %s",
				event.CreatedAt.Format("15:04"),
				event.Content))

			response.WriteString("\n")
		}
		response.WriteString("\n")
	}

	// Отправляем сообщение
	msgToSend := tgbotapi.NewMessage(msg.Chat.ID, response.String())
	msgToSend.ParseMode = "Markdown"

	// Если сообщение слишком длинное, разбиваем на части
	if len(response.String()) > 4096 {
		parts := splitLongMessage(response.String())
		for _, part := range parts {
			h.bot.Send(tgbotapi.NewMessage(msg.Chat.ID, part))
		}
	} else {
		h.bot.Send(msgToSend)
	}
}

// Вспомогательная функция для разбиения длинных сообщений
func splitLongMessage(text string) []string {
	const maxLen = 4096
	var parts []string

	for len(text) > maxLen {
		// Ищем последний перенос строки в пределах лимита
		splitAt := strings.LastIndex(text[:maxLen], "\n")
		if splitAt == -1 {
			splitAt = maxLen
		}
		parts = append(parts, text[:splitAt])
		text = text[splitAt:]
	}
	if len(text) > 0 {
		parts = append(parts, text)
	}
	return parts
}

func (h *BotHandler) handleCallback(cb *tgbotapi.CallbackQuery) {
	// Получаем пользователя из Redis
	user, err := h.userStore.GetByID(h.ctx, cb.From.ID)
	if err != nil {
		// Создаем нового пользователя если не найден
		user = &models.User{
			ID: cb.From.ID,
		}
		if err := h.userStore.Save(h.ctx, user); err != nil {
			h.bot.Send(tgbotapi.NewMessage(cb.Message.Chat.ID, "Ошибка при создании пользователя"))
			h.bot.Request(tgbotapi.NewCallback(cb.ID, ""))
			return
		}
	}

	if strings.HasPrefix(cb.Data, "freq_") {
		freq := cb.Data[5:]

		if freq == "custom" {
			h.bot.Send(tgbotapi.NewMessage(cb.Message.Chat.ID, "Введите время через пробел (например: 09:00 14:30 21:00):"))
			h.bot.Request(tgbotapi.NewCallback(cb.ID, ""))
			return
		}

		if freq == "immediate" {
			user.Frequency = "Immediate"
		} else {
			user.Frequency = freq
		}

		if err := h.userStore.Save(h.ctx, user); err != nil {
			h.bot.Send(tgbotapi.NewMessage(cb.Message.Chat.ID, "Ошибка при сохранении"))
			h.bot.Request(tgbotapi.NewCallback(cb.ID, ""))
			return
		}

		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Telegram", "notif_tg"),
				tgbotapi.NewInlineKeyboardButtonData("Email", "notif_mail"),
			),
		)
		editMsg := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, "Выбрано: "+user.Frequency+". Куда присылать уведомления?")
		editMsg.ReplyMarkup = &keyboard
		h.bot.Send(editMsg)
	}

	if cb.Data == "notif_tg" {
		user.Notifier = models.TelegramNotifier
		user.IsVerified = true
		if err := h.userStore.Save(h.ctx, user); err != nil {
			h.bot.Send(tgbotapi.NewMessage(cb.Message.Chat.ID, "Ошибка при сохранении"))
			h.bot.Request(tgbotapi.NewCallback(cb.ID, ""))
			return
		}
		h.bot.Send(tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, "Выбран Telegram. Теперь введите ваше имя (Label) для системы:"))
	}

	if cb.Data == "notif_mail" {
		user.Notifier = models.MailNotifier
		user.Email = ""
		user.IsVerified = false
		if err := h.userStore.Save(h.ctx, user); err != nil {
			h.bot.Send(tgbotapi.NewMessage(cb.Message.Chat.ID, "Ошибка при сохранении"))
			h.bot.Request(tgbotapi.NewCallback(cb.ID, ""))
			return
		}
		h.bot.Send(tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, "Введите ваш Email:"))
	}

	h.bot.Request(tgbotapi.NewCallback(cb.ID, ""))
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
