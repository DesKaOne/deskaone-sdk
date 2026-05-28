package database

import "errors"

var (
	ErrEmptyDriver       = errors.New("database driver is empty")
	ErrEmptyDSN          = errors.New("database DSN is empty")
	ErrUnsupportedDriver = errors.New("unsupported database driver")
	ErrInvalidIdentifier = errors.New("invalid SQL identifier")
)
