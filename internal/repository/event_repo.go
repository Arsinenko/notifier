package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"notifier/internal/models"
	"time"

	"github.com/redis/go-redis/v9"
)

type EventStore interface {
	AddEvent(ctx context.Context, event models.Event) error
	GetEvents(ctx context.Context, from, to time.Time) ([]models.Event, error)
	GetEventsForUser(ctx context.Context, user models.User, from, to time.Time) ([]models.Event, error)
	CleanUpOldEvents(ctx context.Context, olderThen time.Time) error
}

type RedisEventRepository struct {
	client    *redis.Client
	keyPrefix string
	ttl       time.Duration // Время жизни событий в Redis
}

func NewRedisEventRepository(client *redis.Client, ttl time.Duration) *RedisEventRepository {
	return &RedisEventRepository{
		client:    client,
		keyPrefix: "event:",
		ttl:       ttl,
	}
}

// AddEvent использует Sorted Set для хранения событий по времени
func (r *RedisEventRepository) AddEvent(ctx context.Context, event models.Event) error {
	// Сериализуем событие
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Используем timestamp как score для сортировки
	score := float64(event.CreatedAt.Unix())

	// Сохраняем в основной Sorted Set
	key := r.keyPrefix + "all"
	pipe := r.client.Pipeline()

	// Добавляем событие
	pipe.ZAdd(ctx, key, redis.Z{
		Score:  score,
		Member: data,
	})

	// Устанавливаем TTL для ключа
	pipe.Expire(ctx, key, r.ttl)

	// Также сохраняем отдельно для каждого типа операции (для быстрого поиска по правам)
	opKey := r.keyPrefix + "op:" + event.Op.String()
	pipe.ZAdd(ctx, opKey, redis.Z{
		Score:  score,
		Member: data,
	})
	pipe.Expire(ctx, opKey, r.ttl)

	_, err = pipe.Exec(ctx)
	return err
}

// GetEventsForUser возвращает события с учетом прав пользователя
func (r *RedisEventRepository) GetEventsForUser(ctx context.Context, user models.User, from, to time.Time) ([]models.Event, error) {
	var events []models.Event

	// Если у пользователя нет прав, возвращаем пустой список
	if len(user.Permissions) == 0 {
		return events, nil
	}

	// Получаем события для каждого разрешенного типа операции
	minScore := float64(from.Unix())
	maxScore := float64(to.Unix())

	// Используем pipeline для параллельного запроса
	pipe := r.client.Pipeline()
	results := make([]*redis.StringSliceCmd, len(user.Permissions))

	for i, perm := range user.Permissions {
		opKey := r.keyPrefix + "op:" + string(perm)
		results[i] = pipe.ZRangeByScore(ctx, opKey, &redis.ZRangeBy{
			Min: fmt.Sprintf("%f", minScore),
			Max: fmt.Sprintf("%f", maxScore),
		})
	}

	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to get events: %w", err)
	}

	// Собираем и дедуплицируем результаты
	uniqueEvents := make(map[string]models.Event)

	for _, result := range results {
		members, err := result.Result()
		if err != nil && err != redis.Nil {
			continue
		}

		for _, member := range members {
			var event models.Event
			if err := json.Unmarshal([]byte(member), &event); err != nil {
				continue
			}

			// Используем строковое представление события для дедупликации
			key := fmt.Sprintf("%d-%s", event.CreatedAt.Unix(), event.Content)
			if _, exists := uniqueEvents[key]; !exists {
				uniqueEvents[key] = event
			}
		}
	}

	// Конвертируем map в slice и сортируем по времени
	for _, event := range uniqueEvents {
		events = append(events, event)
	}

	// Сортируем по времени создания
	for i := 0; i < len(events)-1; i++ {
		for j := i + 1; j < len(events); j++ {
			if events[i].CreatedAt.After(events[j].CreatedAt) {
				events[i], events[j] = events[j], events[i]
			}
		}
	}

	return events, nil
}

func (r *RedisEventRepository) CleanUpOldEvents(ctx context.Context, olderThen time.Time) error {
	maxScore := float64(olderThen.Unix())

	// Удаляем из основного хранилища
	allKey := r.keyPrefix + "all"
	_, err := r.client.ZRemRangeByScore(ctx, allKey, "-inf", fmt.Sprintf("(%f", maxScore)).Result()
	if err != nil {
		return err
	}

	// Получаем список всех типов операций
	keys, err := r.client.Keys(ctx, r.keyPrefix+"op:*").Result()
	if err == nil {
		pipe := r.client.Pipeline()
		for _, key := range keys {
			pipe.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("(%f", maxScore))
		}
		pipe.Exec(ctx)
	}

	return nil
}

// GetLatestEventTime возвращает время последнего события
func (r *RedisEventRepository) GetLatestEventTime(ctx context.Context) (time.Time, error) {
	key := r.keyPrefix + "all"

	// Получаем последний элемент с максимальным score
	results, err := r.client.ZRevRangeWithScores(ctx, key, 0, 0).Result()
	if err != nil {
		return time.Time{}, err
	}

	if len(results) == 0 {
		return time.Time{}, nil
	}

	return time.Unix(int64(results[0].Score), 0), nil
}

// GetEvents - базовая реализация для получения событий за период
func (r *RedisEventRepository) GetEvents(ctx context.Context, from, to time.Time) ([]models.Event, error) {
	key := r.keyPrefix + "all"
	minScore := float64(from.Unix())
	maxScore := float64(to.Unix())

	members, err := r.client.ZRangeByScore(ctx, key, &redis.ZRangeBy{
		Min: fmt.Sprintf("%f", minScore),
		Max: fmt.Sprintf("%f", maxScore),
	}).Result()

	if err != nil {
		return nil, err
	}

	events := make([]models.Event, 0, len(members))
	for _, member := range members {
		var event models.Event
		if err := json.Unmarshal([]byte(member), &event); err != nil {
			continue
		}
		events = append(events, event)
	}

	return events, nil
}
