package service

import (
	"time"
)

// fixedTime is a deterministic timestamp used throughout service tests.
var fixedTime = time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
