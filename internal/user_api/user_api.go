package user_api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"notifier/internal/models"
	"notifier/internal/permissions"
	"notifier/internal/repository"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/redis/go-redis/v9"
)

type UserAPI struct {
	userStore repository.UserStore
	ctx       context.Context
}

// RunUserAPI запускает сервер с Redis
func RunUserAPI(port string, repo repository.UserStore) {
	api := &UserAPI{
		userStore: repo,
		ctx:       context.Background(),
	}

	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})

	r.Route("/users", func(r chi.Router) {
		// GET /users - получение всех пользователей
		r.Get("/", api.getAllUsers)

		// POST /users - создание нового пользователя
		r.Post("/", api.createUser)

		// GET /users/{id} - получение пользователя по ID
		r.Get("/{id}", api.getUser)

		// PATCH /users/{id}/permissions - обновление прав пользователя
		r.Patch("/{id}/permissions", api.updatePermissions)

		// PATCH /users/{id}/frequency - обновление частоты уведомлений
		r.Patch("/{id}/frequency", api.updateFrequency)

		// DELETE /users/{id} - удаление пользователя
		r.Delete("/{id}", api.deleteUser)
	})

	fmt.Printf("User API server running on port %s\n", port)
	http.ListenAndServe(":"+port, r)
}

func (api *UserAPI) getAllUsers(w http.ResponseWriter, r *http.Request) {
	users, err := api.userStore.GetAll(api.ctx)
	if err != nil {
		http.Error(w, "Failed to get users", http.StatusInternalServerError)
		return
	}

	if users == nil {
		users = []*models.User{}
	}

	renderJSON(w, users)
}

func (api *UserAPI) getUser(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	user, err := api.userStore.GetByID(api.ctx, id)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	renderJSON(w, user)
}

func (api *UserAPI) createUser(w http.ResponseWriter, r *http.Request) {
	var u models.User
	if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
		http.Error(w, "Bad request: invalid JSON", http.StatusBadRequest)
		return
	}

	// Валидация
	if u.ID == 0 {
		http.Error(w, "User ID is required", http.StatusBadRequest)
		return
	}

	if u.Notifier == "" {
		u.Notifier = models.TelegramNotifier // Значение по умолчанию
	}

	if u.Frequency == "" {
		u.Frequency = "09:00 18:00" // Значение по умолчанию
	}

	if len(u.Permissions) == 0 {
		u.Permissions = []permissions.Permission{permissions.CreatePermission} // Значение по умолчанию
	}

	u.LastNotifiedAt = time.Time{}

	// Проверяем, не существует ли уже пользователь
	existing, _ := api.userStore.GetByID(api.ctx, u.ID)
	if existing != nil {
		http.Error(w, "User already exists", http.StatusConflict)
		return
	}

	if err := api.userStore.Save(api.ctx, &u); err != nil {
		http.Error(w, "Failed to create user", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	renderJSON(w, u)
}

func (api *UserAPI) updatePermissions(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	var newPerms []permissions.Permission
	if err := json.NewDecoder(r.Body).Decode(&newPerms); err != nil {
		http.Error(w, "Invalid permissions format", http.StatusBadRequest)
		return
	}

	// Получаем существующего пользователя
	user, err := api.userStore.GetByID(api.ctx, id)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// Обновляем права
	user.Permissions = newPerms

	// Сохраняем изменения
	if err := api.userStore.Save(api.ctx, user); err != nil {
		http.Error(w, "Failed to update user", http.StatusInternalServerError)
		return
	}

	renderJSON(w, user)
}

func (api *UserAPI) updateFrequency(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	var request struct {
		Frequency string `json:"frequency"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid frequency format", http.StatusBadRequest)
		return
	}

	if request.Frequency == "" {
		http.Error(w, "Frequency cannot be empty", http.StatusBadRequest)
		return
	}

	// Получаем существующего пользователя
	user, err := api.userStore.GetByID(api.ctx, id)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// Обновляем частоту
	user.Frequency = request.Frequency

	// Сохраняем изменения
	if err := api.userStore.Save(api.ctx, user); err != nil {
		http.Error(w, "Failed to update user", http.StatusInternalServerError)
		return
	}

	renderJSON(w, user)
}

func renderJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

// Вспомогательная функция для доступа к Redis клиенту
// В идеале нужно добавить метод Delete в интерфейс UserStore
func getUserRepoRedisClient(store repository.UserStore) *redis.Client {
	// Если у нас есть доступ к конкретной реализации, можно сделать type assertion
	// Но лучше добавить метод Delete в интерфейс UserStore
	return nil
}
func (api *UserAPI) deleteUser(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	// Проверяем, существует ли пользователь
	_, err = api.userStore.GetByID(api.ctx, id)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// Удаляем пользователя
	if err := api.userStore.Delete(api.ctx, id); err != nil {
		http.Error(w, "Failed to delete user", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
