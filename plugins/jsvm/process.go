package jsvm

import (
	"os"
	"strings"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/require"
)

const ModuleName = "process"

type Process struct {
	env map[string]string
}

func ProcessRequire(loop *EventLoop) func(runtime *goja.Runtime, module *goja.Object) {
	return func(runtime *goja.Runtime, module *goja.Object) {
		p := &Process{
			env: make(map[string]string),
		}

		for _, e := range os.Environ() {
			envKeyValue := strings.SplitN(e, "=", 2)
			p.env[envKeyValue[0]] = envKeyValue[1]
		}

		o := module.Get("exports").(*goja.Object)
		o.Set("env", p.env)
		o.Set("args", os.Args)
		o.Set("cwd", os.Getwd)
		o.Set("stop", func(code int) {
			if loop.running {
				loop.Stop()
			}
			os.Exit(code)
		})
		o.Set("exit", func(code int) {
			if loop.running {
				loop.StopNoWait()
			}
			os.Exit(code)
		})
	}
}

func ProcessEnable(runtime *goja.Runtime, loop *EventLoop) {
	require.RegisterCoreModule(ModuleName, ProcessRequire(loop))
	runtime.Set("process", require.Require(runtime, ModuleName))
}
