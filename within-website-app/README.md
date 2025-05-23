# App

When you deploy an application to Kubernetes, you typically have to manage at least three basic resources:

- A Deployment that manages running your application code and rolling out updates without downtime
- A Service to give your application its own stable IP address and DNS name in your cluster
- An Ingress to expose your application to the public Internet

However, most of the time when you deploy an application you need a fair bit more such as:

- External secrets from [1Password](https://developer.1password.com/docs/k8s/operator/)
- Exposing your app as a [Tor hidden service](https://github.com/bugfest/tor-controller)
- Persistent storage via [PersistentVolumeClaim](https://kubernetes.io/docs/concepts/storage/persistent-volumes/)

App takes a more opinionated approach. When you deploy a service using App, you define a single manifest like this:

```yaml
apiVersion: x.within.website/v1
kind: App
metadata:
  name: stickers

spec:
  image: ghcr.io/xe/x/stickers:latest
  autoUpdate: true

  healthcheck:
    enabled: true

  ingress:
    enabled: true
    host: stickers.within.website

  onion:
    enabled: true

  secrets:
    - name: tigris-creds
      itemPath: "vaults/lc5zo4zjz3if3mkeuhufjmgmui/items/kvc2jqoyriem75ny4mvm6keguy"
      environment: true
```

This does the following:

- Creates a Deployment with one replica
- Configures [Keel](https://keel.sh) to update the app's code once per hour
- Configures the application to listen on port 3000
- Creates a Service that points to the application port 3000
- Creates an Ingress with HTTPS certificates for `stickers.within.website`
- Creates DNS entries for `stickers.within.website` to point to the HTTP Ingress for the cluster
- Creates an OnionService for the backend so that people can access it over Tor hidden services
- Creates bindings between 1Password and Kubernetes so that secrets in 1Password are visible as environment variables in the Deployment
- Configures the Deployment to do HTTP liveness and health checks on port 3000 every 3 seconds, restarting the app if it does not respond
- Configures the Deployment to run as a non-root user
- Configures the HTTP Ingress to redirect plain HTTP requests to HTTPS
- Annotates outgoing HTTP responses with the [Onion-Location](https://community.torproject.org/onion-services/advanced/onion-location/) header so users using the Tor browser can upgrade to browsing the service over Tor

The beautiful part about this is that all of this is handled for you. You don't think about the underlying resources or settings. You just specify what you want, and it makes it happen for you.

## Settings

App has a few top-level settings:

| Setting | Example | Description |
| :------ | :------ | :---------- |
| `autoUpdate` | `true` | If true, automatically update the App with [Keel](https://keel.sh). |
| `image` | `ghcr.io/xe/x/stickers` | (REQUIRED) The Docker/OCI image for the App. |
| `imagePullSecrets` | `- git-xeserv-us` | The names of any ImagePullSecrets needed to pull the Docker/OCI image. |
| `logLevel` | `DEBUG` | The [log/slog](https://pkg.go.dev/log/slog) level for the App, which lets you customize how verbose the logging is. |
| `replicas` | `3` | The number of service replicas that should be deployed for the App. By default, an App only has one replica, but for high availability you will want at least two. |
| `port` | `3000` | The port the App is listening on for HTTP/HTTPS traffic. If not set, App will choose port 3000 by fair dice roll. |
| `runAsRoot` | `false` | If true, then the pod will be configured to run your containers as root. Don't do this unless you have no other option. |

### Environment Variables

You can specify additional environment variables in the `env:` setting:

```yaml
env:
  - name: SOME_VARIABLE
    value: some value or something
```

Do not put secret values here. Use 1Password.

You can use anything that Kubernetes uses for environment variables in Deployments.

### Healthchecks

If you enable health checking, App will dispatch health checks every 3 seconds via HTTP to `/` on the main HTTP port:

```yaml
healthcheck:
  enabled: true
  port: 3000
  path: /
```

If the app fails health checks for long enough, Kubernetes will restart it. Consider having multiple replicas if this matters to you.

Most of the time the defaults should work well enough:

```yaml
healthcheck:
  enabled: true
```

The following settings are available:

| Setting | Example | Description |
| :------ | :---- | :---------- |
| `enabled` | `true` | If true, configure health checking for this App. |
| `port` | `3000` | If set, use an arbitrary port number to do health checks for this App. |
| `path` | `/.within/healthz` | If set, use an arbitrary path to do health checks for this App. |

### HTTP Ingress

By default apps are not exposed to the public Internet. If you want your app to have an internet presence, enable the Ingress feature:

```yaml
ingress:
  enabled: true
  host: stickers.within.website
```

This creates the following:

- An Ingress resource pointing `stickers.within.website` to your app's HTTP port via a Service
- DNS entries for `stickers.within.website` pointing to the `ingress-nginx` load balancer
- HTTP certificates for `stickers.within.website` and automatic rotation
- Instructions for the HTTP ingress to forward all plain HTTP traffic to HTTPS

The following settings are available:

| Setting | Example | Description |
| :------ | :---- | :---------- |
| `enabled` | `true` | If true, create a HTTP ingress for this App. |
| `host` | `stickers.within.website` | (REQUIRED) the HTTP hostname for the Ingress. This will be the domain users use to access the service. |
| `clusterIssuer` | `letsencrypt-staging` | If set, the certificate issuer used for this Ingress. If this is not set, then it will default to `letsencrypt-prod`. |
| `className` | `hythlodaeus` | If set, the HTTP ingress class that the Ingress should use. If this is not set, then it will default to `nginx`. |
| `annotations` | Kubernetes annotations | If set, any additional annotations that should be added to the Ingress. |

### Tor Hidden Services

If enabled, create a Tor hidden service for this App.

| Setting | Example | Description |
| :------ | :------ | :---------- |
| `enabled` | `true` | If true, create an OnionService pointing to the backend for this App. |
| `nonAnonymous` | `false` | If true, set up a single hop non-anonymous tor hidden service for this App. This is an opsec risk. |
| `haproxy` | `true` | If true, annotate requests with the Haproxy Proxy protocol to let applications identify individual tor circuits. |
| `proofOfWorkDefense` | `true` | If true, require clients to pass a proof of work challenge before they can connect. |

### Persistent storage

If you enable this, don't have more than one replica. All PersistentVolumeClaims created by this feature use `ReadWriteOnce` storage. You have been warned.

| Setting | Example | Description |
| :------ | :------ | :---------- |
| `enabled` | `true` | If true, create persistent storage for this App. |
| `path` | `/data` | (REQUIRED) Where to mount the storage in your App Pods. |
| `size` | `4Gi` | (REQUIRED) How much storage to allocate. |
| `storageClass` | `ssd` | What Kubernetes storage class to use for the persistent storage. |

### Secrets

Secrets in 1Password's Kubernetes vault.

| Setting | Example | Description |
| :------ | :------ | :---------- |
| `name` | `tigris-creds` | (REQUIRED) The name of the secret in Kubernetes with the App name prepended (eg: `stickers-tigris-creds`). |
| `itemPath` | `vaults/Kubernetes/items/Foo` | (REQUIRED) The 1Password item path of the secret data. |
| `environment` | `true` | If true, set the secret values as environment variables. |
| `folder` | `true` | If true, mount the secret as a folder in `/run/secrets/{name}`. |
