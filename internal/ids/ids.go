package ids

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

func New(prefix string) string {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UTC().UnixNano())
	}

	return fmt.Sprintf("%s_%s_%s", prefix, time.Now().UTC().Format("20060102_150405"), hex.EncodeToString(buf))
}
