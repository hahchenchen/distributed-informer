package main

import (
	ws_informer "distributed-informer"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"log"
	"net/http"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"time"
	v12 "k8s.io/api/apps/v1"
)

func main() {
	stopCh := make( <-chan struct{})

	klog.Info("cfg GetConfig")
	kubeconfig, err := config.GetConfig()
	if err != nil {
		klog.Error(err, "unable to set up client config by default")
		return
	}

	klog.Info("cfg success")

	kubeClient, err := kubernetes.NewForConfig(kubeconfig)
	if err != nil {
		klog.Fatalf("Error building kubernetes clientset: %s", err.Error())
	}

	klog.Info("kubeClient success")

	/*exampleClient, err := clientset.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("Error building example clientset: %s", err.Error())
	}*/

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Second*30)
	//exampleInformerFactory := informers.NewSharedInformerFactory(exampleClient, time.Second*30)
	klog.Info("kubeInformerFactory new success")
	warpInformer := ws_informer.WrapInformerFactory(kubeInformerFactory, time.Second*30)

	//deploy informer start
	deployInformer := warpInformer.Apps().V1().Deployments().Informer()

	//pod informer start
	warpInformer.Core().V1().Pods().Informer()

	deployInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			newDepl := obj.(*v12.Deployment)
			klog.Info(newDepl.Namespace+"/"+newDepl.Name)

		},
		UpdateFunc: func(old, new interface{}) {
			newDepl := new.(*v12.Deployment)
			oldDepl := old.(*v12.Deployment)
			if newDepl.ResourceVersion == oldDepl.ResourceVersion {
				// Periodic resync will send update events for all known Deployments.
				// Two different versions of the same Deployment will always have different RVs.
				return
			}
			klog.Info(newDepl.Namespace+"/"+newDepl.Name)
		},
		DeleteFunc: func(obj interface{}) {
			newPod := obj.(*v12.Deployment)
			klog.Info(newPod.Namespace+"/"+newPod.Name)

		}})

	warpInformer.Start(stopCh)
	klog.Info("kubeInformerFactory start")
	warpInformer.WaitForCacheSync(stopCh)
	klog.Info("kubeInformerFactory success")

	hub := ws_informer.NewWsHub(warpInformer, nil)
	go hub.Run()

	klog.Info("hub success")

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		ws_informer.ServeWs(hub, w, r)
	})

	go func() {
		err = http.ListenAndServe("0.0.0.0:8080", nil)
		if err != nil {
			log.Fatal("ListenAndServe: ", err)
			os.Exit(1)
		}
	}()

	<-stopCh
}
