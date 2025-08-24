# MaxTac: Dynamic Network Policy Controller 👮‍♂️

<img src="public/maxtac.png" width="130" style="margin: 15px;" align=center />

## Table of Contents

- [ℹ️ Description](#️-description)
- [✨ Core Concepts](#-core-concepts)
  - [1. `ClusterAccess`: For Intra-Cluster Communication](#1-access-for-intra-cluster-communication)
  - [2. `ClusterExternalAccess`: For External Communication](#2-externalaccess-for-external-communication)
- [⚙️ How It Works](#️-how-it-works)
- [📦 Installation](#-installation)
- [📜 Custom Resource Reference](#-custom-resource-reference)
  - [`ClusterExternalAccess`](#externalaccess)
  - [`ExternalAccess`](#externalaccess)
  - [`ClusterAccess`](#access)
  - [`Access`](#access)
- [🏷️ Service Annotation Reference](#️-service-annotation-reference)
  - [For `ClusterExternalAccess`](#for-externalaccess)
  - [For `ClusterExternalAccess`](#for-externalaccess)
  - [For `ClusterAccess`](#for-access)
  - [For `Access`](#for-access)
- [ℹ️ Status reference](#️-status-reference)
- [🚀 Usage Example Walkthrough](#-usage-example-walkthrough)

## ℹ️ Description

MaxTac is a Kubernetes controller that simplifies and automates the management of `NetworkPolicy` resources. It works by introducing two Custom Resource Definitions (CRDs), `ClusterAccess` and `ClusterExternalAccess`, which allow you to define network connectivity rules at a higher level of abstraction.

The controller monitors these CRs and specially annotated `Service` resources to dynamically create, update, and delete the underlying Kubernetes `NetworkPolicy` objects, ensuring the cluster's network state always matches your desired intent.

With MaxTac, it is possible to open an ingress/egress from/to an ip or a group of pods, with one resource ClusterAccess or ClusterExternalAccess, and delete it or recreate it all at once in a few seconds.

It is especially useful, if you desire to manage dynamically multiple netpols at once or if you want to set network rules if a more straightforward way as it basically allows you to tell which group of pods can go talk to which other group of pods by polling labeled services then pulling out their podSelectors to create the corresponding netpols.

---

## ✨ Core Concepts

MaxTac operates on two primary resources to manage two distinct use cases:

### 1. `ClusterAccess`: For Intra-Cluster Communication

The **`ClusterAccess`** resource manages network policies for communication _between services_ within the cluster. You define a "source" service and a "target" service, and MaxTac creates the necessary `NetworkPolicy` to allow traffic between them.

### 2. `ClusterExternalAccess`: For External Communication

The **`ClusterExternalAccess`** resource manages network policies for communication between a service inside the cluster and an _external IP address range (CIDR)_. This is ideal for controlling egress traffic to a public API, an external database, or any other resource outside the Kubernetes cluster.

---

## ⚙️ How It Works

The controller uses a declarative, label-driven approach. Instead of creating complex `NetworkPolicy` YAML files by hand, you simply:

1. **Create an `ClusterAccess` or `ClusterExternalAccess` resource.** This resource uses a `serviceSelector` to identify which services it should pay attention to.
2. **Label and Annotate your Services.** You apply a matching label to your "source" `Service`(s). Then, you add specific annotations to declare the desired target and the direction of traffic.
3. **Let the Controller Do the Work.** MaxTac detects the CR, finds the matching services, reads their annotations, and generates the appropriate `NetworkPolicy` in the correct namespace.

When a service is deleted or its labels/annotations change, the controller automatically cleans up or updates the corresponding `NetworkPolicy`, preventing orphaned or incorrect rules.

---

# 📦 Installation

Installing MaxTac is straightforward. You just need to apply the installation manifest to your Kubernetes cluster using kubectl. This single file typically includes the Custom Resource Definitions (CRDs), the controller's Deployment, and the necessary RBAC (Role-Based Access Control) permissions.

Just set the variable using the latest release or the desired version.

![GitHub VERSION (latest by date)](https://img.shields.io/github/v/release/Banh-Canh/MaxTac?label=Latest%20Release)

```bash
VERSION=vX.X.X
kubectl apply -f https://github.com/Banh-Canh/MaxTac/releases/download/$VERSION/bundle.yaml
```

This will create the maxtac-system namespace and all the required resources,

You should be able to see the controller running:

```bash
k get po -n maxtac-system
NAME                                         READY   STATUS    RESTARTS   AGE
maxtac-controller-manager-5f5544b549-w8vhn   1/1     Running   0          112s
```

You can now apply any ClusterAccess or ClusterExternalAccess object.

---

# 📜 Custom Resource Reference

### `ClusterExternalAccess`

This resource grants services access to an external CIDR.

**Spec Fields:**

- `targetCIDRs` (`[]string`, required): The external IPs range to allow traffic to/from. Example: `1.1.1.1/32, 2001:4860:4860::8888/128`.
- `serviceSelector` (`metav1.LabelSelector`, required): A label selector to find `Service` resources that should be granted this access.
- `direction` (`string`, optional): Set the policy type of the network policy. Can be overrided by annotation on the service.
- `duration` (`string` optional): Set the duration of the opened access (ex: 3600s)

**Example (`externalaccess-sample.yaml`):**

```yaml
apiVersion: maxtac.vtk.io/v1alpha1
kind: ClusterExternalAccess
metadata:
  name: externalaccess-sample
spec:
  targetCIDRs:
    - 1.1.1.1/32
    - 2001:4860:4860::8888/128
  serviceSelector:
    matchLabels:
      # The controller will look for services with this label
      externalaccess: 'yes'
```

### `ExternalAccess`

Basically the same as the ClusterExternalAccess, but namespaced. The selector while only match services in the same namespace.

### `ClusterAccess`

**Spec Fields:**

- `serviceSelector` (`metav1.LabelSelector`, required): A label selector to find "source" Service resources that will be granted access to a target.
- `[x]targets.serviceName` (`string`, optional): the name of the service to be targeted. Can be overrided by an annotation on the service.
- `[x]targets.namespace` (`string`, optional): the namespace of the service to be targeted. Can be overrided by an annotation on the service.
- `direction` (`string`, optional): Set the policy type of the network policy. Can be overrided by annotation on the service.
- `mirrored` (`bool`, optional): Set if the policy will be mirrored, as in completed by an ingress in the targeted namespace if the netpol to be created is an egress from the source namespace.
- `duration` (`string` optional): Set the duration of the opened access (ex: 3600s)

**Example (`access-sample.yaml`):**

```yaml
apiVersion: maxtac.vtk.io/v1alpha1
kind: ClusterAccess
metadata:
  name: access-sample
spec:
  serviceSelector:
    matchLabels:
      # The controller will look for services with this label
      access: 'yes'
  targets:
    - serviceName: bazarr
      namespace: bazarr
    - serviceName: sonarr
      namespace: sonarr
  direction: ingress
  mirrored: true
```

### `Access`

Basically the same but namespaced. Also `.spec.mirrored` is not available in the namespaced version.

#### `Service`

Not a custom resource, but an example that showcase a service making use of both resource to create network policies:

```yaml
apiVersion: v1
kind: Service
metadata:
  annotations:
    maxtac.vtk.io.clusteraccess/direction: ingress
    maxtac.vtk.io.clusteraccess/targets: prowlarr,prowlarr;radarr,radarr;jellyfin,jellyfin
    maxtac.vtk.io.clusterexternalaccess/direction: ingress
    maxtac.vtk.io.clusterexternalaccess/targets: 1.1.1.1-32,2001:4860:4860::8888-128
  labels:
    access: yes
    externalaccess: yes
  name: bazarr-test-maxtac-both
  namespace: bazarr
spec:
  internalTrafficPolicy: Cluster
  ipFamilies:
    - IPv4
  ipFamilyPolicy: SingleStack
  ports:
    - port: 6767
      protocol: TCP
      targetPort: bazarr
  selector:
    app: bazarr
  sessionAffinity: None
  type: ClusterIP
```

It is possible for a service to be watched and used to generate network policies with different rules from different Access or ClusterExternalAccess.

---

## 🏷️ Service Annotation Reference

Annotations on the `Service` objects are crucial for telling the controller what to do. It overrides the spec of both resources.

### For `ClusterExternalAccess`

When a `Service` is matched by an `ClusterExternalAccess` resource's `serviceSelector`, it must have the following annotation:

- `maxtac.vtk.io.clusterexternalaccess/direction`: Defines the direction of allowed traffic.
  - **Values**: `"ingress"`, `"egress"`, `"all"`.
  - **Example**: `maxtac.vtk.io.clusterexternalaccess/direction: "egress"` allows the pods behind this service to make outbound calls to the `targetCidr`.
- `maxtac.vtk.io.clusteraccess/targets`: The cidr of the targets. example: `maxtac.vtk.io.clusteraccess/targets: 1.1.1.1-32,2001:4860:4860::8888-128`

### For `ClusterAccess`

When a `Service` is matched by an `ClusterAccess` resource's `serviceSelector`, it must have the following three annotations to define the connection:

- `maxtac.vtk.io.clusteraccess/targets`: The name of the targets `Service`. example: `maxtac.vtk.io.clusteraccess/targets: <ns1>,<svc1;<ns2>,<svc2>`
- `maxtac.vtk.io.clusteraccess/direction`: Defines the direction of allowed traffic relative to the annotated service.
  - **Values**: `"ingress"`, `"egress"`, `"all"`.
  - **Example**: `maxtac.vtk.io.clusteraccess/direction: "egress"` allows the pods behind the annotated service to connect to the target service.

### For the namespaced variant

Remove the `cluster` from the annotation. So `maxtac.vtk.io.clusteraccess/targets` becomes `maxtac.vtk.io.access/targets`.

---

## ℹ️ Status reference

```yaml
status:
  conditions:
    - lastTransitionTime: '2025-08-18T08:50:48Z'
      message: Successfully created all 'NetworkPolicy' child resources.
      reason: NetworkPolicyCreateSuccess
      status: 'True'
      type: NetworkPolicyDeployReady
    - lastTransitionTime: '2025-08-18T08:50:49Z'
      message: Successfully synced all matching netpols child resources.
      reason: NetpolsSyncSuccess
      status: 'True'
      type: NetpolsListReady
    - lastTransitionTime: '2025-08-18T08:50:49Z'
      message: Successfully synced all matching services child resources.
      reason: ServicesSyncSuccess
      status: 'True'
      type: ServicesListReady
    - lastTransitionTime: '2025-08-18T08:50:49Z'
      message: All conditions are met.
      reason: AllConditionsReady
      status: 'True'
      type: Ready
  netpols: ## List generated netpols by this access
    - name: access-sample--zitadel-zitadel---zitadel-postgres-zitadel-all-mirror
      namespace: zitadel
    - name: access-sample--ombi-ombi---zitadel-postgres-zitadel-all-mirror
      namespace: ombi
    - name: access-sample--zitadel-zitadel---ombi-postgres-ombi-all-mirror
      namespace: zitadel
    - name: access-sample--ombi-ombi---ombi-postgres-ombi-all-mirror
      namespace: ombi
  services: ## List matched services by this access
    - name: postgres-zitadel
      namespace: zitadel
    - name: postgres-ombi
      namespace: ombi
```

## 🚀 Usage Example Walkthrough

Let's illustrate how to allow a `backend-api` service to connect to an external database at `1.1.1.1`.

**1. Create the `ClusterExternalAccess` Resource**

First, we apply the `Access` manifest. This tells the controller to watch for services labeled with `database: "things"` and grant them access to and from `zitadel pods and ombi pods`.

We set the mirrored so it create the reciprocating netpols in the target namespace as well.

```yaml
# externalaccess-sample.yaml
apiVersion: maxtac.vtk.io/v1alpha1
kind: Access
metadata:
  labels:
    app.kubernetes.io/name: maxtac
    app.kubernetes.io/managed-by: kustomize
  name: access-sample
spec:
  serviceSelector:
    matchLabels:
      database: things
  targets:
    - serviceName: zitadel
      namespace: zitadel
    - serviceName: ombi
      namespace: ombi
  direction: all
  mirrored: true
---
apiVersion: maxtac.vtk.io/v1alpha1
kind: ClusterExternalAccess
metadata:
  labels:
    app.kubernetes.io/name: maxtac
    app.kubernetes.io/managed-by: kustomize
  name: allow-external
spec:
  targetCIDRs:
    - 1.1.1.1/32
    - 2001:4860:4860::8888/128
  serviceSelector:
    matchLabels:
      externalaccess: yes
  direction: ingress
```

**2. Annotate and Label the Source Service**

Now, we update our `postgres-ombi and postgres-zitadel` service to include the required label.

```yaml
# backend-api-service.yaml
apiVersion: v1
kind: Service
metadata:
  labels:
    database: things
    externalaccess: yes
  name: postgres-zitadel
  namespace: zitadel
spec:
  internalTrafficPolicy: Cluster
  ipFamilies:
    - IPv4
  ipFamilyPolicy: SingleStack
  ports:
    - port: 5432
      protocol: TCP
      targetPort: 5432
  selector:
    cnpg.io/cluster: zitadel-postgres
    cnpg.io/instanceRole: primary
  type: ClusterIP
---
apiVersion: v1
kind: Service
metadata:
  labels:
    database: things
  name: postgres-ombi
  namespace: ombi
spec:
  internalTrafficPolicy: Cluster
  ipFamilies:
    - IPv4
  ipFamilyPolicy: SingleStack
  ports:
    - port: 5432
      protocol: TCP
      targetPort: 5432
  selector:
    cnpg.io/cluster: ombi-postgres
    cnpg.io/instanceRole: primary
  type: ClusterIP
```

**3. The Result ✅**

The MaxTac controller will now automatically create a NetworkPolicy in the ombi and zitadel namespace. This policy will look something like this:

It will open the traffic so both zitadel and ombi can interact with both postgres

<details>
  <summary>Click Here to see the deployed netpols</summary>
<pre>
NAMESPACE               NAME                                                                 POD-SELECTOR
ombi        access-sample--ombi-ombi---ombi-postgres-ombi-all-mirror                 app=ombi,part-of=ombi
ombi        access-sample--ombi-ombi---zitadel-postgres-zitadel-all-mirror           app=ombi,part-of=ombi
ombi        access-sample--ombi-postgres-ombi---ombi-ombi-all                        cnpg.io/cluster=ombi-postgres,cnpg.io/instanceRole=primary
ombi        access-sample--ombi-postgres-ombi---zitadel-zitadel-all                  cnpg.io/cluster=ombi-postgres,cnpg.io/instanceRole=primary
zitadel     access-sample--zitadel-postgres-zitadel---ombi-ombi-all                  cnpg.io/cluster=zitadel-postgres,cnpg.io/instanceRole=primary
zitadel     access-sample--zitadel-postgres-zitadel---zitadel-zitadel-all            cnpg.io/cluster=zitadel-postgres,cnpg.io/instanceRole=primary
zitadel     access-sample--zitadel-zitadel---ombi-postgres-ombi-all-mirror           app.kubernetes.io/instance=zitadel,app.kubernetes.io/name=zitadel
zitadel     access-sample--zitadel-zitadel---zitadel-postgres-zitadel-all-mirror     app.kubernetes.io/instance=zitadel,app.kubernetes.io/name=zitadel
zitadel     allow-external--zitadel-postgres-zitadel---1-1-1-1-32-ing                cnpg.io/cluster=zitadel-postgres,cnpg.io/instanceRole=primary
zitadel     allow-external--zitadel-postgres-zitadel---2001-4860-4860--8888-128-ing  cnpg.io/cluster=zitadel-postgres,cnpg.io/instanceRole=primary
</pre>
</details>
