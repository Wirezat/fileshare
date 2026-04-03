package main

import (
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/pkg/shared"
)

func startExpirationWatcher(interval time.Duration) {
	go func() {
		GoLog.Infof("expiration watcher started (interval: %s)", interval)
		for {
			time.Sleep(interval)

			config, err := shared.LoadConfig()
			if err != nil {
				GoLog.Errorf("failed to load config: %v", err)
				continue
			}

			now := time.Now().Unix()
			changed := false
			for subpath, fd := range config.Files {
				if !fd.Expired && fd.Expiration != 0 && fd.Expiration < now {
					fd.Expired = true
					config.Files[subpath] = fd
					changed = true
					GoLog.Infof("file expired: %s", subpath)
				}
			}

			if changed {
				if err := shared.SaveConfig(config); err != nil {
					GoLog.Errorf("failed to save config after expiration update: %v", err)
				}
			}
		}
	}()
}
