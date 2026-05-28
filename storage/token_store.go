package storage

type TokenStore interface {
	GetString(key string) (string, bool)
}

var GlobalStore *Storage
