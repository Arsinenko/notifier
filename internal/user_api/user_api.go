package user_api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"notifier/internal/models" // проверьте правильность импорта
	"notifier/internal/permissions"
	"strconv"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// RunUserAPI запускает сервер. Принимает указатель на срез и мьютекс из main.
func RunUserAPI(port string, users *[]*models.User, mu *sync.RWMutex) {
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		// Если файл лежит рядом с исполняемым файлом:
		http.ServeFile(w, r, "index.html")
	})

	r.Route("/users", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			mu.RLock()
			defer mu.RUnlock()
			renderJSON(w, *users)
		})

		r.Post("/", func(w http.ResponseWriter, r *http.Request) {
			var u models.User
			if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}

			mu.Lock()
			// Если ID не пришел, генерируем простейший (на основе текущего кол-ва)
			if u.ID == 0 {
				u.ID = int64(len(*users) + 1)
			}
			*users = append(*users, &u)
			mu.Unlock()

			w.WriteHeader(http.StatusCreated)
			renderJSON(w, u)
		})
		// PATCH /users/{id}/permissions
		r.Patch("/{id}/permissions", func(w http.ResponseWriter, r *http.Request) {
			idStr := chi.URLParam(r, "id")
			id, _ := strconv.ParseInt(idStr, 10, 64)

			var newPerms []permissions.Permission
			if err := json.NewDecoder(r.Body).Decode(&newPerms); err != nil {
				http.Error(w, "invalid permissions format", http.StatusBadRequest)
				return
			}

			mu.Lock()
			defer mu.Unlock()

			for _, u := range *users {
				if u.ID == id {
					u.Permissions = newPerms
					renderJSON(w, u)
					return
				}
			}

			http.Error(w, "user not found", http.StatusNotFound)
		})

		r.Delete("/{id}", func(w http.ResponseWriter, r *http.Request) {
			idStr := chi.URLParam(r, "id")
			id, _ := strconv.ParseInt(idStr, 10, 64)

			mu.Lock()
			defer mu.Unlock()

			newUsers := make([]*models.User, 0)
			found := false
			for _, u := range *users {
				if u.ID != id {
					newUsers = append(newUsers, u)
				} else {
					found = true
				}
			}

			if !found {
				http.Error(w, "user not found", http.StatusNotFound)
				return
			}

			*users = newUsers
			w.WriteHeader(http.StatusNoContent)
		})
	})

	http.ListenAndServe(":"+port, r)
	fmt.Println("server running on :", 8080)

}

func renderJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}
