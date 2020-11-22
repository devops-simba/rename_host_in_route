module github.com/devops-simba/rename_host_in_route

go 1.14

require (
	github.com/devops-simba/helpers v1.0.7
	github.com/devops-simba/webhook_core v1.0.17
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b
	// Add openshift v4.5 as requirement in this package
	github.com/openshift/api v0.0.0-20200917102736-0a191b5b9bb0
	github.com/openshift/client-go v0.0.0-20200521150516-05eb9880269c
	k8s.io/api v0.18.3
	k8s.io/apimachinery v0.18.3
)
