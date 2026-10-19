package config

import (
	"errors"
	"fmt"
)

const (
	MinDownloadMbit = 1
	MaxDownloadMbit = 100000
)

var ErrInvalidDownloadLimit = errors.New("config: invalid download limit")

// SetDownloadLimit switches the download limit on or off and sets its rate in Mbit/s; a nil mbit keeps the stored rate. On error c stays unchanged.
func (c *Config) SetDownloadLimit(enabled bool, mbit *int) error {
	rate := c.DownloadLimitMbit
	if mbit != nil {
		rate = *mbit
	}
	if err := checkDownloadLimit(enabled, rate, mbit != nil); err != nil {
		return err
	}
	c.DownloadLimitEnabled, c.DownloadLimitMbit = enabled, rate
	return nil
}

// Validate reports the first setting in c that the server cannot run with, or nil.
func (c *Config) Validate() error {
	return checkDownloadLimit(c.DownloadLimitEnabled, c.DownloadLimitMbit, c.DownloadLimitMbit != 0)
}

func checkDownloadLimit(enabled bool, mbit int, set bool) error {
	if (set || enabled) && (mbit < MinDownloadMbit || mbit > MaxDownloadMbit) {
		return fmt.Errorf("%w: %d Mbit/s, allowed are %d to %d", ErrInvalidDownloadLimit, mbit, MinDownloadMbit, MaxDownloadMbit)
	}
	return nil
}
