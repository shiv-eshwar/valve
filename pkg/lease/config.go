package lease

import "time"

// Config controls local lease borrowing and deny cache.
type Config struct {
	RPMChunk  int64
	TPMChunk  int64
	ITPMChunk int64
	OTPMChunk int64
	LeaseTTL  time.Duration
}

// DefaultConfig returns conservative defaults.
func DefaultConfig() Config {
	return Config{
		RPMChunk:  5,
		TPMChunk:  500,
		ITPMChunk: 500,
		OTPMChunk: 500,
		LeaseTTL:  2 * time.Second,
	}
}

func (c Config) normalized() Config {
	if c.RPMChunk <= 0 {
		c.RPMChunk = 5
	}
	if c.TPMChunk <= 0 {
		c.TPMChunk = 500
	}
	if c.ITPMChunk <= 0 {
		c.ITPMChunk = 500
	}
	if c.OTPMChunk <= 0 {
		c.OTPMChunk = 500
	}
	if c.LeaseTTL <= 0 {
		c.LeaseTTL = 2 * time.Second
	}
	return c
}
