package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"text/template"

	log "github.com/golang/glog"

	"github.com/devops-simba/helpers"
	webhookCore "github.com/devops-simba/webhook_core"

	admissionApi "k8s.io/api/admission/v1"
	admissionRegistration "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	openshiftApi "github.com/openshift/api"
	operatorApi "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	operatorcs "github.com/openshift/client-go/operator/clientset/versioned/typed/operator/v1"
)

type RenameHostInRouteMutatingWebhook struct {
	// ownedHosts list of hosts that will be handled by this webhook
	// if a host name end with this suffix, then its name will be regenerated
	ownedHosts []string
	// defaultRouter Router that will be used for all routes that does not accepted by any router
	defaultRouter string
	// hostNameTemplate template that host names should created from it
	hostNameTemplate *template.Template
	// mutateSystemRoutes if true we will also try to mutate system routes, otherwise they will be ignored
	mutateSystemRoutes bool
}

type RouterData struct {
	Name   string
	Domain string
}
type HostnameRenderData struct {
	Name      string
	Namespace string
	Router    RouterData
}

var trueStrings = []string{"true", "yes", "1", "ok"}

func toBool(value string) bool {
	return helpers.ContainsString(trueStrings, strings.ToLower(value))
}

func NewRenameHostInRouteMutatingWebhook() *RenameHostInRouteMutatingWebhook {
	return &RenameHostInRouteMutatingWebhook{
		ownedHosts:    strings.Split(helpers.ReadEnv("OWNED_HOSTS", ".ic.cloud.snapp.ir"), ","),
		defaultRouter: helpers.ReadEnv("DEFAULT_ROUTER", "internal-router"),
		hostNameTemplate: webhookCore.MustParseTemplate("hostName",
			helpers.ReadEnv("HOST_NAME_TMPL", "{{ .Name }}-{{ .Namespace }}.{{ .Router.Domain }}")),
		mutateSystemRoutes: toBool(helpers.ReadEnv("MUTATE_SYSTEM_ROUTES", "0")),
	}
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

func (this *RenameHostInRouteMutatingWebhook) IsOwnedHost(host string) bool {
	for i := 0; i < len(this.ownedHosts); i++ {
		if strings.HasSuffix(host, this.ownedHosts[i]) {
			return true
		}
	}
	return false
}
func (this *RenameHostInRouteMutatingWebhook) GetIngressControllers() ([]operatorApi.IngressController, error) {
	config := webhookCore.GetRESTConfig()
	cs, err := operatorcs.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	ingressControllers, err := cs.IngressControllers("openshift-ingress-operator").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	return ingressControllers.Items, nil
}
func (this *RenameHostInRouteMutatingWebhook) GetMatchingIngressController(
	ingressControllers []operatorApi.IngressController,
	route *routev1.Route) (*operatorApi.IngressController, error) {
	var routeNamespace *corev1.Namespace
	var defaultController *operatorApi.IngressController
	matchedControllers := []*operatorApi.IngressController{}
	for _, controller := range ingressControllers {
		if defaultController == nil && controller.Name == this.defaultRouter {
			defaultController = &controller
		}

		match := true
		if controller.Spec.RouteSelector != nil {
			match = webhookCore.IsMatch(&route.ObjectMeta, controller.Spec.RouteSelector)
			if log.V(10) {
				labels, _ := json.Marshal(route.ObjectMeta.Labels)
				routeSelector, _ := json.Marshal(controller.Spec.RouteSelector)
				log.Infof("Route(%s: %v) match Controller(%s, %v) => %v",
					route.Name, string(labels),
					controller.Name, string(routeSelector),
					match,
				)
			}
		}
		if match && controller.Spec.NamespaceSelector != nil {
			// this controller use namespace selector, so we need information of namespace of the route
			var err error
			if routeNamespace == nil {
				routeNamespace, err = webhookCore.GetNamespace(route.Namespace, metav1.GetOptions{})
				if err != nil {
					log.Warningf("Failed to read namespace(%s) information: %v", route.Namespace, err)
				}
			}

			if err == nil {
				match = webhookCore.IsMatch(&routeNamespace.ObjectMeta, controller.Spec.NamespaceSelector)
				if log.V(10) {
					labels, _ := json.Marshal(routeNamespace.ObjectMeta.Labels)
					nsSelector, _ := json.Marshal(controller.Spec.NamespaceSelector)
					log.Infof("Namespace(%s: %v) match Controller(%s, %v) => %v",
						routeNamespace.Name, string(labels),
						controller.Name, string(nsSelector),
						match,
					)
				}
			}
		}
		if match {
			matchedControllers = append(matchedControllers, &controller)
		}
	}

	if log.V(10) {
		matchedControllerNames := make([]string, len(matchedControllers))
		for i := 0; i < len(matchedControllers); i++ {
			matchedControllerNames[i] = matchedControllers[i].Name
		}
		log.Infof("Matched controllers: %v", matchedControllerNames)
	}
	var result *operatorApi.IngressController
	if len(matchedControllers) == 1 {
		result = matchedControllers[0]
		log.V(10).Infof("Selected %s as matched controller(only match)", result.Name)
	} else if len(matchedControllers) == 0 {
		result = defaultController
		log.V(10).Infof("Selected default-controller(%s) as matched controller(no match found)", result.Name)
	} else {
		if matchedControllers[0] == defaultController {
			result = matchedControllers[1]
			log.V(10).Infof("Selected next-controller(%s) as matched controller(more than one match)", result.Name)
		} else {
			result = matchedControllers[0]
			log.V(10).Infof("Selected first-controller(%s) as matched controller(more than one match)", result.Name)
		}
	}

	return result, nil
}
func (this *RenameHostInRouteMutatingWebhook) GenerateNewHostname(
	controller *operatorApi.IngressController,
	route *routev1.Route) string {
	builder := &strings.Builder{}
	this.hostNameTemplate.Execute(builder, HostnameRenderData{
		Name:      route.Name,
		Namespace: route.Namespace,
		Router: RouterData{
			Name:   controller.Name,
			Domain: controller.Spec.Domain,
		},
	})
	return builder.String()
}

func createConfig(name, defaultValue, desc string) webhookCore.WebhookConfiguration {
	return webhookCore.WebhookConfiguration{
		Name:         name,
		DefaultValue: &defaultValue,
		Desc:         desc,
	}
}

func (this *RenameHostInRouteMutatingWebhook) Name() string { return "rename-host-in-route" }
func (this *RenameHostInRouteMutatingWebhook) Type() webhookCore.AdmissionWebhookType {
	return webhookCore.MutatingAdmissionWebhook
}
func (this *RenameHostInRouteMutatingWebhook) Configurations() []webhookCore.WebhookConfiguration {
	return []webhookCore.WebhookConfiguration{
		createConfig("DEFAULT_ROUTER", "internal-router",
			"if a route does not match any router, then this router will be added to the route definition"),
		createConfig("OWNED_HOSTS", ".ic.cloud.snapp.ir",
			"a comma separated list of hosts, that if route's selected hostname end with them, it will be regenerated on change"),
		createConfig("HOST_NAME_TMPL", "{{ .Name }}-{{ .Namespace }}.{{ .Router.Domain }}",
			"Template to generate host names"),
		createConfig("MUTATE_SYSTEM_ROUTES", "0",
			"for performance reasons, routes that belong to the system will be ignored. by setting, this you may enable checking those routes"),
	}
}
func (this *RenameHostInRouteMutatingWebhook) Rules() []admissionRegistration.RuleWithOperations {
	return []admissionRegistration.RuleWithOperations{
		admissionRegistration.RuleWithOperations{
			Rule: admissionRegistration.Rule{
				APIGroups:   []string{"route.openshift.io"},
				Resources:   []string{"*"},
				APIVersions: []string{"v1"},
				Scope:       nil, // any scope
			},
			Operations: []admissionRegistration.OperationType{
				admissionRegistration.Create,
				admissionRegistration.Update,
			},
		},
	}
}
func (this *RenameHostInRouteMutatingWebhook) TimeoutInSeconds() int {
	return webhookCore.DefaultTimeoutInSeconds
}
func (this *RenameHostInRouteMutatingWebhook) SupportedAdmissionVersions() []string {
	return webhookCore.SupportedAdmissionVersions
}
func (this *RenameHostInRouteMutatingWebhook) SideEffects() admissionRegistration.SideEffectClass {
	return admissionRegistration.SideEffectClassNone
}
func (this *RenameHostInRouteMutatingWebhook) Initialize() {
	webhookCore.InitializeRuntimeScheme("github.com/openshift/api", openshiftApi.Install)
}
func (this *RenameHostInRouteMutatingWebhook) HandleAdmission(
	request *http.Request,
	ar *admissionApi.AdmissionReview) (*admissionApi.AdmissionResponse, error) {
	if !isRoute(ar.Request.Kind) {
		// The only thing that I know is how to mutate the route.openshift.io/v1/Route
		log.Warningf("Request at our path with invalid request type")
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
