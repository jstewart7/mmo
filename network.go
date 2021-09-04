package mmo

import (
	"log"
	"net"

	"go.nanomsg.org/mangos/v3"

	"github.com/jstewart7/mmo/engine/ecs"
	"github.com/jstewart7/mmo/engine/physics"
	"github.com/jstewart7/mmo/serdes"
)

type Websocket struct {
	net.Conn
}
func (t *Websocket) ComponentSet(val interface{}) { *t = val.(Websocket) }

func ClientSendUpdate(engine *ecs.Engine, conn net.Conn) {
	ecs.Each(engine, ClientOwned{}, func(id ecs.Id, a interface{}) {
		input := physics.Input{}
		ok := ecs.Read(engine, id, &input)
		if !ok {
			log.Println("ERROR: Client Owned Entity should always have an input!")
			return
		}

		update := serdes.WorldUpdate{
			WorldData: map[ecs.Id][]interface{}{
				id: []interface{}{input},
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

func ClientReceive(engine *ecs.Engine, conn net.Conn, networkChannel chan serdes.WorldUpdate) {
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
			log.Println(t)
			networkChannel <- t
		case serdes.ClientLoginResp:
			log.Println("serdes.ClientLoginResp", t)
			ecs.Write(engine, ecs.Id(t.Id), ClientOwned{})
			ecs.Write(engine, ecs.Id(t.Id), Body{})
		default:
			panic("Unknown message type")
		}
	}
}

func ServerSendUpdate(engine *ecs.Engine, sock mangos.Socket) {
	// transformList := make([]TransformUpdate, 0)

	update := serdes.WorldUpdate{
		UserId: 0,
		WorldData: make(map[ecs.Id][]interface{}),
	}

	ecs.Each(engine, physics.Transform{}, func(id ecs.Id, a interface{}) {
		transform := a.(physics.Transform)
		body := Body{}
		ok := ecs.Read(engine, id, &body)
		if !ok { return }

		// transformUpdate, err := NewTransformUpdate(id, transform)
		// if err != nil {
		// 	log.Println(err)
		// 	return
		// }

		// transformList = append(transformList, transformUpdate)

		compList := []interface{}{
			transform,
			body,
		}
		update.WorldData[id] = compList
	})

	ecs.Each(engine, User{}, func(id ecs.Id, a interface{}) {
		user := a.(User)

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

func ServeProxyConnection(sock mangos.Socket, engine *ecs.Engine, networkChannel chan serdes.WorldUpdate) {
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
			input, ok := componentList[id].(physics.Input)
			if !ok { continue }

			trustedUpdate := serdes.WorldUpdate{
				WorldData: map[ecs.Id][]interface{}{
					id: []interface{}{input},
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
			id := engine.NewId()
			ecs.Write(engine, id, User{
				Id: t.UserId,
			})
			ecs.Write(engine, id, physics.Input{})
			ecs.Write(engine, id, Body{})
			ecs.Write(engine, id, SpawnPoint())
			log.Println("Logging in player:", id)

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
