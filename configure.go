package main

import (
	"log"

	"path/filepath"

	dockerClient "github.com/docker/docker/client"
	"github.com/fsnotify/fsnotify"
)

func watchConfigFile(dockerClient *dockerClient.Client) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	done := make(chan bool)
	go func() {
		for {
			select {
			case event := <-watcher.Events:
				log.Println("event:", event)
				if event.Op&fsnotify.Write == fsnotify.Write {
					log.Println("modified file:", event.Name)
				}
				if event.Op&fsnotify.Chmod == fsnotify.Chmod {
					configureService(dockerClient, "")
				}
			case err := <-watcher.Errors:
				log.Println("error:", err)
			}
		}
	}()
	err = watcher.Add(filepath.Join(ConfigDir, "/services/LB/haproxy.cfg"))
	if err != nil {
		log.Fatal(err)
	}
	<-done
}
