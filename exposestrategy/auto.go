package exposestrategy

import (
	"strings"

	"github.com/golang/glog"
	"github.com/pkg/errors"
	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/unversioned"
	"k8s.io/kubernetes/pkg/client/restclient"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/runtime"
)

const (
	ingress            = "route"
	loadBalancer       = "loadbalancer"
	nodePort           = "nodeport"
	route              = "route"
	domainExt          = ".xip.io"
	stackpointNS       = "stackpoint-system"
	stackpointHAProxy  = "spc-balancer"
	stackpointIPEnvVar = "BALANCER_IP"
)

func NewAutoStrategy(exposer, domain string, client *client.Client, restClientConfig *restclient.Config, encoder runtime.Encoder) (ExposeStrategy, error) {

	exposer, err := getAutoDefaultExposeRule(client)
	if err != nil {
		return nil, errors.Wrap(err, "failed to automatically get exposer rule.  consider setting 'exposer' type in config.yml")
	}
	glog.Infof("Using exposer strategy: %s", exposer)

	// only try to get domain if we need wildcard dns and one wasn't given to us
	if len(domain) == 0 && (strings.EqualFold(ingress, exposer) || strings.EqualFold(route, exposer)) {

		domain, err = getAutoDefaultDomain(client)
		if err != nil {
			return nil, errors.Wrap(err, "failed to get a domain")
		}
		glog.Infof("Using domain: %s", domain)
	}

	return New(exposer, domain, client, restClientConfig, encoder)
}

func getAutoDefaultExposeRule(c *client.Client) (string, error) {

	nodes, err := c.Nodes().List(api.ListOptions{})
	if err != nil {
		return "", errors.Wrap(err, "failed to find any nodes")
	}
	if len(nodes.Items) == 1 {
		node := nodes.Items[0]
		if node.Name == "minishift" || node.Name == "minikube" {
			return nodePort, nil
		}
	}

	t, err := typeOfMaster(c)
	if err != nil {
		return "", errors.Wrap(err, "failed to get type of master")
	}
	if t == openShift {
		return route, nil
	}
	return ingress, nil
}

func getAutoDefaultDomain(c *client.Client) (string, error) {
	nodes, err := c.Nodes().List(api.ListOptions{})
	if err != nil {
		return "", errors.Wrap(err, "failed to find any nodes")
	}

	// if we're mini* then there's only one node, any router / ingress controller deployed has to be on this one
	if len(nodes.Items) == 1 {
		node := nodes.Items[0]
		if node.Name == "minishift" || node.Name == "minikube" {
			ip, err := getExternalIP(node)
			if err != nil {
				return "", err
			}
			return ip + domainExt, nil
		}
	}

	// check for a gofabric8 ingress labelled node
	selector, err := unversioned.LabelSelectorAsSelector(&unversioned.LabelSelector{MatchLabels: map[string]string{"fabric8.io/externalIP": "fabric8"}})
	nodes, err = c.Nodes().List(api.ListOptions{LabelSelector: selector})
	if len(nodes.Items) == 1 {
		node := nodes.Items[0]
		ip, err := getExternalIP(node)
		if err != nil {
			return "", err
		}
		return ip + domainExt, nil
	}

	// look for a stackpoint HA proxy
	pod, _ := c.Pods(stackpointNS).Get(stackpointHAProxy)
	if pod != nil {
		containers := pod.Spec.Containers
		for _, container := range containers {
			if container.Name == stackpointHAProxy {
				for _, e := range container.Env {
					if e.Name == stackpointIPEnvVar {
						return e.Value + domainExt, nil
					}
				}
			}
		}
	}
	return "", errors.New("no known automatic ways to get an external ip to use with xip")
}

func getExternalIP(node api.Node) (string, error) {

	addresses := node.Status.Addresses
	for _, a := range addresses {
		if a.Type == api.NodeExternalIP || a.Type == api.NodeLegacyHostIP {
			return a.Address, nil
		}
	}
	return "", errors.New("no node ExternalIP or LegacyHostIP found")
}