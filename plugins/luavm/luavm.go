// Package jsvm implements pluggable utilities for binding a JS goja runtime
// to the PocketBase instance (loading migrations, attaching to app hooks, etc.).
//
// Example:
//
//	luavm.MustRegister(app, jsvm.Config{
//		WatchHooks: true,
//	})
package luavm

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"time"

	m "github.com/pocketbase/pocketbase/migrations"

	rtlib "github.com/arnodel/golua/lib"
	rt "github.com/arnodel/golua/runtime"
	"github.com/fatih/color"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/plugins/luavm/internal/types/generated"
	"github.com/pocketbase/pocketbase/tools/rest"
)

const (
	typesFileName = "types.d.ts"
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
	OnInit func(vm *rt.Runtime)

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
	// If not set it fallbacks to `^.*(\.pb\.js|\.pb\.ts)$`, aka. any
	// HookdsDir file ending in ".pb.js" or ".pb.ts" (the last one is to enforce IDE linters).
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

	// If not set it fallbacks to `^.*(\.js|\.ts)$`, aka. any MigrationDir file
	// ending in ".js" or ".ts" (the last one is to enforce IDE linters).
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
//		OnInit: func(vm *luavm.Runtime) {
//			// register custom bindings
//			vmSet(vm,"myCustomVar", 123)
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
		p.config.HooksFilesPattern = `^.*(\.pb\.lua|\.pb\.lua)$`
	}

	if p.config.MigrationsFilesPattern == "" {
		p.config.MigrationsFilesPattern = `^.*(\.lua|\.lua)$`
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
	// err = p.registerHooks()
	// if err != nil {
	// 	return (fmt.Errorf("registerHooks: %w", err))
	// }
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
			vm := p.newVM()
			baseBinds(vm)
			// dbxBinds(vm)
			// tokensBinds(vm)
			// securityBinds(vm)
			// osBinds(vm)
			// filepathBinds(vm)
			// httpClientBinds(vm)
			if p.config.OnInit != nil {
				p.config.OnInit(vm)
			}
			vmSet(vm, "migrate", func(up, down func(db dbx.Builder) error) {
				m.AppMigrations.Register(up, down, file)
			})
			_, err := vm.CompileAndLoadLuaChunk(file, content, rt.TableValue(vm.GlobalEnv()))
			if err != nil {
				_err <- fmt.Errorf("failed to run migration %s: %w", file, err)
				return
			}
		}
		_err <- nil
		// defer loop.Stop()
	}()

	return <-_err
}

func (p *plugin) newVM() *rt.Runtime {
	// First we obtain a new Lua runtime which outputs to stdout
	r := rt.New(os.Stdout)

	// Load the basic library into the runtime (we need print)
	rtlib.LoadAll(r)
	return r
}

//////////////////// UTILITIES

func vmSet(vm *rt.Runtime, name string, v interface{}) {
	vType := reflect.TypeOf(v)
	if vType.Kind() == reflect.Func {
		fmt.Println("Args:")
		for i := 0; i < vType.NumIn(); i++ {
			ti := vType.In(i) // get type of i'th argument
			fmt.Println("\t", ti)
		}
		fmt.Println("Results:")
		for i := 0; i < vType.NumOut(); i++ {
			ti := vType.Out(i) // get type of i'th result
			fmt.Println("\t", ti)
		}
	}
}
func baseBinds(vm *rt.Runtime) {

	vmSet(vm, "readerToString", func(r io.Reader, maxBytes int) (string, error) {
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

	vmSet(vm, "sleep", func(milliseconds int64) {
		time.Sleep(time.Duration(milliseconds) * time.Millisecond)
	})

	vmSet(vm, "arrayOf", func(model any) any {
		mt := reflect.TypeOf(model)
		st := reflect.SliceOf(mt)
		elem := reflect.New(st).Elem()

		return elem.Addr().Interface()
	})

	// vmSet(vm, "DynamicModel", func(call goja.ConstructorCall) *goja.Object {
	// 	shape, ok := call.Argument(0).Export().(map[string]any)
	// 	if !ok || len(shape) == 0 {
	// 		panic("[DynamicModel] missing shape data")
	// 	}

	// 	instance := newDynamicModel(shape)
	// 	instanceValue := vm.ToValue(instance).(*goja.Object)
	// 	instanceValue.SetPrototype(call.This.Prototype())

	// 	return instanceValue
	// })

	// vmSet(vm, "Record", func(call goja.ConstructorCall) *goja.Object {
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

	// vmSet(vm, "Collection", func(call goja.ConstructorCall) *goja.Object {
	// 	instance := &models.Collection{}
	// 	return structConstructorUnmarshal(vm, call, instance)
	// })

	// vmSet(vm, "Admin", func(call goja.ConstructorCall) *goja.Object {
	// 	instance := &models.Admin{}
	// 	return structConstructorUnmarshal(vm, call, instance)
	// })

	// vmSet(vm, "Schema", func(call goja.ConstructorCall) *goja.Object {
	// 	instance := &schema.Schema{}
	// 	return structConstructorUnmarshal(vm, call, instance)
	// })

	// vmSet(vm, "SchemaField", func(call goja.ConstructorCall) *goja.Object {
	// 	instance := &schema.SchemaField{}
	// 	return structConstructorUnmarshal(vm, call, instance)
	// })

	// vmSet(vm, "MailerMessage", func(call goja.ConstructorCall) *goja.Object {
	// 	instance := &mailer.Message{}
	// 	return structConstructor(vm, call, instance)
	// })

	// vmSet(vm, "Command", func(call goja.ConstructorCall) *goja.Object {
	// 	instance := &cobra.Command{}
	// 	return structConstructor(vm, call, instance)
	// })

	// vmSet(vm, "RequestInfo", func(call goja.ConstructorCall) *goja.Object {
	// 	instance := &models.RequestInfo{Context: models.RequestInfoContextDefault}
	// 	return structConstructor(vm, call, instance)
	// })

	// vmSet(vm, "DateTime", func(call goja.ConstructorCall) *goja.Object {
	// 	instance := types.NowDateTime()

	// 	val, _ := call.Argument(0).Export().(string)
	// 	if val != "" {
	// 		instance, _ = types.ParseDateTime(val)
	// 	}

	// 	instanceValue := vm.ToValue(instance).(*goja.Object)
	// 	instanceValue.SetPrototype(call.This.Prototype())

	// 	return structConstructor(vm, call, instance)
	// })

	// vmSet(vm, "ValidationError", func(call goja.ConstructorCall) *goja.Object {
	// 	code, _ := call.Argument(0).Export().(string)
	// 	message, _ := call.Argument(1).Export().(string)

	// 	instance := validation.NewError(code, message)
	// 	instanceValue := vm.ToValue(instance).(*goja.Object)
	// 	instanceValue.SetPrototype(call.This.Prototype())

	// 	return instanceValue
	// })

	// vmSet(vm, "Dao", func(call goja.ConstructorCall) *goja.Object {
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

	// vmSet(vm, "Cookie", func(call goja.ConstructorCall) *goja.Object {
	// 	instance := &http.Cookie{}
	// 	return structConstructor(vm, call, instance)
	// })

	// vmSet(vm, "SubscriptionMessage", func(call goja.ConstructorCall) *goja.Object {
	// 	instance := &subscriptions.Message{}
	// 	return structConstructor(vm, call, instance)
	// })
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
