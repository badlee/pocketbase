package apis

import (
	"github.com/labstack/echo/v5"
	"github.com/pocketbase/pocketbase/core"
	"github.com/zishang520/engine.io/v2/config"
	"github.com/zishang520/socket.io/v2/socket"
)

var SocketIO *socket.Server

func bindSocketIO(app core.App, router *echo.Echo) {
	path := "/socket.io"
	if flag, found, _ := app.Config().RootCmd.Find([]string{"serve"}); len(found) == 0 && flag.Name() == "serve" {
		if flag := flag.Flags().Lookup("socket-io-path"); flag != nil {
			path = flag.Value.String()
		}
	}
	router.GET(path, echo.WrapHandler(SocketIO.ServeHandler(nil)))
}

func init() {
	var configServer = &socket.ServerOptions{
		ServerOptions: config.ServerOptions{},
	}
	configServer.SetCleanupEmptyChildNamespaces(false)
	configServer.SetAllowEIO3(true)
	configServer.SetServeClient(true)
	configServer.SetPath("/")
	io := socket.NewServer(nil, configServer)
	io.On("connection", func(clients ...any) {
		client := clients[0].(*socket.Socket)
		client.Emit("request" /* … */) // emit an event to the socket
	})
	SocketIO = io
}
