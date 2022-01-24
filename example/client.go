package main

import (
	"flag"
	"k8s.io/klog/v2"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	ws_informer "distributed-informer"
)

var addr = flag.String("addr", "localhost:8080", "http service address")

func main() {

	stopCh := make( <-chan struct{})

	wsInformer := ws_informer.NewWsInformer("test", *addr)

	wsInformer.AddResourceEventHandler("core/v1/Pod", cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {

			klog.Info("add")



			var newPod  v1.Pod

			err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.(map[string]interface{}), &newPod)
			if err != nil {
				klog.Error(err)
				return
			}

			klog.Info(newPod.GetNamespace()+"/"+newPod.GetName())

		},
		UpdateFunc: func(old, new interface{}) {
			klog.Info("update")
			var newPod v1.Pod
			var oldPod v1.Pod
			err := runtime.DefaultUnstructuredConverter.FromUnstructured(new.(map[string]interface{}), &newPod)
			if err != nil {
				klog.Error(err)
				return
			}
			err = runtime.DefaultUnstructuredConverter.FromUnstructured(old.(map[string]interface{}), &oldPod)
			if err != nil {
				klog.Error(err)
				return
			}
			if newPod.GetResourceVersion() == oldPod.GetResourceVersion() {
				// Periodic resync will send update events for all known Deployments.
				// Two different versions of the same Deployment will always have different RVs.
				return
			}
			klog.Info(newPod.GetNamespace()+"/"+newPod.GetName())
		},
		DeleteFunc: func(obj interface{}) {
			var newPod v1.Pod
			err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.(map[string]interface{}), &newPod)
			if err != nil {
				klog.Error(err)
				return
			}
			klog.Info(newPod.GetNamespace()+"/"+newPod.GetName())

		},
	})

	wsInformer.Start(stopCh)

	<-stopCh


}
