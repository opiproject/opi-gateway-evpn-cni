# EVPN GW CNI Plugin

[![Linters](https://github.com/opiproject/opi-gateway-evpn-cni/actions/workflows/linters.yml/badge.svg)](https://github.com/opiproject/opi-gateway-evpn-cni/actions/workflows/linters.yml)
[![CodeQL](https://github.com/opiproject/opi-gateway-evpn-cni/actions/workflows/codeql.yml/badge.svg)](https://github.com/opiproject/opi-gateway-evpn-cni/actions/workflows/codeql.yml)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/opiproject/opi-gateway-evpn-cni/badge)](https://securityscorecards.dev/viewer/?platform=github.com&org=opiproject&repo=opi-gateway-evpn-cni)
[![tests](https://github.com/opiproject/opi-gateway-evpn-cni/actions/workflows/go.yml/badge.svg)](https://github.com/opiproject/opi-gateway-evpn-cni/actions/workflows/go.yml)
[![Docker](https://github.com/opiproject/opi-gateway-evpn-cni/actions/workflows/docker-publish.yml/badge.svg)](https://github.com/opiproject/opi-gateway-evpn-cni/actions/workflows/docker-publish.yml)
[![License](https://img.shields.io/github/license/opiproject/opi-gateway-evpn-cni?style=flat-square&color=blue&label=License)](https://github.com/opiproject/opi-gateway-evpn-cni/blob/master/LICENSE)
[![codecov](https://codecov.io/gh/opiproject/opi-gateway-evpn-cni/branch/main/graph/badge.svg)](https://codecov.io/gh/opiproject/opi-gateway-evpn-cni)
[![Go Report Card](https://goreportcard.com/badge/github.com/opiproject/opi-gateway-evpn-cni)](https://goreportcard.com/report/github.com/opiproject/opi-gateway-evpn-cni)
[![Go Doc](https://img.shields.io/badge/godoc-reference-blue.svg)](http://godoc.org/github.com/opiproject/opi-gateway-evpn-cni)
[![Pulls](https://img.shields.io/docker/pulls/opiproject/opi-gateway-evpn-cni.svg?logo=docker&style=flat&label=Pulls)](https://hub.docker.com/r/opiproject/opi-gateway-evpn-cni)
[![Last Release](https://img.shields.io/github/v/release/opiproject/opi-gateway-evpn-cni?label=Latest&style=flat-square&logo=go)](https://github.com/opiproject/opi-gateway-evpn-cni/releases)
[![GitHub stars](https://img.shields.io/github/stars/opiproject/opi-gateway-evpn-cni.svg?style=flat-square&label=github%20stars)](https://github.com/opiproject/opi-gateway-evpn-cni)
[![GitHub Contributors](https://img.shields.io/github/contributors/opiproject/opi-gateway-evpn-cni.svg?style=flat-square)](https://github.com/opiproject/opi-gateway-evpn-cni/graphs/contributors)

- [EVPN GW CNI Plugin](#evpn-gw-cni-plugin)
  - [Build](#build)
  - [Kubernetes Quick Start](#kubernetes-quick-start)
  - [Usage](#usage)
    - [Basic configuration parameters](#basic-configuration-parameters)
    - [Example NADs](#example-nads)
      - [Access type](#access-type)
      - [Selective trunk type](#selective-trunk-type)
      - [Transparent trunk type](#transparent-trunk-type)
    - [Kernel driver device](#kernel-driver-device)
    - [DPDK userspace driver device](#dpdk-userspace-driver-device)
    - [CNI Configuration](#cni-configuration)
  - [I Want To Contribute](#i-want-to-contribute)

This plugin integrates with the different xPU cards in order to enable secondary xPU VF interfaces in the Kubernetes Pods which will terminate traffic that runs through an xPU pipeline.

It has two main sections. The first one attaches xPU VFs into Pods the same way as any SR-IOV VF. The second part contacts the [opi-evpn-bridge](https://github.com/opiproject/opi-evpn-bridge) component in order to create a `BridgePort` which will act as the abstracted port representor of the previously attached VF in the Pod. The `BridgePort` can be of type `access` attaching to only one `LogicalBridge` or of type `trunk` attaching to multiple `LogicalBridges`. This way the `opi-evpn-bridge` component will offload all the appropriate rules into the xPU forwarding pipeline which will result in traffic flowing from and towards the Pod using the attached xPU VF which acts as secondary interface in the Pod's networking namespace.

The plugin is heavily integrated with the [EVPN GW API](https://github.com/opiproject/opi-api/tree/main/network/evpn-gw) in OPI and is used to serve the EVPN GW offload Use Case into an xPU. The only xPU that is supported currently is the Intel Mt.Evans IPU card.

EVPN GW CNI plugin works with [SR-IOV device plugin](https://github.com/k8snetworkplumbingwg/sriov-network-device-plugin) for VF allocation in Kubernetes. A metaplugin such as [Multus](https://github.com/intel/multus-cni) gets the allocated VF's `deviceID` (PCI address) and is responsible for invoking the EVPN GW CNI plugin with that `deviceID`.

The CNI has been tested against Multus v4.0.1, v3.9.1 versions

## Build

This plugin uses Go modules for dependency management and requires Go 1.20.x to build.

To build the plugin binary:

``
make
``

Upon successful build the plugin binary will be available in `build/evpn-gw`.

## Kubernetes Quick Start

A full guide on orchestrating SR-IOV virtual functions in Kubernetes can be found at the [SR-IOV Device Plugin project.](https://github.com/k8snetworkplumbingwg/sriov-network-device-plugin#quick-start)

Creating VFs is outside the scope of the EVPN GW CNI plugin. [More information about allocating VFs on different NICs can be found here](https://github.com/k8snetworkplumbingwg/sriov-network-device-plugin/blob/master/docs/vf-setup.md)

To deploy EVPN GW CNI by itself on a Kubernetes 1.23+ cluster

Build the EVPN GW CNI docker image:

`make image`

Deploy the EVPN GW CNI daemonset:

`kubectl apply -f images/evpn-gw-cni-daemonset.yaml`

**Note** The above deployment is not sufficient to manage and configure SR-IOV virtual functions. [See the full orchestration guide for more information.](https://github.com/k8snetworkplumbingwg/sriov-network-device-plugin#sr-iov-network-device-plugin)

## Usage

EVPN GW CNI networks are commonly configured using Multus and SR-IOV Device Plugin using Network Attachment Definitions. More information about configuring Kubernetes networks using this pattern can be found in the [Multus configuration reference document.](https://github.com/k8snetworkplumbingwg/multus-cni/blob/master/docs/configuration.md)

A Network Attachment Definition for EVPN GW CNI takes the form:

```yaml
apiVersion: "k8s.cni.cncf.io/v1"
kind: NetworkAttachmentDefinition
metadata:
  name: nad-access
  annotations:
    k8s.v1.cni.cncf.io/resourceName: intel.com/intel_sriov_mev 
spec:
  config: '{
      "cniVersion": "0.4.0",
      "type": "evpn-gw",
      "logical_bridge": "//network.opiproject.org/bridges/vlan10",
      "ipam": {
              "type": "static"
              }
    }'
```

The `.spec.config` field contains the configuration information used by the EVPN GW CNI.

### Basic configuration parameters

The following parameters are generic parameters which are not specific to the EVPN GW CNI configuration, though (with the exception of ipam) they need to be included in the config.

- `cniVersion` : the version of the CNI spec used.
- `type` : CNI plugin used. "evpn-gw" corresponds to EVPN GW CNI.
- `ipam` (optional) : the configuration of the IP Address Management plugin. Required to designate an IP for a kernel interface.

### Example NADs

The following examples show the config needed to set up basic secondary networking in a container using EVPN GW CNI. Each of the json config objects below can be placed in the `.spec.config` field of a Network Attachment Definition to integrate with Multus.

#### Access type

To allow untagged vlan access type of traffic flowing from and towards the attached XPU VF of the Pod then a NAD is needed that will refer to just one `LogicalBridge`. This way the `BridgePort` that will be created by EVPN GW CNI will be of type `Access`

```yaml
apiVersion: "k8s.cni.cncf.io/v1"
kind: NetworkAttachmentDefinition
metadata:
  name: nad-access
  annotations:
    k8s.v1.cni.cncf.io/resourceName: intel.com/intel_sriov_mev 
spec:
  config: '{
      "cniVersion": "0.4.0",
      "type": "evpn-gw",
      "logical_bridge": "//network.opiproject.org/bridges/vlan10",
      "ipam": {
              "type": "static"
              }
    }'
```

#### Selective trunk type

To allow selective vlan tagged type of traffic flowing from and towards the attached xPU VF of the Pod then a NAD is needed that will refer to  multiple `LogicalBridges`. This way the `BridgePort` that will be created by EVPN GW CNI will be of type `Trunk` but only for selective range of VLANs

```yaml
apiVersion: "k8s.cni.cncf.io/v1"
kind: NetworkAttachmentDefinition
metadata:
  name: nad-selective-trunk
  annotations:
    k8s.v1.cni.cncf.io/resourceName: intel.com/intel_sriov_mev 
spec:
  config: '{
      "cniVersion": "0.4.0",
      "type": "evpn-gw",
      "logical_bridges": ["//network.opiproject.org/bridges/vlan10","//network.opiproject.org/bridges/vlan20","//network.opiproject.org/bridges/vlan40"]
    }'
```

#### Transparent trunk type

To allow transparent vlan tagged type of traffic flowing from and towards the attached xPU VF of the Pod then a NAD is needed without any `LogicalBridges`. This way the `BridgePort` that will be created by EVPN GW CNI will be of type `Trunk` and will allow transparent vlan tagged traffic.

```yaml
apiVersion: "k8s.cni.cncf.io/v1"
kind: NetworkAttachmentDefinition
metadata:
  name: nad-trunk
  annotations:
    k8s.v1.cni.cncf.io/resourceName: intel.com/intel_sriov_mev 
spec:
  config: '{
      "cniVersion": "0.4.0",
      "type": "evpn-gw"
    }'
```

### Kernel driver device

All the above examples can work implicitly when xPU VFs using a kernel driver are configured as secondary interfaces into containers. Also when the VF is handled by a kernel driver any IPAM configuration that is passed will be configured into the attached VF in the container.

### DPDK userspace driver device

The above examples will configure also a xPU VF using a userspace driver (uio/vfio) for use in a container. If this plugin is used with a xPU VF bound to a dpdk driver then the IPAM configuration will still be respected, but it will only allocate IP address(es) using the specified IPAM plugin, not apply the IP address(es) to container interface. In order for the EVPN GW CNI to configure a userspace driver bound xPU VF the only thing that needs to be changed in the above example NADs is the annotation so the correct device pool is used.

### CNI Configuration

Due to a limitation on Intel Mt.Evans for the dpdk use case to work
we need a `pci_to_mac.conf` file that looks like this:

```json
{
  "0000:b0:00.1": "00:21:00:00:03:14",
  "0000:b0:00.0": "00:20:00:00:03:14",
  "0000:b0:00.3": "00:23:00:00:03:14",
  "0000:b0:00.2": "00:22:00:00:03:14"
}
```

in the path: `/etc/cni/net.d/evpn-gw.d/`

The EVPN GW CNI plugin needs a `evpn-gw.conf` configuration file in order to know where to find the `pci_to_mac.conf` file and also how to contact the `opi-evpn-bridge` grpc server (It is a component of the bigger xpu_infra_mgr system) for the creation of the `BridgePorts`. The file looks like this:

```json
{
  "opi_evpn_bridge_conn": "<grpc-server-ip>:<grpc-server-port>",
  "pci_to_mac_path": "/etc/cni/net.d/evpn-gw.d/pci_to_mac.conf"
}
```

and should be putted in the path: `/etc/cni/net.d/evpn-gw.d/`

**Note** [DHCP](https://github.com/containernetworking/plugins/tree/master/plugins/ipam/dhcp) IPAM plugin can not be used for VF bound to a dpdk driver (uio/vfio).

## I Want To Contribute

This project welcomes contributions and suggestions.  We are happy to have the Community involved via submission of **Issues and Pull Requests** (with substantive content or even just fixes). We are hoping for the documents, test framework, etc. to become a community process with active engagement.  PRs can be reviewed by by any number of people, and a maintainer may accept.

See [CONTRIBUTING](https://github.com/opiproject/opi/blob/main/CONTRIBUTING.md) and [GitHub Basic Process](https://github.com/opiproject/opi/blob/main/doc-github-rules.md) for more details.s.

---

## Funding & Initiative

KeeCEK is part of the Important Projects of Common European Interest – Cloud Infrastructure and Services (8ra/IPCEI-CIS) initiative. 8ra/IPCEI-CIS aims to establish a Multi-Provider Cloud-Edge Continuum across Europe – a cloud ecosystem designed to revolutionize data processing in the next generation of cloud and edge infrastructure and services. The initiative represents a united European effort and stands under a common governance mandated by the community of industrial partners and member states.<br><br>
**More info:** [https://8ra.com/ipcei-cis/](https://8ra.com/ipcei-cis/)

![Funding](https://github.com/opi-gateway-evpn-cni/BMWE2025_NextGenEU_gef_en_RGB.png)
