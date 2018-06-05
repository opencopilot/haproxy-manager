package main

import (
	"crypto/md5"
	"io"
	"log"
	"os"
	"time"

	"path/filepath"

	dockerClient "github.com/docker/docker/client"
	"github.com/subgraph/inotify"
)

func pollConfig(dockerCli *dockerClient.Client) {
	filePath := filepath.Join(ConfigDir, "/services/", ServiceName, "/haproxy.cfg")
	fileHash := ""
	for {
		f, err := os.Open(filePath)
		if err != nil {
			log.Println(err)
		}
		h := md5.New()
		if _, err := io.Copy(h, f); err != nil {
			log.Println(err)
		}
		newHash := string(h.Sum(nil))
		if fileHash != newHash {
			configureService(dockerCli)
			fileHash = newHash
		}
		f.Close()
		time.Sleep(3 * time.Second)
	}
}

func watchConfig(dockerCli *dockerClient.Client) {
	filePath := filepath.Join(ConfigDir, "/services/", ServiceName, "/haproxy.cfg")
	watcher, err := inotify.NewWatcher()
	err = watcher.Watch(filePath)
	if err != nil {
		log.Fatal(err)
	}
	for {
		select {
		case ev := <-watcher.Event:
			// log.Println("event:", ev, ev.Mask)
			if ev.Mask == inotify.IN_MODIFY {
				configureService(dockerCli)
			}
		case err := <-watcher.Error:
			log.Println("error:", err)
		}
	}
}
