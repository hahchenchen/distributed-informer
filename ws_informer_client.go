package ws_informer


import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"k8s.io/utils/buffer"
	"net/url"
	"strings"
	"time"
	)

const (
	defaultPort = "8080"

	bufferSize = 1024

)
type WsInformer struct {
	cluster string
	addr string
	WsConnMap map[string]*WsConn
	wg               wait.Group
}

type WsConn struct {
	informer             *WsInformer
	conn                 *websocket.Conn
	gvk                  string
	isConnected          bool
	url                  string
	handler              ResourceEventHandler
	nextCh               chan interface{}
	addCh                chan interface{}
	pendingNotifications buffer.RingGrowing
	stopCh               <-chan struct{}
}

type updateNotification struct {
	oldObj interface{}
	newObj interface{}
}

type addNotification struct {
	newObj interface{}
}

type deleteNotification struct {
	oldObj interface{}
}


func NewWsInformer(cluster, addr string) *WsInformer {

	addrs := strings.Split(addr, ":")
	if len(addrs) == 1 {
		addr += ":" + defaultPort
	}

	return &WsInformer{
		cluster: cluster,
		addr:    addr,
		WsConnMap: make(map[string]*WsConn),
	}
}

func (w *WsInformer) AddResourceEventHandler(gvk string, handler ResourceEventHandler) error {

	u := url.URL{Scheme: "ws", Host: w.addr, Path: "/ws"}

	wsConn := &WsConn{
		gvk: gvk,
		isConnected: false,
		url: u.String(),
		informer: w,
		handler: handler,
		nextCh:                make(chan interface{}),
		addCh:                 make(chan interface{}),
		stopCh:  make(<-chan struct{}),
		pendingNotifications:  *buffer.NewRingGrowing(bufferSize),
	}

	w.WsConnMap[gvk] = wsConn

	return nil

}


func (wsconn *WsConn) Run() {

	klog.Infof("ready to connect to %s", wsconn.url)

	header := make(map[string][]string)
	header["cluster"] = []string{wsconn.informer.cluster}
	header["gvk"] = []string{wsconn.gvk}

	klog.Info("connecting ...")

	for {
		newc, _, err := websocket.DefaultDialer.Dial(wsconn.url, header)
		if err != nil {
			klog.Error("err dial:", err)
			time.Sleep(10 * time.Second)
			klog.Info("reconnecting ...")
		}else {
			newc.SetReadLimit(maxMessageSize)
			newc.SetPongHandler(func(string) error { fmt.Println("pong"); newc.SetReadDeadline(time.Now().Add(pongWait)); return nil })
			wsconn.isConnected = true
			wsconn.conn = newc
			klog.Info("connect success")
			break
		}
	}


	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		wsconn.conn.Close()
		wsconn.isConnected = false
	}()

	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			wsconn.conn.SetReadDeadline(time.Now().Add(pongWait))
			_, message, err := wsconn.conn.ReadMessage()
			if err != nil {
				wsconn.isConnected = false
				klog.Error("ReadMessage err:", err)
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					klog.Error("IsUnexpectedCloseError error: %v", err)
					return
				}else {
					//1006 abnormal closure or 1001
					for {
						klog.Info("reconnecting ...")
						newc, _, err := websocket.DefaultDialer.Dial(wsconn.url, header)
						if err != nil {
							klog.Error("err dial:", err)
							time.Sleep(10 * time.Second)
						}else {
							newc.SetReadLimit(maxMessageSize)
							newc.SetPongHandler(func(string) error { fmt.Println("pong"); newc.SetReadDeadline(time.Now().Add(pongWait)); return nil })
							wsconn.isConnected = true
							wsconn.conn = newc
							klog.Info("connect success")
							break
						}
					}
				}
			}else {
				//klog.Infof("recv: %s", message)
				var msg wsMsg
				err := json.Unmarshal(message, &msg)
				if err != nil {
					klog.Error("Unmarshal message error, ignore this message" , err)
				}else {
					switch msg.EventType {
					case addEvent:
						//klog.Infof("recv: addEvent")
						wsconn.addCh <- addNotification{newObj: msg.NewObj}
					case updateEvent:
						klog.Infof("recv: updateEvent")
						wsconn.addCh <- updateNotification{newObj: msg.NewObj, oldObj: msg.OldObj}
					case deleteEvent:
						klog.Infof("recv: deleteEvent")
						wsconn.addCh <- deleteNotification{oldObj: msg.NewObj}
					}

				}
			}
		}
	}()



	for {
		select {
		case <-done:
			return
		case <-wsconn.stopCh:
			klog.Info("interrupt")
			// Cleanly close the connection by sending a close message and then
			// waiting (with timeout) for the server to close the connection.
			err := wsconn.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				klog.Error("write close:", err)
				return
			}
			select {
			case <-done:
			case <-time.After(time.Second):
			}
			return
		case <-ticker.C:
			if wsconn.isConnected == true {
				klog.Info("send ping message")
				wsconn.conn.SetWriteDeadline(time.Now().Add(writeWait))
				if err := wsconn.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					klog.Error("send ping Message, err,", err)
					return
				}
			}
		}
	}
}


func (wsconn *WsConn) pop() {
	defer utilruntime.HandleCrash()
	defer close(wsconn.nextCh) // Tell .run() to stop

	var nextCh chan<- interface{}
	var notification interface{}
	for {
		select {
		case nextCh <- notification:
			// Notification dispatched
			var ok bool
			notification, ok = wsconn.pendingNotifications.ReadOne()
			if !ok { // Nothing to pop
				nextCh = nil // Disable this select case
			}
		case notificationToAdd, ok := <-wsconn.addCh:
			//klog.Info("notificationToAdd")
			if !ok {
				return
			}
			if notification == nil { // No notification to pop (and pendingNotifications is empty)
				// Optimize the case - skip adding to pendingNotifications
				klog.Info("notification == nil")
				notification = notificationToAdd
				nextCh = wsconn.nextCh
			} else { // There is already a notification waiting to be dispatched
			//	klog.Info("pendingNotifications.WriteOne")
				wsconn.pendingNotifications.WriteOne(notificationToAdd)
			}
		}
	}
}


func (wsconn *WsConn) distribute() {
	// this call blocks until the channel is closed.  When a panic happens during the notification
	// we will catch it, **the offending item will be skipped!**, and after a short delay (one second)
	// the next notification will be attempted.  This is usually better than the alternative of never
	// delivering again.
	stopCh := make(chan struct{})
	wait.Until(func() {
		for next := range wsconn.nextCh {
			klog.Info("range wsconn.nextCh")
			switch notification := next.(type) {
			case updateNotification:
				wsconn.handler.OnUpdate(notification.oldObj, notification.newObj)
			case addNotification:
				klog.Info("distribute addNotification")
				wsconn.handler.OnAdd(notification.newObj)
			case deleteNotification:
				wsconn.handler.OnDelete(notification.oldObj)
			default:
				utilruntime.HandleError(fmt.Errorf("unrecognized notification: %T", next))
			}
		}
		// the only way to get here is if the p.nextCh is empty and closed
		close(stopCh)
	}, 1*time.Second, stopCh)
}


func (w *WsInformer) Start(stopCh <-chan struct{}) {
	for _, wsConn := range w.WsConnMap {
		wsConn.stopCh = stopCh
		if wsConn.isConnected == false {
			w.wg.Start(wsConn.Run)
			w.wg.Start(wsConn.pop)
			w.wg.Start(wsConn.distribute)
		}
	}
	<-stopCh
	for _, conn := range w.WsConnMap {
		close(conn.addCh) // Tell .pop() to stop. .pop() will tell .run() to stop
	}
	w.wg.Wait()

}

type ResourceEventHandler interface {
	OnAdd(obj interface{})
	OnUpdate(oldObj, newObj interface{})
	OnDelete(obj interface{})
}

type ResourceEventHandlerFuncs struct {
	AddFunc    func(obj interface{})
	UpdateFunc func(oldObj, newObj interface{})
	DeleteFunc func(obj interface{})
}


