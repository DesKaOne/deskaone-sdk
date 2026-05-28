package emitter

import "sync"

type EventHandler[D any, E any] func(data D, extra E)

type HandlerID uint64

type EventEmitter[T comparable, D any, E any] struct {
	mu       sync.RWMutex
	handlers map[T]map[HandlerID]EventHandler[D, E]
	nextID   HandlerID
}

func NewEventEmitter[T comparable, D any, E any]() *EventEmitter[T, D, E] {
	return &EventEmitter[T, D, E]{
		handlers: make(map[T]map[HandlerID]EventHandler[D, E]),
	}
}

func (e *EventEmitter[T, D, E]) On(evt T, h EventHandler[D, E]) HandlerID {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.nextID++
	id := e.nextID

	if e.handlers[evt] == nil {
		e.handlers[evt] = make(map[HandlerID]EventHandler[D, E])
	}

	e.handlers[evt][id] = h
	return id
}

func (e *EventEmitter[T, D, E]) Once(evt T, h EventHandler[D, E]) HandlerID {
	var id HandlerID

	id = e.On(evt, func(d D, ex E) {
		e.Off(evt, id)
		h(d, ex)
	})

	return id
}

func (e *EventEmitter[T, D, E]) Off(evt T, id HandlerID) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.handlers[evt] != nil {
		delete(e.handlers[evt], id)
		if len(e.handlers[evt]) == 0 {
			delete(e.handlers, evt)
		}
	}
}

func (e *EventEmitter[T, D, E]) Emit(evt T, data D, extra E) {
	e.mu.RLock()
	handlers := make([]EventHandler[D, E], 0, len(e.handlers[evt]))
	for _, h := range e.handlers[evt] {
		handlers = append(handlers, h)
	}
	e.mu.RUnlock()

	for _, h := range handlers {
		h(data, extra)
	}
}

func (e *EventEmitter[T, D, E]) EmitAsync(evt T, data D, extra E) {
	e.mu.RLock()
	handlers := make([]EventHandler[D, E], 0, len(e.handlers[evt]))
	for _, h := range e.handlers[evt] {
		handlers = append(handlers, h)
	}
	e.mu.RUnlock()

	for _, h := range handlers {
		go h(data, extra)
	}
}
