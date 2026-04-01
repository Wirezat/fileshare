package main

import (
	"time"

	"github.com/Wirezat/fileshare/pkg/shared"
)

func startExpirationWatcher(interval time.Duration) {
	go func() {
		for {
			time.Sleep(interval)
			config, err := shared.LoadConfig()
			if err != nil {
				continue
			}
			now := time.Now().Unix()
			changed := false
			for subpath, fd := range config.Files {
				if !fd.Expired && fd.Expiration != 0 && fd.Expiration < now {
					fd.Expired = true
					config.Files[subpath] = fd
					changed = true
				}
			}
			if changed {
				shared.SaveConfig(config)
			}
		}
	}()
}
