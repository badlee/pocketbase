package jsvm

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/dop251/goja"
	"github.com/influx6/faux/pattern"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/zishang520/engine.io/v2/events"
	_types "github.com/zishang520/engine.io/v2/types"
	"github.com/zishang520/socket.io/v2/socket"
)

var socketRoomEvents = []string{"create", "delete", "join", "leave"}

type socketEventListener = func(client *socket.Socket) events.Listener
type socketEvent struct {
	once     bool
	fnEvent  []*events.Listener
	Listener []socketEventListener
}

type SocketIO_NS struct {
	ns *socket.Namespace
}
type SocketIO_Clients struct {
	clients map[string]*SocketIO_Client
	vm      goja.Runtime
}

// Delete implements goja.DynamicObject.
func (socket *SocketIO_Clients) Delete(key string) bool {
	return false
}

// Get implements goja.DynamicObject.
func (socket *SocketIO_Clients) Get(key string) goja.Value {
	return socket.vm.ToValue(socket.clients[key])
}

// Has implements goja.DynamicObject.
func (socket *SocketIO_Clients) Has(key string) bool {
	_, found := socket.clients[key]
	return found
}

// Keys implements goja.DynamicObject.
func (socket *SocketIO_Clients) Keys() []string {
	keys := make([]string, 0, len(socket.clients))
	for k := range socket.clients {
		keys = append(keys, k)
	}
	return keys
}

// Set implements goja.DynamicObject.
func (socket *SocketIO_Clients) Set(key string, val goja.Value) bool {
	return false
}

func (socket *SocketIO_Clients) RemoveSocket(client *socket.Socket) {
	delete(socket.clients, string(client.Id()))
}

func (socket *SocketIO_Clients) GetSocket(client *socket.Socket) *SocketIO_Client {
	return socket.clients[string(client.Id())]
}
func (socket *SocketIO_Clients) GetSocketFromString(clientId string) *SocketIO_Client {
	return socket.clients[clientId]
}
func (socket *SocketIO_Clients) AddSocket(client *socket.Socket) {
	socket.clients[string(client.Id())] = &SocketIO_Client{
		client,
		string(client.Id()),
		false,
		&SocketIO_NS{
			client.Nsp(),
		},
	}
}

type SocketIO_Client struct {
	client    *socket.Socket
	Id        string
	Rejected  bool
	Namespace *SocketIO_NS
}

func socketIOSharedBinds(loader *goja.Runtime) {
	obj := loader.NewObject()
	loader.Set("SocketIO", obj)
	obj.Set("on", func(event string, listener events.Listener) func() {
		err := apis.SocketIO.On(string(event), listener)
		if err != nil {
			panic(err)
		}
		return func() {
			apis.SocketIO.RemoveListener(events.EventName(event), listener)
		}
	})
	obj.Set("once", func(event string, listener events.Listener) func() {
		apis.SocketIO.Once(event, listener)
		return func() {
			apis.SocketIO.RemoveListener(events.EventName(event), listener)
		}
	})
	obj.Set("emit", func(event string, data ...any) {
		apis.SocketIO.ServerSideEmit(event, data...)
	})
	obj.Set("ack", func(event string, data ...any) func(func(err error, args ...any)) {
		ackFn := []func(err error, args ...any){}
		apis.SocketIO.ServerSideEmitWithAck(event, data, func(args []any, err error) {
			for _, ack := range ackFn {
				ack(err, args...)
			}
			ackFn = nil // release allocated memory
		})
		return func(ack func(err error, args ...any)) {
			ackFn = append(ackFn, ack)
		}
	})
	obj.Set("emitIn", func(room socket.Room, event string, data ...any) {
		apis.SocketIO.In(room).Emit(event, data...)
	})
	obj.Set("ackIn", func(room socket.Room, event string, data ...any) func(func(err error, args ...any)) {
		ackFn := []func(err error, args ...any){}
		apis.SocketIO.In(room).EmitWithAck(event, data, func(args []any, err error) {
			for _, ack := range ackFn {
				ack(err, args...)
			}
			ackFn = nil // release allocated memory
		})
		return func(ack func(err error, args ...any)) {
			ackFn = append(ackFn, ack)
		}
	})
	obj.Set("emitInAllExcept", func(rooms []socket.Room, event string, data ...any) {
		apis.SocketIO.Except(rooms...).Emit(event, data...)
	})
	obj.Set("ackInAllExcept", func(room socket.Room, event string, data ...any) func(func(err error, args ...any)) {
		ackFn := []func(err error, args ...any){}
		apis.SocketIO.Except(room).EmitWithAck(event, data, func(args []any, err error) {
			for _, ack := range ackFn {
				ack(err, args...)
			}
			ackFn = nil // release allocated memory
		})
		return func(ack func(err error, args ...any)) {
			ackFn = append(ackFn, ack)
		}
	})
	obj.Set("sockets", func(data ...any) map[socket.SocketId]*socket.Socket {
		var s = apis.SocketIO.Sockets().Sockets()
		roomInfo := data[0]
		if roomInfo != nil {
			roomValue, found := roomInfo.(string)
			if found {
				s = &_types.Map[socket.SocketId, *socket.Socket]{}
				apis.SocketIO.In(socket.Room(roomValue)).FetchSockets()(func(rs []*socket.RemoteSocket, err error) {
					for _, rs2 := range rs {
						client, isOk := apis.SocketIO.Sockets().Sockets().Load(rs2.Id())
						if isOk {
							s.Store(rs2.Id(), client)
						}
					}
				})
			}
		}
		clients := make(map[socket.SocketId]*socket.Socket)
		for _, si := range s.Keys() {
			client, exist := s.Load(si)
			if exist {
				clients[si] = client
			}
		}
		return clients
	})
	obj.Set("socket", func(data ...any) *socket.Socket {
		clientInfo := data[0]
		if clientInfo != nil {
			clientValue, found := clientInfo.(string)
			if found {
				client, isOk := apis.SocketIO.Sockets().Sockets().Load(socket.SocketId(clientValue))
				if isOk {
					return client
				}
			}
		}
		return nil
	})
	obj.Set("rooms", func(rooms ...socket.Room) *socket.BroadcastOperator {
		return apis.SocketIO.In(rooms...)
	})
	obj.Set("roomsExcept", func(rooms ...socket.Room) *socket.BroadcastOperator {
		return apis.SocketIO.Except(rooms...)
	})
}

func sockeIOEchoHandler(_ core.App, loader *goja.Runtime, _ *vmsPool) func(namespace string) *goja.Object {
	return func(namespace string) *goja.Object {
		io := apis.SocketIO.Sockets()
		var rooms map[string]*goja.Object = map[string]*goja.Object{}
		var roomEvents map[string]*socketEvent = map[string]*socketEvent{}
		if namespace != "" {
			p := pattern.New(namespace)
			io = apis.SocketIO.Of(func(name string, a any, next func(error, bool)) {
				_, _, found := p.Validate(name)
				next(nil, found)
			}, nil)
		}
		clients := &SocketIO_Clients{}
		io.On("connection", func(args ...any) {
			client := args[0].(*socket.Socket)
			clients.AddSocket(client)
			client.Emit("$SYS_WELCOM")
			client.On("disconnecting", func(args ...any) {
				clients.RemoveSocket(client)
				client.Emit("$SYS_BYE")
			})
		})
		io.On("create-room", func(a ...any) {
			// TODO: New room
		})
		io.On("delete-room", func(a ...any) {
			Room := a[0].(socket.Room)
			isRoom := regexp.MustCompile("^" + string(Room) + "::")
			eventNames := []string{}
			for k := range roomEvents {
				if isRoom.MatchString(k) {
					eventNames = append(eventNames, k)
				}
			}
			io.Sockets().Range(func(SocketId socket.SocketId, Socket *socket.Socket) bool {
				if Socket.Rooms().Has(Room) {
					defer func() {
						go func() {
							for _, eventName := range eventNames {
								Socket.RemoveAllListeners(events.EventName(eventName))
							}
						}()
					}()
				}
				return true
			})
		})
		obj := loader.NewObject()
		obj.Set("sockets", loader.NewDynamicObject(clients))
		obj.Set("room", func(Room string) *goja.Object {
			roomFound, isFound := rooms[Room]
			if isFound {
				return roomFound
			}
			obj := loader.NewObject()
			rooms[Room] = obj
			OnOnce := func(once bool, event string, fn func(client goja.Value, a ...any)) {
				eventName := Room + "::" + strings.ToLower(event)
				switch strings.ToLower(event) {
				case "delete":
					var fnEvent events.Listener = func(a ...any) {
						room := a[0].(socket.Room)
						if strings.EqualFold(strings.ToLower(Room), strings.ToLower(fmt.Sprintf("%v", room))) {
							id := a[1].(socket.SocketId)
							client, isOk := io.Sockets().Load(id)
							if isOk {
								for eventName, eventListener := range roomEvents {
									if !slices.Contains(socketRoomEvents, strings.Replace(eventName, Room+"::", "", 1)) {
										for _, fn := range eventListener.Listener {
											if eventListener.once {
												client.EventEmitter.Once(events.EventName(eventName), fn(client))
											} else {
												client.EventEmitter.On(events.EventName(eventName), fn(client))
											}
										}
									}
								}
								fn(clients.Get(string(client.Id())), func() {
									clients.GetSocket(client).Rejected = true
									client.Leave(room)
								})

							}
						}
					}
					_, isFound := roomEvents[eventName]
					if isFound {
						roomEvents[eventName].fnEvent = append(roomEvents[eventName].fnEvent, &fnEvent)
					} else {
						roomEvents[eventName] = &socketEvent{
							once,
							[]*events.Listener{&fnEvent},
							[]socketEventListener{},
						}
					}
					if once {
						io.Once(strings.ToLower(event)+"-room", *roomEvents[eventName].fnEvent[len(roomEvents[eventName].fnEvent)-1])
					} else {
						io.On(strings.ToLower(event)+"-room", *roomEvents[eventName].fnEvent[len(roomEvents[eventName].fnEvent)-1])
					}
					// break
				case "join":
					var fnEvent events.Listener = func(a ...any) {
						room := a[0].(socket.Room)
						if strings.EqualFold(strings.ToLower(Room), strings.ToLower(fmt.Sprintf("%v", room))) {
							id := a[1].(socket.SocketId)
							client, isOk := io.Sockets().Load(id)
							if isOk {
								for eventName, eventListener := range roomEvents {
									if !slices.Contains(socketRoomEvents, strings.Replace(eventName, Room+"::", "", 1)) {
										for _, fn := range eventListener.Listener {
											if eventListener.once {
												client.EventEmitter.Once(events.EventName(eventName), fn(client))
											} else {
												client.EventEmitter.On(events.EventName(eventName), fn(client))
											}
										}
									}
								}
								fn(clients.Get(string(client.Id())), func() {
									clients.GetSocket(client).Rejected = true
									client.Leave(room)
								})

							}
						}
					}
					_, isFound := roomEvents[eventName]
					if isFound {
						roomEvents[eventName].fnEvent = append(roomEvents[eventName].fnEvent, &fnEvent)
					} else {
						roomEvents[eventName] = &socketEvent{
							once,
							[]*events.Listener{&fnEvent},
							[]socketEventListener{},
						}
					}
					if once {
						io.Once(strings.ToLower(event)+"-room", *roomEvents[eventName].fnEvent[len(roomEvents[eventName].fnEvent)-1])
					} else {
						io.On(strings.ToLower(event)+"-room", *roomEvents[eventName].fnEvent[len(roomEvents[eventName].fnEvent)-1])
					}
					// break
				case "leave":
					var fnEvent events.Listener = func(a ...any) {
						room := a[0].(socket.Room)
						if strings.EqualFold(strings.ToLower(Room), strings.ToLower(fmt.Sprintf("%v", room))) {
							id := a[1].(socket.SocketId)
							client, found := clients.clients[string(id)]
							if found {
								for eventName := range roomEvents {
									if !slices.Contains(socketRoomEvents, strings.Replace(eventName, Room+"::", "", 1)) {
										client.client.EventEmitter.RemoveAllListeners(events.EventName(eventName))
									}
								}
								fn(clients.Get(client.Id), client.Rejected)
							}
						}
					}
					_, isFound := roomEvents[eventName]
					if isFound {
						roomEvents[eventName].fnEvent = append(roomEvents[eventName].fnEvent, &fnEvent)
					} else {
						roomEvents[eventName] = &socketEvent{
							once,
							[]*events.Listener{&fnEvent},
							[]socketEventListener{},
						}
					}
					if once {
						io.Once(strings.ToLower(event)+"-room", *roomEvents[eventName].fnEvent[len(roomEvents[eventName].fnEvent)-1])
					} else {
						io.On(strings.ToLower(event)+"-room", *roomEvents[eventName].fnEvent[len(roomEvents[eventName].fnEvent)-1])
					}
					// break
				default:
					fnEvent := func(client *socket.Socket) events.Listener {
						return func(args ...any) {
							fn(loader.ToValue(client), args...)
						}
					}
					_, isFound := roomEvents[eventName]
					if isFound {
						roomEvents[eventName].Listener = append(roomEvents[eventName].Listener, fnEvent)
					} else {
						roomEvents[eventName] = &socketEvent{
							once,
							[]*events.Listener{},
							[]socketEventListener{fnEvent},
						}
					}
					io.Sockets().Range(func(SocketId socket.SocketId, Socket *socket.Socket) bool {
						if Socket.Rooms().Has(socket.Room(Room)) {
							if once {
								Socket.Once(eventName, roomEvents[eventName].Listener[len(roomEvents[eventName].Listener)-1](Socket))
							} else {
								Socket.On(eventName, roomEvents[eventName].Listener[len(roomEvents[eventName].Listener)-1](Socket))
							}
						}
						return true
					})
					// break

				}
			}

			obj.Set("on", func(event string, fn func(client goja.Value, a ...any)) {
				OnOnce(false, event, fn)
			})
			obj.Set("once", func(event string, fn func(client goja.Value, a ...any)) {
				OnOnce(true, event, fn)
			})
			obj.Set("off", func(event string) {
				io.Sockets().Range(func(SocketId socket.SocketId, Socket *socket.Socket) bool {
					if Socket.Rooms().Has(socket.Room(Room)) {
						eventName := Room + "::" + strings.ToLower(event)
						Socket.EventEmitter.RemoveAllListeners(events.EventName(eventName))
					}
					return true
				})
			})
			return obj
		})
		OnOnce := func(once bool, name string, args ...goja.Value) *goja.Object {
			var err error
			values := []events.Listener{}
			for _, value := range args {
				fn, isFn := goja.AssertFunction(value)
				if isFn {
					args = args[:len(args)-1]
					values = append(values, func(args ...any) {
						values := []goja.Value{}
						for _, value := range args {
							values = append(values, loader.ToValue(value))
						}
						fn(loader.Get("SocketIO").ToObject(loader), values...)
					})
				}
			}
			if once {
				err = io.Once(name, values...)
			} else {
				err = io.On(name, values...)
			}
			if err != nil {
				return loader.NewGoError(err)
			}
			return nil
		}
		obj.Set("emit", func(name string, args ...goja.Value) *goja.Object {
			last := args[len(args)-1]
			var fn *goja.Callable
			var err error

			if last != nil {
				f, isFn := goja.AssertFunction(last)
				if isFn {
					args = args[:len(args)-1]
					fn = &f
				}
			}
			values := []any{}
			for _, value := range args {
				values = append(values, value.Export())
			}
			if fn == nil {
				err = io.Local().Emit(name, values...)
			} else {
				ack := io.Local().EmitWithAck(name, values...)
				ack(func(args []any, err error) {
					values := []goja.Value{}
					if err != nil {
						values = append(values, loader.NewGoError(err))
					} else {
						values = append(values, goja.Null())
						for _, value := range args {
							values = append(values, loader.ToValue(value))
						}
					}
					(*fn)(loader.Get("SocketIO").ToObject(loader), values...)
				})

			}
			if err != nil {
				return loader.NewGoError(err)
			}
			return nil
		})
		obj.Set("on", func(name string, args ...goja.Value) *goja.Object {
			return OnOnce(false, name, args...)
		})
		obj.Set("once", func(name string, args ...goja.Value) *goja.Object {
			return OnOnce(true, name, args...)
		})
		return obj
	}
}

func socketIOBinds(app core.App, loader *goja.Runtime, executors *vmsPool) (*goja.Object, *func(namespace string) *goja.Object) {
	io := sockeIOEchoHandler(app, loader, executors)("")
	ioNs := func(namespace string) *goja.Object {
		if namespace == "" || strings.ToLower(namespace) == "@local" || strings.ToLower(namespace) == "@global" {
			return io
		}
		var re = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)
		if re.MatchString(namespace) {
			panic("[SockeIO NameSpace] can must containt one or more of these characteres A to Z,0 to 9, _ and -")
		}
		return sockeIOEchoHandler(app, loader, executors)(namespace)
	}
	registerFactoryAsConstructor(loader, "SocketIO", func() *goja.Object {
		return io
	})
	injectFactoryAsConstructor(loader, loader.Get("SocketIO").ToObject(loader), "Namespace", func(name string) *goja.Object {
		return ioNs(name)
	})
	return io, &ioNs
}

func injectFactoryAsConstructor(vm *goja.Runtime, obj *goja.Object, constructorName string, factoryFunc any) {
	rv := reflect.ValueOf(factoryFunc)
	rt := reflect.TypeOf(factoryFunc)
	totalArgs := rt.NumIn()

	obj.Set(constructorName, func(call goja.ConstructorCall) *goja.Object {
		args := make([]reflect.Value, totalArgs)

		for i := 0; i < totalArgs; i++ {
			v := call.Argument(i).Export()

			// use the arg type zero value
			if v == nil {
				args[i] = reflect.New(rt.In(i)).Elem()
			} else if number, ok := v.(int64); ok {
				// goja uses int64 for "int"-like numbers but we rarely do that and use int most of the times
				// (at later stage we can use reflection on the arguments to validate the types in case this is not sufficient anymore)
				args[i] = reflect.ValueOf(int(number))
			} else {
				args[i] = reflect.ValueOf(v)
			}
		}

		result := rv.Call(args)

		if len(result) != 1 {
			panic("the factory function should return only 1 item")
		}

		value := vm.ToValue(result[0].Interface()).(*goja.Object)
		value.SetPrototype(call.This.Prototype())

		return value
	})
}
