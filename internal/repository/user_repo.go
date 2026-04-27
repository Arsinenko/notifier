package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"notifier/internal/models"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type UserStore interface {
	Save(ctx context.Context, user *models.User) error
	GetByID(ctx context.Context, id int64) (*models.User, error)
	GetAll(ctx context.Context) ([]*models.User, error)
	UpdateLastNotified(ctx context.Context, id int64, t time.Time) error
	Delete(ctx context.Context, id int64) error
}
type UserRepo struct {
	client *redis.Client
	key    string
}

func (u *UserRepo) Delete(ctx context.Context, id int64) error {
	return u.client.HDel(ctx, u.key, strconv.FormatInt(id, 10)).Err()
}

func NewUserRepo(c *redis.Client) UserRepo {
	return UserRepo{
		client: c,
		key:    "notifier:users",
	}
}

func (u *UserRepo) Save(ctx context.Context, user *models.User) error {
	data, err := json.Marshal(user)
	if err != nil {
		return fmt.Errorf("marshal user failed %v", err)
	}
	return u.client.HSet(ctx, u.key, strconv.FormatInt(user.ID, 10), data).Err()
}

func (u *UserRepo) GetByID(ctx context.Context, id int64) (*models.User, error) {
	val, err := u.client.HGet(ctx, u.key, strconv.FormatInt(id, 10)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("user not found")
	} else if err != nil {
		return nil, err
	}

	var user models.User
	err = json.Unmarshal([]byte(val), &user)
	if err != nil {
		return nil, err
	}
	return &user, nil

}

func (u *UserRepo) GetAll(ctx context.Context) ([]*models.User, error) {
	data, err := u.client.HGetAll(ctx, u.key).Result()
	if errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("users empty")
	}
	users := make([]*models.User, 0, len(data))
	for _, item := range data {
		var u models.User
		if err := json.Unmarshal([]byte(item), &u); err == nil {
			users = append(users, &u)
		}
	}
	return users, nil
}

func (u *UserRepo) UpdateLastNotified(ctx context.Context, id int64, t time.Time) error {
	user, err := u.GetByID(ctx, id)
	if err != nil {
		return err
	}
	user.LastNotifiedAt = t
	return u.Save(ctx, user)
}
