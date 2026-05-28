package storage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
)

type Storage struct {
	Path       string
	BackupPath string
	Key        []byte

	store map[string]any
	mu    sync.RWMutex
}

const (
	defaultKey = "381a0130d76c062f9f0219ee485ee3f2"
)

func New(path string) (*Storage, error) {
	return NewWithKey(path, defaultKey)
}

func NewWithKey(path string, keyHex string) (*Storage, error) {
	if keyHex == "" {
		keyHex = defaultKey
	}
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, err
	}
	if l := len(key); l != 16 && l != 24 && l != 32 {
		return nil, errors.New("key must be 16/24/32 bytes (32/48/64 hex)")
	}

	base := stripExt(path)

	return &Storage{
		Path:       path,
		BackupPath: base + ".backup.bin",
		Key:        key,
		store:      map[string]any{},
	}, nil
}

func (s *Storage) Initialize() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ensureParent(s.Path)
	ensureParent(s.BackupPath)

	if b, err := os.ReadFile(s.Path); err == nil && len(b) > 16 {
		if err := s.decryptToStore(b); err == nil {
			return nil
		}

		// fallback ke backup
		if fb, err := os.ReadFile(s.BackupPath); err == nil {
			if err := s.decryptToStore(fb); err == nil {
				return nil
			}
		}
	}

	s.store = map[string]any{}
	return s.save()
}

func (s *Storage) Get(key string) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.store[key]
}

func (s *Storage) GetString(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.store[key].(string)
	return v, ok
}

func (s *Storage) GetInt(key string) (int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	switch v := s.store[key].(type) {
	case float64: // JSON number
		return int(v), true
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			return n, true
		}
	}
	return 0, false
}

func (s *Storage) GetBool(key string) (bool, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.store[key].(bool)
	return v, ok
}

func (s *Storage) GetJSON(key string) map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if m, ok := s.store[key].(map[string]any); ok {
		out := make(map[string]any, len(m))
		maps.Copy(out, m)
		return out
	}

	return map[string]any{}
}

func (s *Storage) GetJSONInto(key string, out any) error {
	m := s.GetJSON(key)
	b, _ := json.Marshal(m)
	return json.Unmarshal(b, out)
}

func (s *Storage) GetStringList(key string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.store[key]
	if !ok {
		return []string{}
	}

	raw, ok := v.([]any)
	if !ok {
		return []string{}
	}

	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if str, ok := item.(string); ok {
			out = append(out, str)
		}
	}
	return out
}

func (s *Storage) GetListData(key string) []any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.store[key]
	if !ok {
		return []any{}
	}

	if l, ok := v.([]any); ok {
		// copy biar aman (tidak mutate internal store)
		out := make([]any, len(l))
		copy(out, l)
		return out
	}

	return []any{}
}

func (s *Storage) GetListJSONInto(key string, out any) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// out HARUS pointer ke slice
	rv := reflect.ValueOf(out)
	if rv.Kind() != reflect.Ptr || rv.Elem().Kind() != reflect.Slice {
		return errors.New("out must be pointer to slice")
	}

	v, ok := s.store[key]
	if !ok {
		// key tidak ada → slice kosong
		rv.Elem().Set(reflect.MakeSlice(rv.Elem().Type(), 0, 0))
		return nil
	}

	list, ok := v.([]any)
	if !ok {
		return fmt.Errorf("value for key %q is not a list", key)
	}

	// marshal list (of map[string]any) → json
	b, err := json.Marshal(list)
	if err != nil {
		return err
	}

	// unmarshal → []T
	return json.Unmarshal(b, out)
}

func (s *Storage) Set(key string, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store[key] = value
	return s.save()
}

func (s *Storage) SetStruct(key string, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// kalau value sudah map, langsung pakai
	if m, ok := value.(map[string]any); ok {
		s.store[key] = m
		return s.save()
	}

	// normalize struct → map
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}

	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}

	s.store[key] = m
	return s.save()
}

func (s *Storage) SetListStruct(key string, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}

	var arr []any
	if err := json.Unmarshal(b, &arr); err != nil {
		return err
	}

	s.store[key] = arr
	return s.save()
}

func (s *Storage) SetAuto(key string, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}

	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}

	s.store[key] = v
	return s.save()
}

func (s *Storage) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.store[key]; ok {
		delete(s.store, key)
		return s.save()
	}
	return nil
}

func (s *Storage) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.store = map[string]any{}
	return s.save()
}

func (s *Storage) Keys() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]string, 0, len(s.store))
	for k := range s.store {
		out = append(out, k)
	}
	return out
}

func (s *Storage) RestoreFromBackup() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	b, err := os.ReadFile(s.BackupPath)
	if err != nil {
		return err
	}
	if err := s.decryptToStore(b); err != nil {
		return err
	}
	return s.save()
}

// save assumes caller already holds s.mu.Lock()
func (s *Storage) save() error {
	raw, err := json.Marshal(s.store)
	if err != nil {
		return err
	}

	iv := make([]byte, 16)
	io.ReadFull(rand.Reader, iv)

	block, _ := aes.NewCipher(s.Key)
	mode := cipher.NewCBCEncrypter(block, iv)

	pad := pkcs7Pad(raw, block.BlockSize())
	mode.CryptBlocks(pad, pad)

	out := append(iv, pad...)

	if _, err := os.Stat(s.Path); err == nil {
		b, _ := os.ReadFile(s.Path)
		_ = os.WriteFile(s.BackupPath, b, 0600)
	}

	return os.WriteFile(s.Path, out, 0600)
}

func (s *Storage) decryptToStore(blob []byte) error {
	if len(blob) < 16 {
		return errors.New("invalid blob")
	}

	iv := blob[:16]
	data := blob[16:]

	block, _ := aes.NewCipher(s.Key)
	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(data, data)

	plain, err := pkcs7Unpad(data, block.BlockSize())
	if err != nil {
		return err
	}
	var obj map[string]any
	if err := json.Unmarshal(plain, &obj); err != nil {
		return err
	}

	s.store = obj
	return nil
}

func (s *Storage) ExportJSON() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// deep copy supaya aman
	cp := make(map[string]any, len(s.store))
	for k, v := range s.store {
		cp[k] = v
	}

	return json.MarshalIndent(cp, "", "  ")
}

func (s *Storage) ExportJSONToFile(path string) error {
	b, err := s.ExportJSON()
	if err != nil {
		return err
	}

	ensureParent(path)
	return os.WriteFile(path, b, 0644)
}

func (s *Storage) ImportJSON(data []byte) error {
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.store = obj
	return s.save()
}

func (s *Storage) ImportJSONFile(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return s.ImportJSON(b)
}

func (s *Storage) MergeJSON(data []byte) error {
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for k, v := range obj {
		s.store[k] = v
	}
	return s.save()
}

func (s *Storage) MergeJSONFile(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return s.MergeJSON(b)
}

func pkcs7Pad(b []byte, blockSize int) []byte {
	p := blockSize - len(b)%blockSize
	out := make([]byte, len(b)+p)
	copy(out, b)
	for i := 0; i < p; i++ {
		out[len(b)+i] = byte(p)
	}
	return out
}

func pkcs7Unpad(b []byte, blockSize int) ([]byte, error) {
	if len(b) == 0 || len(b)%blockSize != 0 {
		return nil, errors.New("invalid padding size")
	}
	p := int(b[len(b)-1])
	if p == 0 || p > blockSize || p > len(b) {
		return nil, errors.New("invalid padding")
	}
	for i := 0; i < p; i++ {
		if b[len(b)-1-i] != byte(p) {
			return nil, errors.New("invalid padding")
		}
	}
	return b[:len(b)-p], nil
}

func ensureParent(path string) {
	dir := filepath.Dir(path)
	_ = os.MkdirAll(dir, 0755)
}

func stripExt(p string) string {
	ext := filepath.Ext(p)
	return p[:len(p)-len(ext)]
}
