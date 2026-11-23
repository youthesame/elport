package config

import "fmt"

type APIKey struct {
	value string
}

func NewAPIKey(value string) APIKey {
	return APIKey{value: value}
}

func (k APIKey) Empty() bool {
	return k.value == ""
}

func (k APIKey) Reveal() string {
	return k.value
}

func (k APIKey) String() string {
	return "[REDACTED]"
}

func (k APIKey) GoString() string {
	return "config.APIKey{[REDACTED]}"
}

func (k APIKey) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte("[REDACTED]"))
}
