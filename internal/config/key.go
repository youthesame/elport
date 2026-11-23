package config

import "fmt"

type APIKey struct {
	reveal func() string
}

func NewAPIKey(value string) APIKey {
	if value == "" {
		return APIKey{}
	}
	v := value
	return APIKey{reveal: func() string { return v }}
}

func (k APIKey) Empty() bool {
	return k.reveal == nil || k.reveal() == ""
}

func (k APIKey) Reveal() string {
	if k.reveal == nil {
		return ""
	}
	return k.reveal()
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
