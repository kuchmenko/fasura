package fasura

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Plugin adds handlers or background workers to an App.
type Plugin interface {
	Register(*App) error
}

// Worker runs alongside EVM subscriptions and stops with the App context.
type Worker func(context.Context) error

// App owns compiled sources, handlers, plugins, and their shared lifecycle.
type App struct {
	config compiledConfig
	dial   dialClient

	mu       sync.Mutex
	running  bool
	handlers map[string][]Handler
	all      []Handler
	workers  []Worker
}

// New validates and compiles source configuration.
func New(config Config) (*App, error) {
	compiled, err := compileConfig(config)
	if err != nil {
		return nil, err
	}
	return &App{
		config:   compiled,
		dial:     defaultDialClient,
		handlers: make(map[string][]Handler),
	}, nil
}

// Handle registers a handler for one source.event key.
func (app *App) Handle(key string, handler Handler) error {
	if handler == nil {
		return errors.New("handler is required")
	}
	if _, exists := app.config.eventKeys[key]; !exists {
		return fmt.Errorf("unknown event %q", key)
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.running {
		return errors.New("cannot register handler after Run")
	}
	app.handlers[key] = append(app.handlers[key], handler)
	return nil
}

// HandleAll registers a handler for every configured event.
func (app *App) HandleAll(handler Handler) error {
	if handler == nil {
		return errors.New("handler is required")
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.running {
		return errors.New("cannot register handler after Run")
	}
	app.all = append(app.all, handler)
	return nil
}

// AddWorker registers background work owned by a plugin.
func (app *App) AddWorker(worker Worker) error {
	if worker == nil {
		return errors.New("worker is required")
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.running {
		return errors.New("cannot register worker after Run")
	}
	app.workers = append(app.workers, worker)
	return nil
}

// Use registers plugins before Run starts.
func (app *App) Use(plugins ...Plugin) error {
	for index, plugin := range plugins {
		if plugin == nil {
			return fmt.Errorf("plugin %d is nil", index)
		}
		if err := plugin.Register(app); err != nil {
			return fmt.Errorf("register plugin %d: %w", index, err)
		}
	}
	return nil
}

func (app *App) prepareRun() (map[string][]Handler, []Handler, []Worker, error) {
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.running {
		return nil, nil, nil, errors.New("App is already running")
	}
	if len(app.all) == 0 {
		for key := range app.config.eventKeys {
			if len(app.handlers[key]) == 0 {
				return nil, nil, nil, fmt.Errorf("event %q has no handler", key)
			}
		}
	}
	app.running = true

	handlers := make(map[string][]Handler, len(app.handlers))
	for key, registered := range app.handlers {
		handlers[key] = append([]Handler(nil), registered...)
	}
	return handlers, append([]Handler(nil), app.all...), append([]Worker(nil), app.workers...), nil
}

func dispatch(ctx context.Context, event Event, handlers map[string][]Handler, all []Handler) error {
	for _, handler := range handlers[event.Key()] {
		if err := handler(ctx, event); err != nil {
			return err
		}
	}
	for _, handler := range all {
		if err := handler(ctx, event); err != nil {
			return err
		}
	}
	return nil
}
