package folder_watcher

import (
	"log"
	"notifier/models"
	"time"

	"github.com/fsnotify/fsnotify"
)

func Watcher(notsChan chan models.Event, dir string) {
	watcher, _ := fsnotify.NewWatcher()
	defer watcher.Close()

	go func() {
		for {
			select {
			case Event, ok := <-watcher.Events:
				if !ok {
					return
				}
				notsChan <- models.Event{
					ID:        0,
					Content:   Event.String(),
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
