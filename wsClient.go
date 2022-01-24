package ws_informer

import (
	"github.com/gorilla/websocket"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"log"
	"net/http"
	cache2 "sigs.k8s.io/controller-runtime/pkg/cache"
	"time"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 20 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = 10 * time.Second

	// Maximum message size allowed from peer.
	maxMessageSize = 1048576
)

var (
	newline = []byte{'\n'}
	space   = []byte{' '}
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type WsClient struct {
	hub *WsHub

	// The websocket connection.
	conn *websocket.Conn

	gvk  string

	// Buffered channel of outbound messages.
	send chan []byte
	listener interface{}
	informer cache.SharedIndexInformer
	controllerInformer cache2.Informer
}


func (c *WsClient) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	//c.conn.SetPongHandler(func(string) error { klog.Info("pong"); c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	//c.conn.SetPingHandler(func(string) error { klog.Info("ping"); c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })

	for {
		//actually, read message is only for client ping message. the ping handler do not set, use the default
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				klog.Error("error: %v", err)
			}
			break
		}
		/*message = bytes.TrimSpace(bytes.Replace(message, newline, space, -1))
		fmt.Println("message: ", string(message))
		//c.hub.broadcast <- message*/
	}
}

// writePump pumps messages from the hub to the websocket connection.
//
// A goroutine running writePump is started for each connection. The
// application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
func (c *WsClient) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			//klog.Info("message:", string(message))
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued chat messages to the current websocket message.
			n := len(c.send)
			klog.Info("len(c.send), ", len(c.send))
			for i := 0; i < n; i++ {
				w.Write(newline)
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}
		}
	}
}


// serveWs handles websocket requests from the peer.
func ServeWs(hub *WsHub, w http.ResponseWriter, r *http.Request) {

	klog.Info("serveWs")
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	gvk := r.Header.Get("gvk")
	client := &WsClient{hub: hub, conn: conn, send: make(chan []byte, 256), gvk: gvk}
	client.hub.register <- client

	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	go client.writePump()
	go client.readPump()
}