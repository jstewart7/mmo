package mmo

import (
	"log"
	"net"

	"go.nanomsg.org/mangos/v3"

	"github.com/unitoftime/ecs"
	"github.com/unitoftime/flow/physics"
	"github.com/unitoftime/mmo/serdes"
)
type Websocket struct {
	net.Conn
}
func (t *Websocket) ComponentSet(val interface{}) { *t = val.(Websocket) }

func ClientSendUpdate(world *ecs.World, conn net.Conn) {
	ecs.Map2(world, func(id ecs.Id, _ *ClientOwned, input *physics.Input) {
		update := serdes.WorldUpdate{
			WorldData: map[ecs.Id][]ecs.Component{
				id: []ecs.Component{ecs.C(*input)},
			},
		}
		log.Println("ClientSendUpdate:", update)
		serializedInput, err := serdes.MarshalWorldUpdateMessage(update)
		if err != nil {
			log.Println("Flatbuffers, Failed to serialize", err)
		}

		_, err = conn.Write(serializedInput)
		if err != nil {
			log.Println("Error Sending:", err)
			return
		}
	})
}

func ClientReceive(world *ecs.World, conn net.Conn, networkChannel chan serdes.WorldUpdate) {
	const MaxMsgSize int = 4 * 1024

	msg := make([]byte, MaxMsgSize)
	for {
		n, err := conn.Read(msg)

		if err != nil {
			log.Println("Read Error:", err)
			return
		}
		if n <= 0 { continue }

		fbMessage, err := serdes.UnmarshalMessage(msg)
		if err != nil {
			log.Println("Failed to unmarshal:", err)
			continue
		}

		switch t := fbMessage.(type) {
		case serdes.WorldUpdate:
			// log.Println(t)
			networkChannel <- t
		case serdes.ClientLoginResp:
			log.Println("serdes.ClientLoginResp", t)
			// ecs.Write(engine, ecs.Id(t.Id), ClientOwned{})
			// ecs.Write(engine, ecs.Id(t.Id), Body{})
			ecs.Write(world, ecs.Id(t.Id), ecs.C(ClientOwned{}), ecs.C(Body{}))
		default:
			panic("Unknown message type")
		}
	}
}

func ServerSendUpdate(world *ecs.World, sock mangos.Socket) {
	// transformList := make([]TransformUpdate, 0)

	update := serdes.WorldUpdate{
		UserId: 0,
		WorldData: make(map[ecs.Id][]ecs.Component),
	}

	{
		ecs.Map2(world, func(id ecs.Id, transform *physics.Transform, body *Body) {
			compList := []ecs.Component{
				ecs.C(*transform),
				ecs.C(*body),
			}
			update.WorldData[id] = compList
		})
	}

	{
		ecs.Map(world, func(id ecs.Id, user *User) {
			log.Println("ServerSendUpdate WorldUpdate:", update)

			update.UserId = user.Id
			serializedUpdate, err := serdes.MarshalWorldUpdateMessage(update)
			if err != nil {
				log.Println("Error Marshalling", err)
				return
			}

			err = sock.Send(serializedUpdate)
			if err != nil {
				log.Println("Error Sending:", err)
				return
			}
		})
	}
}

func ServeProxyConnection(sock mangos.Socket, world *ecs.World, networkChannel chan serdes.WorldUpdate) {
	log.Println("Server: ServeProxyConnection")
	loginMap := make(map[uint64]ecs.Id)

	// Read data
	for {
		msg, err := sock.Recv()
		if err != nil {
			log.Println("Read Error:", err)
		}

		fbMessage, err := serdes.UnmarshalMessage(msg)
		if err != nil {
			log.Println("Failed to unmarshal:", err)
			continue
		}

		// Interpret different messages
		switch t := fbMessage.(type) {
		case serdes.WorldUpdate:
			id := loginMap[t.UserId]
			// TODO - requires client to put their input on spot 0
			componentList := t.WorldData[id]
			inputBox, ok := componentList[0].(ecs.CompBox[physics.Input]) // TODO - should id be replaced with 0?
			if !ok { continue }
			input := inputBox.Get()

			trustedUpdate := serdes.WorldUpdate{
				WorldData: map[ecs.Id][]ecs.Component{
					id: []ecs.Component{ecs.C(input)},
				},
			}
			log.Println("TrustedUpdate:", trustedUpdate)

			networkChannel <- trustedUpdate
		case serdes.ClientLogin:
			log.Println("Server: serdes.ClientLogin")
			// Login player
			// TODO - put into a function
			// TODO - not thread safe! Concurrent map access
			// TODO - Refactor networking layer to have an RPC functionality
			id := world.NewId()
			ecs.Write(world, id, ecs.C(User{
				Id: t.UserId,
			}),
				ecs.C(physics.Input{}),
				ecs.C(Body{}),
				ecs.C(SpawnPoint()),
			)
			// log.Println("Logging in player:", id)

			loginMap[t.UserId] = id
			loginResp := serdes.MarshalClientLoginRespMessage(t.UserId, id)
			err := sock.Send(loginResp)
			if err != nil {
				log.Println("Failed to send login response")
			}
		default:
			panic("Unknown message type")
		}
	}
}
