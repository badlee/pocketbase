package apis

import (
	"github.com/zishang520/engine.io/v2/config"
	"github.com/zishang520/socket.io/v2/socket"
)

var SocketIO *socket.Server

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
