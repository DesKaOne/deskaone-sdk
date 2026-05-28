package network

import (
	"encoding/binary"
	"errors"
	"math"
	"sync"
	"sync/atomic"
)

const (
	defaultBufferSize = 32 * 1024
	maxFrameSize      = 256 * 1024
)

type SeekOrigin int

const (
	SeekBegin SeekOrigin = iota
	SeekCurrent
	SeekEnd
)

type NetworkStream struct {
	buffer   []byte
	position int
	length   int

	// stream-style events (parity with Dart)
	ch     chan []byte
	errCh  chan error
	doneCh chan struct{}
	once   sync.Once
	closed atomic.Bool
}

// ==========================
// constructors
// ==========================

func NewNetworkStream(capacity int) *NetworkStream {
	if capacity <= 0 {
		capacity = defaultBufferSize
	}
	return &NetworkStream{
		buffer: make([]byte, capacity),
	}
}

func NewStreamFromBytes(data []byte) *NetworkStream {
	buf := make([]byte, len(data))
	copy(buf, data)
	return &NetworkStream{
		buffer: buf,
		length: len(buf),
	}
}

// ==========================
// core helpers
// ==========================

func (s *NetworkStream) Bytes() []byte {
	return s.buffer[:s.length]
}

func (s *NetworkStream) Remaining() int {
	return s.length - s.position
}

func (s *NetworkStream) Reset() {
	s.position = 0
	s.length = 0
}

func (s *NetworkStream) ensureCapacity(required int) {
	if required <= len(s.buffer) {
		return
	}
	n := len(s.buffer) * 2
	for n < required {
		n *= 2
	}
	buf := make([]byte, n)
	copy(buf, s.buffer)
	s.buffer = buf
}

// ==========================
// append (ONLY TcpReader uses this)
// ==========================

func (s *NetworkStream) Append(data []byte) {
	s.ensureCapacity(s.length + len(data))
	copy(s.buffer[s.length:], data)
	s.length += len(data)
}

// ==========================
// stream-style (Add/Listen)
// ==========================

func (s *NetworkStream) ensureStream() {
	s.once.Do(func() {
		s.ch = make(chan []byte, 16)
		s.errCh = make(chan error, 1)
		s.doneCh = make(chan struct{})
	})
}

// Add pushes data to stream listeners and appends to internal buffer.
func (s *NetworkStream) Add(data []byte) {
	s.Append(data)
	s.ensureStream()
	if s.closed.Load() {
		return
	}
	// copy to avoid external mutation
	b := append([]byte(nil), data...)
	s.ch <- b
}

// AddError notifies listeners about an error.
func (s *NetworkStream) AddError(err error) {
	s.ensureStream()
	if s.closed.Load() {
		return
	}
	s.errCh <- err
}

// CloseStream closes the stream channels (buffer remains).
func (s *NetworkStream) CloseStream() {
	s.ensureStream()
	if s.closed.Swap(true) {
		return
	}
	close(s.ch)
	close(s.errCh)
	close(s.doneCh)
}

// Listen subscribes to stream events.
func (s *NetworkStream) Listen(
	onData func([]byte),
	onError func(error),
	onDone func(),
) {
	s.ensureStream()
	go func() {
		for {
			select {
			case b, ok := <-s.ch:
				if !ok {
					if onDone != nil {
						onDone()
					}
					return
				}
				if onData != nil {
					onData(b)
				}
			case err, ok := <-s.errCh:
				if ok && onError != nil {
					onError(err)
				}
			case <-s.doneCh:
				if onDone != nil {
					onDone()
				}
				return
			}
		}
	}()
}

// ==========================
// positioning
// ==========================

func (s *NetworkStream) Position() int { return s.position }
func (s *NetworkStream) Length() int   { return s.length }

func (s *NetworkStream) SetPosition(pos int) error {
	if pos < 0 || pos > s.length {
		return errors.New("position out of bounds")
	}
	s.position = pos
	return nil
}

func (s *NetworkStream) Seek(offset int, origin SeekOrigin) error {
	var pos int
	switch origin {
	case SeekBegin:
		pos = offset
	case SeekCurrent:
		pos = s.position + offset
	case SeekEnd:
		pos = s.length + offset
	default:
		return errors.New("invalid seek origin")
	}
	return s.SetPosition(pos)
}

// ==========================
// internal read guard
// ==========================

func (s *NetworkStream) ensureReadable(n int) error {
	if n < 0 || n > maxFrameSize {
		return errors.New("invalid read size")
	}
	if s.position+n > s.length {
		return errors.New("not enough data in buffer")
	}
	return nil
}

// ==========================
// read (NetworkStream.Read)
// ==========================

func (s *NetworkStream) ReadByte() (byte, error) {
	if err := s.ensureReadable(1); err != nil {
		return 0, err
	}
	v := s.buffer[s.position]
	s.position++
	return v, nil
}

func (s *NetworkStream) ReadBool() (bool, error) {
	b, err := s.ReadByte()
	return b != 0, err
}

func (s *NetworkStream) ReadInt16() (int16, error) {
	if err := s.ensureReadable(2); err != nil {
		return 0, err
	}
	v := int16(binary.LittleEndian.Uint16(s.buffer[s.position:]))
	s.position += 2
	return v, nil
}

func (s *NetworkStream) ReadInt32() (int32, error) {
	if err := s.ensureReadable(4); err != nil {
		return 0, err
	}
	v := int32(binary.LittleEndian.Uint32(s.buffer[s.position:]))
	s.position += 4
	return v, nil
}

func (s *NetworkStream) ReadInt64() (int64, error) {
	if err := s.ensureReadable(8); err != nil {
		return 0, err
	}
	v := int64(binary.LittleEndian.Uint64(s.buffer[s.position:]))
	s.position += 8
	return v, nil
}

func (s *NetworkStream) ReadFloat32() (float32, error) {
	if err := s.ensureReadable(4); err != nil {
		return 0, err
	}
	v := math.Float32frombits(binary.LittleEndian.Uint32(s.buffer[s.position:]))
	s.position += 4
	return v, nil
}

func (s *NetworkStream) ReadDouble() (float64, error) {
	return s.ReadFloat64()
}

func (s *NetworkStream) ReadFloat64() (float64, error) {
	if err := s.ensureReadable(8); err != nil {
		return 0, err
	}
	v := math.Float64frombits(binary.LittleEndian.Uint64(s.buffer[s.position:]))
	s.position += 8
	return v, nil
}

func (s *NetworkStream) ReadBytes(n int) ([]byte, error) {
	if n < 0 || n > maxFrameSize {
		return nil, errors.New("invalid read size")
	}
	if err := s.ensureReadable(n); err != nil {
		return nil, err
	}
	out := make([]byte, n)
	copy(out, s.buffer[s.position:s.position+n])
	s.position += n
	return out, nil
}

func (s *NetworkStream) ReadString() (string, error) {
	n, err := s.ReadInt32()
	if err != nil {
		return "", err
	}
	if n < 0 || n > maxFrameSize {
		return "", errors.New("string length overflow")
	}
	b, err := s.ReadBytes(int(n))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ==========================
// write
// ==========================

func (s *NetworkStream) WriteByte(v byte) error {
	s.ensureCapacity(s.position + 1)
	s.buffer[s.position] = v
	s.position++
	if s.position > s.length {
		s.length = s.position
	}
	return nil
}

func (s *NetworkStream) WriteBool(v bool) error {
	if v {
		return s.WriteByte(1)
	} else {
		return s.WriteByte(0)
	}
}

func (s *NetworkStream) WriteInt16(v int16) error {
	s.ensureCapacity(s.position + 2)
	binary.LittleEndian.PutUint16(s.buffer[s.position:], uint16(v))
	s.position += 2
	if s.position > s.length {
		s.length = s.position
	}
	return nil
}

func (s *NetworkStream) WriteInt32(v int32) error {
	s.ensureCapacity(s.position + 4)
	binary.LittleEndian.PutUint32(s.buffer[s.position:], uint32(v))
	s.position += 4
	if s.position > s.length {
		s.length = s.position
	}
	return nil
}

func (s *NetworkStream) WriteInt64(v int64) error {
	s.ensureCapacity(s.position + 8)
	binary.LittleEndian.PutUint64(s.buffer[s.position:], uint64(v))
	s.position += 8
	if s.position > s.length {
		s.length = s.position
	}
	return nil
}

func (s *NetworkStream) WriteFloat32(v float32) error {
	s.ensureCapacity(s.position + 4)
	binary.LittleEndian.PutUint32(s.buffer[s.position:], math.Float32bits(v))
	s.position += 4
	if s.position > s.length {
		s.length = s.position
	}
	return nil
}

func (s *NetworkStream) WriteDouble(v float64) error {
	return s.WriteFloat64(v)
}

func (s *NetworkStream) WriteFloat64(v float64) error {
	s.ensureCapacity(s.position + 8)
	binary.LittleEndian.PutUint64(s.buffer[s.position:], math.Float64bits(v))
	s.position += 8
	if s.position > s.length {
		s.length = s.position
	}
	return nil
}

func (s *NetworkStream) WriteBytes(b []byte) error {
	s.ensureCapacity(s.position + len(b))
	copy(s.buffer[s.position:], b)
	s.position += len(b)
	if s.position > s.length {
		s.length = s.position
	}
	if s.position > s.length {
		s.length = s.position
	}
	return nil
}

func (s *NetworkStream) WriteString(str string) error {
	data := []byte(str)
	if err := s.WriteInt32(int32(len(data))); err != nil {
		return err
	}
	return s.WriteBytes(data)
}
