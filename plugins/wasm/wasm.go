// Package jsvm implements pluggable utilities for binding a JS goja runtime
// to the PocketBase instance (loading migrations, attaching to app hooks, etc.).
//
// Example:
//
//	quickjs.MustRegister(app, quickjs.Config{
//		HooksWatch: true,
//	})
package wasm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"

	m "github.com/pocketbase/pocketbase/migrations"

	"github.com/fatih/color"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/plugins/wasm/internal/types/generated"
	"github.com/pocketbase/pocketbase/tools/rest"
	"github.com/pocketbase/pocketbase/tools/waitgroup"
)

const (
	typesFileName = "wasm.d.ts"
)

type plugin struct {
	app    core.App
	config Config
}

// Config defines the config options of the jsvm plugin.
type Config struct {
	// OnInit is an optional function that will be called
	// after a JS runtime is initialized, allowing you to
	// attach custom Go variables and functions.
	OnInit func(vm *wazero.Runtime)

	// HooksWatch enables auto app restarts when a JS app hook file changes.
	//
	// Note that currently the application cannot be automatically restarted on Windows
	// because the restart process relies on execve.
	HooksWatch bool

	// HooksDir specifies the JS app hooks directory.
	//
	// If not set it fallbacks to a relative "pb_data/../pb_hooks" directory.
	HooksDir string

	// HooksFilesPattern specifies a regular expression pattern that
	// identify which file to load by the hook vm(s).
	//
	// If not set it fallbacks to `^.*\.wasm$`, aka. any
	// HookdsDir file ending in ".wasm" (the last one is to enforce IDE linters).
	HooksFilesPattern string

	// HooksPoolSize specifies how many goja.Runtime instances to prewarm
	// and keep for the JS app hooks gorotines execution.
	//
	// Zero or negative value means that it will create a new goja.Runtime
	// on every fired goroutine.
	HooksPoolSize int

	// MigrationsDir specifies the JS migrations directory.
	//
	// If not set it fallbacks to a relative "pb_data/../pb_migrations" directory.
	MigrationsDir string

	// If not set it fallbacks to `^.*\.wasm$`, aka. any MigrationDir file
	// ending in ".wasm" (the last one is to enforce IDE linters).
	MigrationsFilesPattern string

	// TypesDir specifies the directory where to store the embedded
	// TypeScript declarations file.
	//
	// If not set it fallbacks to "pb_data".
	//
	// Note: Avoid using the same directory as the HooksDir when HooksWatch is enabled
	// to prevent unnecessary app restarts when the types file is initially created.
	TypesDir string
}

// MustRegister registers the jsvm plugin in the provided app instance
// and panics if it fails.
//
// Example usage:
//
//	jsvm.MustRegister(app, jsvm.Config{
//		OnInit: func(vm *wazero.Runtime) {
//			// register custom bindings
//			vmSet(&module,"myCustomVar", 123)
//		}
//	})
func MustRegister(app core.App, config Config) {
	if err := Register(app, config); err != nil {
		panic(err)
	}
}

// Register registers the jsvm plugin in the provided app instance.
func Register(app core.App, config Config) error {
	p := &plugin{app: app, config: config}

	if p.config.HooksDir == "" {
		p.config.HooksDir = filepath.Join(app.DataDir(), "../pb_hooks")
	}

	if p.config.MigrationsDir == "" {
		p.config.MigrationsDir = filepath.Join(app.DataDir(), "../pb_migrations")
	}

	if p.config.HooksFilesPattern == "" {
		p.config.HooksFilesPattern = `^.*\.wasm$`
	}

	if p.config.MigrationsFilesPattern == "" {
		p.config.MigrationsFilesPattern = `^.*\.wasm$`
	}

	if p.config.TypesDir == "" {
		p.config.TypesDir = app.DataDir()
	}

	p.app.OnAfterBootstrap().Add(func(e *core.BootstrapEvent) error {
		// ensure that the user has the latest types declaration
		if err := p.refreshTypesFile(); err != nil {
			color.Yellow("Unable to refresh app types file: %v", err)
		}

		return nil
	})
	err := p.registerMigrations()
	if err != nil {
		return (fmt.Errorf("registerMigrations: %w", err))
	}
	err = p.registerHooks()
	if err != nil {
		return (fmt.Errorf("registerHooks: %w", err))
	}
	return nil
}

// refreshTypesFile saves the embedded TS declarations as a file on the disk.
func (p *plugin) refreshTypesFile() error {
	fullPath := p.fullTypesPath()

	// ensure that the types directory exists
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return err
	}

	// retrieve the types data to write
	data, err := generated.Types.ReadFile(typesFileName)
	if err != nil {
		return err
	}

	// read the first timestamp line of the old file (if exists) and compare it to the embedded one
	// (note: ignore errors to allow always overwriting the file if it is invalid)
	existingFile, err := os.Open(fullPath)
	if err == nil {
		timestamp := make([]byte, 13)
		io.ReadFull(existingFile, timestamp)
		existingFile.Close()

		if len(data) >= len(timestamp) && bytes.Equal(data[:13], timestamp) {
			return nil // nothing new to save
		}
	}

	return os.WriteFile(fullPath, data, 0644)
}

// fullTypesPathReturns returns the full path to the generated TS file.
func (p *plugin) fullTypesPath() string {
	return filepath.Join(p.config.TypesDir, typesFileName)
}

// registerMigrations registers the JS migrations loader.
func (p *plugin) registerMigrations() error {
	// fetch all js migrations sorted by their filename
	files, err := filesContent(p.config.MigrationsDir, p.config.MigrationsFilesPattern)
	if err != nil {
		return err
	}
	var _err = make(chan error)
	go func() {
		// vm := goja.New()
		for file, content := range files {
			vm, ctx, vmConfig := p.newVM()
			defer (*vm).Close(ctx)

			envBinds(vm, &ctx, &file)
			// dbxBinds(vm)
			// tokensBinds(vm)
			// securityBinds(vm)
			// osBinds(vm)
			// filepathBinds(vm)
			// httpClientBinds(vm)
			if p.config.OnInit != nil {
				p.config.OnInit(vm)
			}

			// Create a new context
			_, err := (*vm).InstantiateWithConfig(ctx, content, *vmConfig)
			if err != nil {
				_err <- fmt.Errorf("failed to run migration %s: %w", file, err)
				return
			}
		}
		_err <- nil
	}()

	return <-_err
}

// registerHooks registers the JS migrations loader.
func (p *plugin) registerHooks() error {
	// fetch all js migrations sorted by their filename
	files, err := filesContent(p.config.HooksDir, p.config.HooksFilesPattern)
	if err != nil {
		return err
	}
	var _err = make(chan error)
	go func() {
		waiter := waitgroup.Create()
		// vm := goja.New()
		for file, content := range files {
			waiter.Inc()
			go func(file string, content []byte) {
				vm, ctx, vmConfig := p.newVM()
				defer (*vm).Close(ctx)

				envBinds(vm, &ctx, nil)
				// dbxBinds(vm)
				// tokensBinds(vm)
				// securityBinds(vm)
				// osBinds(vm)
				// filepathBinds(vm)
				// httpClientBinds(vm)
				if p.config.OnInit != nil {
					p.config.OnInit(vm)
				}
				// Create a new context
				_, err := (*vm).InstantiateWithConfig(ctx, content, *vmConfig)
				waiter.Dec()
				if err != nil {
					_err <- fmt.Errorf("failed to run hook %s: %w", file, err)
				}
			}(file, content)
		}
		waiter.Wait()
		_err <- nil
	}()
	e := <-_err

	return e
}

func (p *plugin) newVM() (*wazero.Runtime, context.Context, *wazero.ModuleConfig) {
	// First we obtain a new Lua runtime which outputs to stdout
	// Choose the context to use for function calls.
	ctx := context.Background()
	// Create a new WebAssembly Runtime.
	r := wazero.NewRuntime(ctx)
	// Combine the above into our baseline config, overriding defaults.
	config := wazero.NewModuleConfig().
		// By default, I/O streams are discarded and there's no file system.
		WithStdout(os.Stdout).
		WithStderr(os.Stderr).
		WithFS(os.DirFS(p.config.HooksDir)).
		WithArgs(os.Args...)

	for _, v := range os.Environ() {
		k := strings.Split(v, "=")
		config.WithEnv(k[0], strings.Join(k[1:], "="))
	}
	// Instantiate WASI, which implements system I/O such as console output.
	wasi_snapshot_preview1.MustInstantiate(ctx, r)
	return &r, ctx, &config
}

//////////////////// UTILITIES

func vmSet(module *wazero.HostModuleBuilder, name string, v interface{}) {
	vType := reflect.TypeOf(v)
	if vType.Kind() == reflect.Func {
		totalArgs := vType.NumIn()
		fmt.Println("Args:")
		for i := 0; i < totalArgs; i++ {
			ti := vType.In(i) // get type of i'th argument
			fmt.Println("\t", ti)
		}
		fmt.Println("Results:")
		for i := 0; i < vType.NumOut(); i++ {
			ti := vType.Out(i) // get type of i'th result
			fmt.Println("\t", ti)
		}
		(*module).
			NewFunctionBuilder().
			WithFunc(func(_ context.Context, m api.Module, offset, byteCount uint32) {
				buf, ok := m.Memory().Read(offset, byteCount)
				if ok {
					// in := make([]reflect.Value, totalArgs)
					fmt.Println(string(buf))
				}
			}).Export(name)
	}
}
func envBinds(vm *wazero.Runtime, ctx *context.Context, file *string) {
	module := (*vm).NewHostModuleBuilder("env")
	if file != nil {
		vmSet(&module, "migrate", func(up, down func(db dbx.Builder) error) {
			m.AppMigrations.Register(up, down, *file)
		})
	}
	vmSet(&module, "log", fmt.Sprintf)
	vmSet(&module, "readerToString", func(r io.Reader, maxBytes int) (string, error) {
		if maxBytes == 0 {
			maxBytes = rest.DefaultMaxMemory
		}

		limitReader := io.LimitReader(r, int64(maxBytes))

		bodyBytes, readErr := io.ReadAll(limitReader)
		if readErr != nil {
			return "", readErr
		}

		return string(bodyBytes), nil
	})

	vmSet(&module, "sleep", func(milliseconds int64) {
		time.Sleep(time.Duration(milliseconds) * time.Millisecond)
	})

	vmSet(&module, "arrayOf", func(model any) any {
		mt := reflect.TypeOf(model)
		st := reflect.SliceOf(mt)
		elem := reflect.New(st).Elem()

		return elem.Addr().Interface()
	})

	// vmSet(&module, "DynamicModel", func(call goja.ConstructorCall) *goja.Object {
	// 	shape, ok := call.Argument(0).Export().(map[string]any)
	// 	if !ok || len(shape) == 0 {
	// 		panic("[DynamicModel] missing shape data")
	// 	}

	// 	instance := newDynamicModel(shape)
	// 	instanceValue := vm.ToValue(instance).(*goja.Object)
	// 	instanceValue.SetPrototype(call.This.Prototype())

	// 	return instanceValue
	// })

	// vmSet(&module, "Record", func(call goja.ConstructorCall) *goja.Object {
	// 	var instance *models.Record

	// 	collection, ok := call.Argument(0).Export().(*models.Collection)
	// 	if ok {
	// 		instance = models.NewRecord(collection)
	// 		data, ok := call.Argument(1).Export().(map[string]any)
	// 		if ok {
	// 			instance.Load(data)
	// 		}
	// 	} else {
	// 		instance = &models.Record{}
	// 	}

	// 	instanceValue := vm.ToValue(instance).(*goja.Object)
	// 	instanceValue.SetPrototype(call.This.Prototype())

	// 	return instanceValue
	// })

	// vmSet(&module, "Collection", func(call goja.ConstructorCall) *goja.Object {
	// 	instance := &models.Collection{}
	// 	return structConstructorUnmarshal(vm, call, instance)
	// })

	// vmSet(&module, "Admin", func(call goja.ConstructorCall) *goja.Object {
	// 	instance := &models.Admin{}
	// 	return structConstructorUnmarshal(vm, call, instance)
	// })

	// vmSet(&module, "Schema", func(call goja.ConstructorCall) *goja.Object {
	// 	instance := &schema.Schema{}
	// 	return structConstructorUnmarshal(vm, call, instance)
	// })

	// vmSet(&module, "SchemaField", func(call goja.ConstructorCall) *goja.Object {
	// 	instance := &schema.SchemaField{}
	// 	return structConstructorUnmarshal(vm, call, instance)
	// })

	// vmSet(&module, "MailerMessage", func(call goja.ConstructorCall) *goja.Object {
	// 	instance := &mailer.Message{}
	// 	return structConstructor(vm, call, instance)
	// })

	// vmSet(&module, "Command", func(call goja.ConstructorCall) *goja.Object {
	// 	instance := &cobra.Command{}
	// 	return structConstructor(vm, call, instance)
	// })

	// vmSet(&module, "RequestInfo", func(call goja.ConstructorCall) *goja.Object {
	// 	instance := &models.RequestInfo{Context: models.RequestInfoContextDefault}
	// 	return structConstructor(vm, call, instance)
	// })

	// vmSet(&module, "DateTime", func(call goja.ConstructorCall) *goja.Object {
	// 	instance := types.NowDateTime()

	// 	val, _ := call.Argument(0).Export().(string)
	// 	if val != "" {
	// 		instance, _ = types.ParseDateTime(val)
	// 	}

	// 	instanceValue := vm.ToValue(instance).(*goja.Object)
	// 	instanceValue.SetPrototype(call.This.Prototype())

	// 	return structConstructor(vm, call, instance)
	// })

	// vmSet(&module, "ValidationError", func(call goja.ConstructorCall) *goja.Object {
	// 	code, _ := call.Argument(0).Export().(string)
	// 	message, _ := call.Argument(1).Export().(string)

	// 	instance := validation.NewError(code, message)
	// 	instanceValue := vm.ToValue(instance).(*goja.Object)
	// 	instanceValue.SetPrototype(call.This.Prototype())

	// 	return instanceValue
	// })

	// vmSet(&module, "Dao", func(call goja.ConstructorCall) *goja.Object {
	// 	concurrentDB, _ := call.Argument(0).Export().(dbx.Builder)
	// 	if concurrentDB == nil {
	// 		panic("[Dao] missing required Dao(concurrentDB, [nonconcurrentDB]) argument")
	// 	}

	// 	nonConcurrentDB, _ := call.Argument(1).Export().(dbx.Builder)
	// 	if nonConcurrentDB == nil {
	// 		nonConcurrentDB = concurrentDB
	// 	}

	// 	instance := daos.NewMultiDB(concurrentDB, nonConcurrentDB)
	// 	instanceValue := vm.ToValue(instance).(*goja.Object)
	// 	instanceValue.SetPrototype(call.This.Prototype())

	// 	return instanceValue
	// })

	// vmSet(&module, "Cookie", func(call goja.ConstructorCall) *goja.Object {
	// 	instance := &http.Cookie{}
	// 	return structConstructor(vm, call, instance)
	// })

	// vmSet(&module, "SubscriptionMessage", func(call goja.ConstructorCall) *goja.Object {
	// 	instance := &subscriptions.Message{}
	// 	return structConstructor(vm, call, instance)
	// })
	module.Instantiate((*ctx))
}

// filesContent returns a map with all direct files within the specified dir and their content.
//
// If directory with dirPath is missing or no files matching the pattern were found,
// it returns an empty map and no error.
//
// If pattern is empty string it matches all root files.
func filesContent(dirPath string, pattern string) (map[string][]byte, error) {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string][]byte{}, nil
		}
		return nil, err
	}

	var exp *regexp.Regexp
	if pattern != "" {
		var err error
		if exp, err = regexp.Compile(pattern); err != nil {
			return nil, err
		}
	}

	result := map[string][]byte{}

	for _, f := range files {
		if f.IsDir() || (exp != nil && !exp.MatchString(f.Name())) {
			continue
		}

		raw, err := os.ReadFile(filepath.Join(dirPath, f.Name()))
		if err != nil {
			return nil, err
		}

		result[f.Name()] = raw
	}

	return result, nil
}
