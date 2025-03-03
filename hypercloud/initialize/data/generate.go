package data

//go:generate wget -O cert-manager.yaml https://github.com/cert-manager/cert-manager/releases/download/v1.17.0/cert-manager.yaml
//go:generate wget -O tor-controller.yaml https://raw.githubusercontent.com/bugfest/tor-controller/master/hack/install.yaml
//go:generate wget -O external-dns-crd.yaml https://raw.githubusercontent.com/kubernetes-sigs/external-dns/refs/heads/master/charts/external-dns/crds/dnsendpoint.yaml
