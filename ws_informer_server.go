package ws_informer

import (
	"errors"
	"fmt"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"strings"
	"time"
)

type wsWrapInformerFactory struct {
	expandInformers map[schema.GroupVersionResource]cache.SharedIndexInformer
	kubeinformers.SharedInformerFactory
}

func WrapInformerFactory(factory kubeinformers.SharedInformerFactory, defaultResync time.Duration) *wsWrapInformerFactory {
	return &wsWrapInformerFactory{SharedInformerFactory: factory, expandInformers: make(map[schema.GroupVersionResource]cache.SharedIndexInformer)}
}

func NewExpandGenericInformer(informer cache.SharedIndexInformer, resource schema.GroupResource) kubeinformers.GenericInformer {
	return &expandGenericInformer{informer: informer, resource: resource}
}

type expandGenericInformer struct {
	informer cache.SharedIndexInformer
	resource schema.GroupResource
}

// Informer returns the SharedIndexInformer.
func (f *expandGenericInformer) Informer() cache.SharedIndexInformer {
	return f.informer
}

// Lister returns the GenericLister.
func (f *expandGenericInformer) Lister() cache.GenericLister {
	return cache.NewGenericLister(f.Informer().GetIndexer(), f.resource)
}

func (f *wsWrapInformerFactory) AddExpandInformer(resource schema.GroupVersionResource, informer cache.SharedIndexInformer) {
	f.expandInformers[resource] = informer
}

func (f *wsWrapInformerFactory) AddWsEventHandler(client *WsClient, gvk string, handler cache.ResourceEventHandler) {

	tmpList := strings.Split(gvk, "/")

	gvkList := make([]string, 3)
	if len(tmpList) < 3 {
		if len(tmpList) == 2 && tmpList[0] == "v1" {
			gvkList[2] = tmpList[1]
			gvkList[1] = tmpList[0]
			gvkList[0] = "core"
		}else {
			err :=  errors.New(fmt.Sprintf("gvkList is not match, %s", tmpList))
			klog.Error(err)
			return
		}
	}else {
		gvkList = tmpList
	}

	klog.Info("gvkList:", gvkList)
	if gvkList[0] == "Core" {
		gvkList[0] = ""
	}

	schemaGvk := schema.GroupVersionKind{
		Group:   gvkList[0],
		Version: gvkList[1],
		Kind:    gvkList[2],
	}
	gvr, _ := meta.UnsafeGuessKindToResource(schemaGvk)

	var usedInformer cache.SharedIndexInformer


	if r, err := f.SharedInformerFactory.ForResource(gvr); err != nil {
		if in, ok := f.expandInformers[gvr]; ok {
			usedInformer = in
		} else {
			klog.Error("no informer found for %v", gvr)
			return
		}
	}else {
		usedInformer = r.Informer()
	}

	listener := usedInformer.AddEventHandler(handler)

	client.listener = listener
	client.informer = usedInformer


}

func (f *wsWrapInformerFactory) AddWsEventHandlerWithResyncPeriod(gvk string, handler cache.ResourceEventHandler, defaultResync time.Duration) {

	tmpList := strings.Split(gvk, "/")

	gvkList := make([]string, 3)
	if len(tmpList) < 3 {
		if len(tmpList) == 2 && tmpList[0] == "v1" {
			gvkList[2] = tmpList[1]
			gvkList[1] = tmpList[0]
			gvkList[0] = "core"
		}else {
			err :=  errors.New(fmt.Sprintf("gvkList is not match, %s", tmpList))
			klog.Error(err)
			return
		}
	}else {
		gvkList = tmpList
	}

	schemaGvk := schema.GroupVersionKind{
		Group:   gvkList[0],
		Version: gvkList[1],
		Kind:    gvkList[2],
	}
	gvr, _ := meta.UnsafeGuessKindToResource(schemaGvk)

	var usedInformer cache.SharedIndexInformer

	if r, err := f.SharedInformerFactory.ForResource(gvr); err != nil {
		if in, ok := f.expandInformers[gvr]; ok {
			usedInformer = in
		} else {
			klog.Error("no informer found for %v", gvr)
			return
		}
	}else {
		usedInformer = r.Informer()
	}



	usedInformer.AddEventHandlerWithResyncPeriod(handler, defaultResync)



}