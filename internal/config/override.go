package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/metacubex/mihomo/constant"
)

type OverrideSlot int

const (
	OverrideSlotPersist OverrideSlot = iota
	OverrideSlotSession
)

const defaultPersistOverride = `{}`
const defaultSessionOverride = `{}`

var sessionOverride = defaultSessionOverride
var overrideMu sync.RWMutex

func overridePersistPath() string {
	return constant.Path.Resolve("override.json")
}

func ReadOverride(slot OverrideSlot) string {
	overrideMu.RLock()
	defer overrideMu.RUnlock()
	switch slot {
	case OverrideSlotPersist:
		buf, err := os.ReadFile(overridePersistPath())
		if err != nil {
			return defaultPersistOverride
		}

		return string(buf)
	case OverrideSlotSession:
		return sessionOverride
	}

	return ""
}

func WriteOverride(slot OverrideSlot, content string) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &obj); err != nil {
		return fmt.Errorf("invalid override: %w", err)
	}
	if obj == nil {
		return fmt.Errorf("override must be a JSON object")
	}
	overrideMu.Lock()
	defer overrideMu.Unlock()
	switch slot {
	case OverrideSlotPersist:
		return writeFileAtomically(overridePersistPath(), strings.NewReader(content))
	case OverrideSlotSession:
		sessionOverride = content
		return nil
	}
	return fmt.Errorf("invalid override slot: %d", slot)
}

func ClearOverride(slot OverrideSlot) error {
	overrideMu.Lock()
	defer overrideMu.Unlock()
	switch slot {
	case OverrideSlotPersist:
		err := os.Remove(overridePersistPath())
		if os.IsNotExist(err) {
			return nil
		}
		return err
	case OverrideSlotSession:
		sessionOverride = defaultSessionOverride
		return nil
	}
	return fmt.Errorf("invalid override slot: %d", slot)
}
