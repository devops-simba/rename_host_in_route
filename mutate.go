package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"strings"

	log "github.com/golang/glog"

	webhookCore "github.com/devops-simba/webhook_core"

	admissionApi "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	openshiftApi "github.com/openshift/api"
	operatorApi "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	operatorcs "github.com/openshift/client-go/operator/clientset/versioned/typed/operator/v1"
)

type RenameHostInRoute struct {
	// ownedHosts list of hosts that will be handled by this webhook
	// if a host name end with this suffix, then its name will be regenerated
	ownedHosts []string
	// defaultRouter Router that will be used for all routes that does not accepted by any router
	defaultRouter string
	// hostNameTemplate template that host names should created from it
	hostNameTemplate string
	// mutateSystemRoutes if true we will also try to mutate system routes, otherwise they will be ignored
	mutateSystemRoutes bool
}

func isRoute(kind metav1.GroupVersionKind) bool {
	return kind.Group == "route.openshift.io" && kind.Version == "v1" && kind.Kind == "Route"
}
func isSystemRoute(route *routev1.Route) bool {
	if webhookCore.IsObjectInNamespaces(&route.ObjectMeta, webhookCore.IgnoredNamespaces) {
		return true
	}

	n := strings.Index(route.Namespace, "/")
	if n == -1 {
		return false
	}

	return strings.HasSuffix(route.Namespace[:n], ".openshift.io")
}

func (this *RenameHostInRoute) IsOwnedHost(host string) bool {
	for i := 0; i < len(this.ownedHosts); i++ {
		if strings.HasSuffix(host, this.ownedHosts[i]) {
			return true
		}
	}
	return false
}
func (this *RenameHostInRoute) GetIngressControllers() ([]operatorApi.IngressController, error) {
	config := webhookCore.GetRESTConfig()
	cs, err := operatorcs.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	ingressControllers, err := cs.IngressControllers("openshift-ingress-operator").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	return ingressControllers.Items, nil
}
func (this *RenameHostInRoute) GetMatchingIngressController(
	ingressControllers []operatorApi.IngressController,
	route *routev1.Route) (*operatorApi.IngressController, error) {
	var routeNamespace *corev1.Namespace
	var defaultController *operatorApi.IngressController
	for _, controller := range ingressControllers {
		if defaultController == nil && controller.Name == this.defaultRouter {
			defaultController = &controller
		}

		match := true
		if controller.Spec.RouteSelector != nil {
			match = webhookCore.IsMatch(&route.ObjectMeta, controller.Spec.RouteSelector)
		}
		if match && controller.Spec.NamespaceSelector != nil {
			// this controller use namespace selector, so we need information of namespace of the route
			if routeNamespace == nil {
				var err error
				routeNamespace, err = webhookCore.GetNamespace(route.Namespace, metav1.GetOptions{})
				if err != nil {
					return nil, err
				}
			}

			match = webhookCore.IsMatch(&routeNamespace.ObjectMeta, controller.Spec.NamespaceSelector)
		}
		if match {
			return &controller, nil
		}
	}

	return defaultController, nil
}
func (this *RenameHostInRoute) GenerateNewHostname(
	controller *operatorApi.IngressController,
	route *routev1.Route) string {
	result := this.hostNameTemplate
	result = strings.Replace(result, "<name>", route.Name, -1)
	result = strings.Replace(result, "<ns>", route.Namespace, -1)
	result = strings.Replace(result, "<router-name>", controller.Name, -1)
	return strings.Replace(result, "<router-domain>", controller.Spec.Domain, -1)
}

func (this *RenameHostInRoute) Bind() interface{} {
	var ownedHosts string
	flag.StringVar(&this.defaultRouter, "defaultrouter", "internal-router",
		"if a route does not match any router, then this router will be added to the route definition")
	flag.StringVar(&ownedHosts, "ownedhosts", ".ic.cloud.snapp.ir",
		"a comma separated list of hosts, that if route's selected hostname end with them, it will be regenerated on change")
	flag.StringVar(&this.hostNameTemplate, "hostnametmpl", "<name>-<ns>.<router-domain>",
		"Template to generate host names. <name>, <ns>, <router-name>, <router-domain> will be replaced in this template")
	flag.BoolVar(&this.mutateSystemRoutes, "mutatesysroutes", false,
		"for performance reasons, routes that belong to the system will be ignored, by setting this you may enable checking those routes")

	webhookCore.InitializeRuntimeScheme("github.com/openshift/api", openshiftApi.Install)
	return ownedHosts
}
func (this *RenameHostInRoute) CompleteBinding(data interface{}) error {
	ownedHosts, ok := data.(string)
	if !ok {
		return errors.New("Invalid data")
	}

	this.ownedHosts = strings.Split(ownedHosts, ",")
	return nil
}

func (this *RenameHostInRoute) Name() string { return "rename_host_in_route" }
func (this *RenameHostInRoute) Desc() string {
	return "change host name in the route based on its router"
}
func (this *RenameHostInRoute) Path() string { return "rename-host-in-route" }
func (this *RenameHostInRoute) HandleAdmission(
	action string,
	request *http.Request,
	ar *admissionApi.AdmissionReview) (*admissionApi.AdmissionResponse, error) {
	if action != "mutate" || !isRoute(ar.Request.Kind) {
		// The only thing that I know is how to mutate the route.openshift.io/v1/Route
		log.Warningf("Request at our path with invalid request type or action")
		return nil, nil
	}

	// now we must deserialize this route
	var route routev1.Route
	if err := json.Unmarshal(ar.Request.Object.Raw, &route); err != nil {
		log.Errorf("Could not unmarshal route from %v: %v", string(ar.Request.Object.Raw), err)
		return webhookCore.CreateErrorResponse(err.Error()), nil
	}

	if !this.mutateSystemRoutes && isSystemRoute(&route) {
		log.Infof("Ignoring route mutation because it is in system NS: %v(%v)", route.Namespace, route.Name)
		return &admissionApi.AdmissionResponse{Allowed: true}, nil
	}

	// find list of controllers from the server
	var patches []webhookCore.PatchOperation
	controllers, err := this.GetIngressControllers()
	if err != nil {
		return nil, err
	}

	// find the controller
	routeController, err := this.GetMatchingIngressController(controllers, &route)
	if err != nil {
		return nil, err
	}
	if routeController == nil {
		return nil, fmt.Errorf("Can't find a router that match this route, did you ever specified a default route?")
	}

	// make sure that we have required labels on this route to be selected by our router
	if routeController.Spec.RouteSelector != nil {
		updatedLabels := webhookCore.UpdateLabels(route.Labels, routeController.Spec.RouteSelector.MatchLabels)
		if len(updatedLabels) != 0 {
			patches = append(patches, updatedLabels...)
		}
	}

	// Get list of ingress controllers to find the ingress controller that match this route
	if route.Spec.Host == "" {
		newHostName := this.GenerateNewHostname(routeController, &route)
		patches = append(patches, webhookCore.NewAddPatch("/spec/host", newHostName))
	} else if this.IsOwnedHost(route.Spec.Host) {
		newHostName := this.GenerateNewHostname(routeController, &route)
		if newHostName != route.Spec.Host {
			patches = append(patches, webhookCore.NewReplacePatch("/spec/host", newHostName))
		}
	}

	return webhookCore.CreatePatchResponse(patches)
}

func main() {
	err := CreateServerFromFlagsAndRunToTermination(&RenameHostInRoute{})
	if err != nil {
		log.Fatalf("FAILED: %v", err)
	}
}
