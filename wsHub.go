package ws_informer

import (
	"encoding/json"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"k8s.io/client-go/tools/cache"
	cache2 "sigs.k8s.io/controller-runtime/pkg/cache"
)

const (
	addEvent = "add"
	updateEvent = "update"
	deleteEvent = "delete"
)

type wsMsg struct {
	EventType string
	NewObj interface{}
	OldObj interface{}
}

type WsHub struct {
	// Registered clients.
	clients map[*WsClient]bool

	// Inbound messages from the clients.
	broadcast chan []byte

	// Register requests from the clients.
	register chan *WsClient

	// Unregister requests from clients.
	unregister chan *WsClient
	wsWrapInformer *wsWrapInformerFactory
	informerMap map[string]*cache2.Informer
}

func NewWsHub(wsWrapInformer *wsWrapInformerFactory, informerMap map[string]*cache2.Informer) *WsHub {
	wsHub := &WsHub{
		broadcast:  make(chan []byte),
		register:   make(chan *WsClient),
		unregister: make(chan *WsClient),
		clients:    make(map[*WsClient]bool),
		wsWrapInformer: wsWrapInformer,
		informerMap: make(map[string]*cache2.Informer),
	}

	wsHub.informerMap = informerMap

	return wsHub
}

func (h *WsHub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true

			resourceEventHandlerFuncs := cache.ResourceEventHandlerFuncs{
				AddFunc: func(obj interface{}) {

					// the defer is for client.send <- by, when send is close.
					defer func() {
						if recover() != nil {
							klog.Info("add revcover !!!")
						}
					}()

					unstructuredmap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
					if err != nil {
						klog.Error(err)
						return
					}

					unstructuredObj := unstructured.Unstructured{Object: unstructuredmap}

					//klog.Info("add unstructuredObj. ", unstructuredObj.GetName())

					msg := &wsMsg{
						EventType: addEvent,
						NewObj:    unstructuredObj.Object,
					}

					by, err := json.Marshal(msg)
					if err != nil {
						klog.Error(err)
						return
					}

					//klog.Info("add event: by:,", string(by))

					client.send <- by
				},
				UpdateFunc: func(old, new interface{}) {

					defer func() {
						if recover() != nil {
							klog.Info("update revcover !!!")
						}
					}()


					newunstructuredmap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(new)
					if err != nil {
						klog.Error(err)
						return
					}

					newUnstructuredObj := unstructured.Unstructured{Object: newunstructuredmap}

					oldunstructuredmap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(old)
					if err != nil {
						klog.Error(err)
						return
					}

					oldUnstructuredObj := unstructured.Unstructured{Object: oldunstructuredmap}


					if newUnstructuredObj.GetResourceVersion() == oldUnstructuredObj.GetResourceVersion() {
						// Periodic resync will send update events for all known Deployments.
						// Two different versions of the same Deployment will always have different RVs.
						return
					}

					msg := &wsMsg{
						EventType: updateEvent,
						NewObj:    newUnstructuredObj.Object,
						OldObj: oldUnstructuredObj.Object,
					}

					by, err := json.Marshal(msg)
					if err != nil {
						klog.Error(err)
						return
					}

					//klog.Info("update event: by:,", string(by))

					client.send <- by
				},
				DeleteFunc: func(obj interface{}) {
					unstructuredmap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
					if err != nil {
						klog.Error(err)
						return
					}

					newUnstructuredObj := unstructured.Unstructured{Object: unstructuredmap}

					msg := &wsMsg{
						EventType: deleteEvent,
						NewObj:    newUnstructuredObj.Object,
					}

					by, err := json.Marshal(msg)
					if err != nil {
						klog.Error(err)
						return
					}

					klog.Info("delete event: by:,", string(by))

					client.send <- by
				},
			}

			if h.wsWrapInformer != nil {
				h.wsWrapInformer.AddWsEventHandler(client, client.gvk, resourceEventHandlerFuncs)
			}else if h.informerMap != nil {
				usedInformer := h.informerMap[client.gvk]

				listener := (*usedInformer).AddEventHandler(resourceEventHandlerFuncs)

				client.listener = listener
				client.controllerInformer = *usedInformer
			}


		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				if h.wsWrapInformer != nil {
					client.informer.DeleteListener(client.listener)
				}else if h.informerMap != nil {
					client.controllerInformer.DeleteListener(client.listener)
				}

				klog.Info("close it!!!!!!!!")
				delete(h.clients, client)
				close(client.send)
			}
		/*case message := <-h.broadcast:
			klog.Info("message, ", string(message))
			for client := range h.clients {
				klog.Info("client, ")
				select {
				case client.send <- message:
				default:
					klog.Info("close(client.send)11")
					close(client.send)
					delete(h.clients, client)
				}
			}*/
		}
	}
}

// Object2Unstructured converts an object to an unstructured struct
func Object2Unstructured(obj interface{}) (*unstructured.Unstructured, error) {
	objMap, err := Object2Map(obj)
	if err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{
		Object: objMap,
	}, nil
}

// Object2Map turn the Object to a map
func Object2Map(obj interface{}) (map[string]interface{}, error) {
	var res map[string]interface{}
	bts, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(bts, &res)
	return res, err
}
