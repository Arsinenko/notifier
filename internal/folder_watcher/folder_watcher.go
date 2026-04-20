package folder_watcher

import (
	"log"
	"notifier/internal/models"
	"os" // Добавляем пакет os
	"time"

	"github.com/fsnotify/fsnotify"
)

func Watcher(notsChan chan models.Event, dir string) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				op := event.Op
				if event.Op&fsnotify.Rename == fsnotify.Rename {
					if _, err := os.Stat(event.Name); os.IsNotExist(err) {
						// Подменяем тип операции для вашей модели данных
						op = fsnotify.Remove
					}
				}

				notsChan <- models.Event{
					ID:        0,
					Op:        op, // используем обработанную операцию
					Content:   event.String(),
					CreatedAt: time.Now(),
				}

			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("Ошибка:", err)
			}
		}
	}()

	_ = watcher.Add(dir)
	<-make(chan struct{})
}
