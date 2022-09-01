package main

import (
	"log"
	"os"
	"os/signal"
	"net/http"
	"sync"
	"net"
	"time"
	"context"

	"nhooyr.io/websocket"

	"go.nanomsg.org/mangos/v3"
	"go.nanomsg.org/mangos/v3/protocol/pair"
	_ "go.nanomsg.org/mangos/v3/transport/tcp"

	"github.com/unitoftime/mmo/serdes"
	"github.com/unitoftime/ecs"
)

func main() {
	url := "tcp://127.0.0.1:9000"

	sock, err := pair.NewSocket()
	if err != nil {
		panic(err)
	}

	err = sock.Dial(url)
	if err != nil {
		panic(err)
	}

	listener, err := net.Listen("tcp", ":8000")
	if err != nil {
		panic(err)
	}

	room := NewRoom()

	go room.HandleGameUpdates(sock)

	s := &http.Server{
		Handler: websocketServer{
			serverSocket: sock,
			room: room,
		},
		ReadTimeout: 10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Println("Starting Proxy", listener.Addr())

	errc := make(chan error, 1)
	go func() {
		errc <- s.Serve(listener)
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt)

	select{
	case err := <-errc:
		log.Println("Failed to serve:", err)
	case sig := <-sigs:
		log.Println("Terminating:", sig)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10 * time.Second)
	defer cancel()

	err = s.Shutdown(ctx)
	if err != nil {
		log.Println("Error shutting down server:", err)
	}
}

type websocketServer struct {
	serverSocket mangos.Socket
	room *Room
}

func (s websocketServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"localhost:8081"}, // TODO - Refactor this once I have a good deployment format
	})
	if err != nil {
		log.Println("Error Accepting Websocket:", err)
		return
	}

	ctx := context.Background()

	conn := websocket.NetConn(ctx, c, websocket.MessageBinary)

	go ServeNetConn(conn, s.serverSocket, s.room)
}

var userIdCounter uint64
func ServeNetConn(conn net.Conn, serverSocket mangos.Socket, room *Room) {
	defer func() {
		err := conn.Close()
		if err != nil {
			log.Println("Error Closing net.Conn:", err)
		}
	}()

	timeoutSeconds := 60 * time.Second
	timeout := make(chan uint8, 1)
	const StopTimeout uint8 = 0
	const ContTimeout uint8 = 1
	const MaxMsgSize int = 4 * 1024 // TODO - use the maximum MTU size

	// Login player
	room.mu.Lock()
	// TODO - Eventually This id should come from the login request which probably has a JWT which encodes the data. You probably don't need that in a lock
	userId := userIdCounter
	userIdCounter++
	_, ok := room.Map[userId]
	if ok {
		log.Println("Duplicate Login Detected! Exiting.")
		room.mu.Unlock()
		return
	}
	room.Map[userId] = conn
	room.mu.Unlock()

	// Cleanup room once they leave
	defer func() {
		room.mu.Lock()
		delete(room.Map, userId)
		room.mu.Unlock()
	}()

	// Send login message to server
	log.Println("Sending Login Message for", userId)
	serLogin := serdes.MarshalClientLoginMessage(userId)

	err := serverSocket.Send(serLogin)
	if err != nil {
		log.Println("Failed to send login message")
		return
	}

	// Send logout message to server
	defer func() {
		// TODO - maybe just kick of a goroutine that just continually tries to do this until it succeeds. Or maybe have one worker that reads from a queue or map or something like that
		serLogout := serdes.MarshalClientLogoutMessage(userId)

		err := serverSocket.Send(serLogout)
		if err != nil {
			panic("Failed to send logout message") // TODO - this needs to not panic
		}
	}()

	// Read data from client and sends to game server
	// TODO - TCP doesn't provide framing, so message framing needs to be added
	// TODO - TCP will send 0 byte messages to indicate closes, websockets sends them without closing
	go func() {
		msg := make([]byte, MaxMsgSize)
		for {
			n, err := conn.Read(msg)

			if err != nil {
				log.Println("Read Error:", err)
				timeout <- StopTimeout // Stop timeout because of a read error
				return
			} else if n <= 0 {
				continue
			}

			// Tick the timeout watcher so we don't timeout!
			timeout <- ContTimeout

			// log.Println("Unmarshalling")
			// TODO - replace with mutateInPlace code?
			fbMessage, err := serdes.UnmarshalMessage(msg)
			if err != nil {
				log.Println("Failed to unmarshal:", err)
			}
			log.Println("ServeNetConn:", fbMessage)

			switch t := fbMessage.(type) {
			case serdes.WorldUpdate:
				log.Println("Client->Proxy: World Update received")
				// TODO - replace with mutateInPlace code?
				t.UserId = userId
				serializedUpdate, err := serdes.MarshalWorldUpdateMessage(t)
				if err != nil {
					log.Println("Error Marshalling", err)
					continue
				}

				err = serverSocket.Send(serializedUpdate)
				if err != nil {
					log.Println("Error Sending:", err)
				}
			default:
				panic("Unknown message type")
			}
		}
	}()

	// Manage Timeout
ExitTimeout:
	for {
		select {
		case res := <-timeout:
			if res == StopTimeout {
				log.Println("Manually Stopping Timeout Manager")
				break ExitTimeout
			}
		case <-time.After(timeoutSeconds):
			log.Println("User timed out!")
			break ExitTimeout
		}
	}
}

// TODO - rename
type Room struct {
	mu sync.RWMutex
	Map map[uint64]net.Conn
}

func NewRoom() *Room {
	return &Room{
		Map: make(map[uint64]net.Conn),
	}
}

func (r *Room) HandleGameUpdates(sock mangos.Socket) {
	// Read data from game server and send to client
	for {
		msg, err := sock.Recv()
		if err != nil {
			log.Println("Read Error:", err)
		}

		fbMessage, err := serdes.UnmarshalMessage(msg)
		if err != nil {
			log.Println("Failed to unmarshal:", err)
		}
		// log.Println("HandleGameUpdate:", fbMessage)

		switch t := fbMessage.(type) {
		case serdes.WorldUpdate:
			r.mu.RLock()
			conn, ok := r.Map[t.UserId]
			r.mu.RUnlock()
			if ok {
				// TODO - replace with mutateInPlace code?
				t.UserId = 0
				serializedUpdate, err := serdes.MarshalWorldUpdateMessage(t)
				if err != nil {
					log.Println("Error Marshalling", err)
					continue
				}
				log.Println("Proxy WorldUpdate:", t)

				_, err = conn.Write(serializedUpdate)
				if err != nil {
					log.Println("Error Sending:", err)
					// TODO - User disconnected? Remove from map?
				}
			} else {
				log.Println("User Disconnected", t.UserId)
				// TODO - Send back to server "hey this person disconnected!"
			}
		case serdes.ClientLoginResp:
			log.Println("serdes.ClientLoginResp")
			r.mu.RLock()
			conn, ok := r.Map[t.UserId]
			r.mu.RUnlock()
			if ok {
				// TODO - replace with mutateInPlace code?
				t.UserId = 0
				serializedMsg := serdes.MarshalClientLoginRespMessage(t.UserId, ecs.Id(t.Id))
				if err != nil {
					log.Println("Error Marshalling", err)
					continue
				}
				log.Println("Proxy LoginResp:", t)

				_, err = conn.Write(serializedMsg)
				if err != nil {
					log.Println("Error Sending:", err)
					// TODO - User disconnected? Remove from map?
				}
			} else {
				log.Println("User Disconnected", t.UserId)
				// TODO - Send back to server "hey this person disconnected!"
			}
		case serdes.ClientLogoutResp:
			log.Println("serdes.ClientLogoutResp")
			// TODO - should I double check that they've been removed from the map?
			// TODO - I should send a "logged out successful" message
		default:
			panic("Unknown message type")
		}
	}
}
